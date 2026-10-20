// Package repocheck has no code; its tests keep files about the repository itself
// consistent, such as the command lists the coding agents' tools read (QA-15).
package repocheck

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// commandLists are the commands one tool runs without asking (allow) and the ones it
// must ask about (ask), each as the words it starts with, e.g. "gh pr view". Both
// tools match how a command starts, whatever arguments follow (QA-15).
type commandLists struct {
	allow, ask map[string]bool
}

// claudeCommands reads Claude Code's shared settings. Claude Code matches
// "Bash(git log)" only without arguments and "Bash(git log *)" only with them,
// while a Codex rule matches both; so every command needs both forms, and a
// command with one form is returned as a problem instead of hiding the difference
// (QA-15).
func claudeCommands(data []byte) (commandLists, []string, error) {
	var s struct {
		Permissions struct{ Allow, Ask []string }
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return commandLists{}, nil, err
	}
	var problems []string
	words := func(list string, rules []string) map[string]bool {
		exact, withArgs := map[string]bool{}, map[string]bool{}
		for _, r := range rules {
			cmd, ok := strings.CutPrefix(r, "Bash(")
			if !ok || !strings.HasSuffix(cmd, ")") {
				continue // other tools' rules (Read, WebFetch, …) have no Codex twin
			}
			cmd = strings.TrimSuffix(cmd, ")")
			switch base, ok := strings.CutSuffix(cmd, " *"); {
			case ok:
				withArgs[base] = true
			case strings.Contains(cmd, "*"):
				problems = append(problems, fmt.Sprintf("Claude %s %q: write it as %q and %q", list, r, "Bash(X)", "Bash(X *)"))
			default:
				exact[cmd] = true
			}
		}
		for _, cmd := range difference(exact, withArgs) {
			problems = append(problems, fmt.Sprintf("Claude %s %q only without arguments; Codex matches it with any: add \"Bash(%s *)\"", list, cmd, cmd))
		}
		for _, cmd := range difference(withArgs, exact) {
			problems = append(problems, fmt.Sprintf("Claude %s %q only with arguments; Codex matches it without: add \"Bash(%s)\"", list, cmd, cmd))
		}
		for cmd := range exact {
			withArgs[cmd] = true
		}
		return withArgs
	}
	lists := commandLists{allow: words("allow", s.Permissions.Allow), ask: words("ask", s.Permissions.Ask)}
	return lists, problems, nil
}

var (
	ruleStart   = regexp.MustCompile(`(?m)^prefix_rule\(`)
	patternExpr = regexp.MustCompile(`pattern\s*=\s*\[`)
	decisionArg = regexp.MustCompile(`decision\s*=\s*"(\w+)"`)
)

// codexCommands reads the Codex rules. A pattern element that is a list holds
// alternatives, so ["gh", "pr", ["list", "view"]] stands for "gh pr list" and
// "gh pr view"; "prompt" is Codex's word for ask (QA-15).
func codexCommands(data []byte) (commandLists, error) {
	lists := commandLists{allow: map[string]bool{}, ask: map[string]bool{}}
	starts := ruleStart.FindAllIndex(data, -1)
	for i, s := range starts {
		end := len(data)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		rule := string(data[s[1]:end])
		loc := patternExpr.FindStringIndex(rule)
		if loc == nil {
			return lists, fmt.Errorf("a prefix_rule without a pattern: %.60q", rule)
		}
		pattern, err := parseList(rule[loc[1]:])
		if err != nil {
			return lists, fmt.Errorf("%w in %.60q", err, rule)
		}
		target := lists.allow // Codex's default decision
		if m := decisionArg.FindStringSubmatch(rule); m != nil {
			switch m[1] {
			case "allow":
			case "prompt":
				target = lists.ask
			default:
				return lists, fmt.Errorf("decision %q has no Claude Code twin", m[1])
			}
		}
		for _, cmd := range expand(pattern) {
			target[cmd] = true
		}
	}
	return lists, nil
}

// parseList reads a Starlark list of strings and lists of strings, starting just
// after its "[", e.g. `"git", ["status", "log"]]` (QA-15).
func parseList(s string) ([][]string, error) {
	var out [][]string
	var alts []string
	inner := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == ' ' || c == '\n' || c == '\t' || c == ',':
		case c == '"':
			end := strings.IndexByte(s[i+1:], '"')
			if end < 0 {
				return nil, fmt.Errorf("unclosed string")
			}
			word, err := strconv.Unquote(s[i : i+end+2])
			if err != nil {
				return nil, err
			}
			i += end + 1
			if inner {
				alts = append(alts, word)
			} else {
				out = append(out, []string{word})
			}
		case c == '[' && !inner:
			inner, alts = true, nil
		case c == ']' && inner:
			inner = false
			out = append(out, alts)
		case c == ']':
			return out, nil
		default:
			return nil, fmt.Errorf("unexpected %q in pattern", c)
		}
	}
	return nil, fmt.Errorf("pattern not closed")
}

// expand turns a pattern with alternatives into every command it matches (QA-15).
func expand(pattern [][]string) []string {
	cmds := []string{""}
	for _, alts := range pattern {
		var next []string
		for _, c := range cmds {
			for _, a := range alts {
				next = append(next, strings.TrimSpace(c+" "+a))
			}
		}
		cmds = next
	}
	return cmds
}

// difference lists what a has and b lacks, sorted (QA-15).
func difference(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// compareLists returns every way the two tools' files disagree, or a file cannot be
// read, as one line each (QA-15).
func compareLists(claudeData, codexData []byte) []string {
	claude, problems, err := claudeCommands(claudeData)
	if err != nil {
		return []string{".claude/settings.json: " + err.Error()}
	}
	codex, err := codexCommands(codexData)
	if err != nil {
		return []string{"apptrol.rules: " + err.Error()}
	}
	for _, d := range []struct {
		name          string
		claude, codex map[string]bool
	}{
		{"allowed", claude.allow, codex.allow},
		{"asked about", claude.ask, codex.ask},
	} {
		if missing := difference(d.claude, d.codex); len(missing) > 0 {
			problems = append(problems, fmt.Sprintf("%s for Claude, not for Codex: %s", d.name, strings.Join(missing, ", ")))
		}
		if missing := difference(d.codex, d.claude); len(missing) > 0 {
			problems = append(problems, fmt.Sprintf("%s for Codex, not for Claude: %s", d.name, strings.Join(missing, ", ")))
		}
	}
	return problems
}

func readRepoFile(t *testing.T, parts ...string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestQA15_AgentCommandListsMatch keeps the two tools' lists equal, so Claude Code
// and Codex may run, and must ask about, the same commands (QA-15).
func TestQA15_AgentCommandListsMatch(t *testing.T) {
	claudeData, codexData := readRepoFile(t, ".claude", "settings.json"), readRepoFile(t, ".codex", "rules", "apptrol.rules")
	for _, p := range compareLists(claudeData, codexData) {
		t.Error(p)
	}
	codex, err := codexCommands(codexData)
	if err != nil {
		t.Fatal(err)
	}
	if len(codex.allow) < 10 || len(codex.ask) < 10 {
		t.Fatalf("found only %d allowed and %d asked-about commands; did a file change shape?", len(codex.allow), len(codex.ask))
	}
}

// mustAsk are the commands that push or change GitHub. They need the owner's OK
// (AGENTS.md › Workflow), so this list is kept here on its own: removing one from
// both files at once must still fail (QA-15).
var mustAsk = []string{
	"git push",
	"gh pr create", "gh pr ready", "gh pr edit", "gh pr merge", "gh pr close", "gh pr comment", "gh pr review",
	"gh issue create", "gh issue edit", "gh issue close", "gh issue comment",
	"gh run rerun", "gh run cancel", "gh run delete",
	"gh api", "gh label", "gh release", "gh repo", "gh secret", "gh variable", "gh workflow",
}

// TestQA15_ChangesOutsideTheCheckoutAsk: both tools ask before pushing or
// changing GitHub, whatever else a personal settings file allows (QA-15).
func TestQA15_ChangesOutsideTheCheckoutAsk(t *testing.T) {
	claude, _, err := claudeCommands(readRepoFile(t, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	codex, err := codexCommands(readRepoFile(t, ".codex", "rules", "apptrol.rules"))
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range mustAsk {
		if !claude.ask[cmd] || !codex.ask[cmd] {
			t.Errorf("%q must ask in both tools (Claude %v, Codex %v)", cmd, claude.ask[cmd], codex.ask[cmd])
		}
		if claude.allow[cmd] || codex.allow[cmd] {
			t.Errorf("%q must not be allowed", cmd)
		}
	}
}

// TestQA15_OneFormOnlyIsADifference: Claude Code's "Bash(git pull)" does not
// allow "git pull --rebase", Codex's rule does; the check must not treat the two
// as equal (QA-15, review of #159).
func TestQA15_OneFormOnlyIsADifference(t *testing.T) {
	codex := []byte(`prefix_rule(pattern = ["git", ["status", "pull"]], decision = "allow")` + "\n")
	claude := func(rules ...string) []byte {
		data, err := json.Marshal(map[string]any{"permissions": map[string]any{"allow": rules}})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if p := compareLists(claude("Bash(git status)", "Bash(git status *)", "Bash(git pull)", "Bash(git pull *)"), codex); len(p) > 0 {
		t.Fatalf("matching lists reported: %q", p)
	}
	for name, c := range map[string]struct {
		rules []string
		want  string
	}{
		"exact only":     {[]string{"Bash(git status)", "Bash(git status *)", "Bash(git pull)"}, `"git pull" only without arguments`},
		"arguments only": {[]string{"Bash(git status *)", "Bash(git pull)", "Bash(git pull *)"}, `"git status" only with arguments`},
		"glob":           {[]string{"Bash(git status)", "Bash(git status *)", "Bash(git pull*)"}, `"Bash(git pull*)"`},
	} {
		t.Run(name, func(t *testing.T) {
			p := compareLists(claude(c.rules...), codex)
			if !slices.ContainsFunc(p, func(s string) bool { return strings.Contains(s, c.want) }) {
				t.Errorf("problems %q do not mention %s", p, c.want)
			}
		})
	}
}

func TestQA15_ParseCodexPattern(t *testing.T) {
	for in, want := range map[string][]string{
		`"git", "push"]`:                    {"git push"},
		`"gh", "pr", ["list", "view"]]`:     {"gh pr list", "gh pr view"},
		`"make", ["check", "test-audio"],]`: {"make check", "make test-audio"},
	} {
		p, err := parseList(in)
		if err != nil {
			t.Errorf("parseList(%q): %v", in, err)
			continue
		}
		if got := expand(p); !slices.Equal(got, want) {
			t.Errorf("parseList(%q) = %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{`"git", "push"`, `"git", x]`, `"git`} {
		if _, err := parseList(bad); err == nil {
			t.Errorf("parseList(%q) accepted", bad)
		}
	}
}
