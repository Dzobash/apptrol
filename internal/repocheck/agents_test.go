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
// must ask about (ask), each as the words it starts with, e.g. "gh pr view".
type commandLists struct {
	allow, ask map[string]bool
}

// claudeCommands reads Claude Code's shared settings. "Bash(git log *)" and
// "Bash(git log)" both stand for "git log"; a trailing "*" only means "with any
// arguments", like a Codex prefix rule (QA-15).
func claudeCommands(t *testing.T) commandLists {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Permissions struct{ Allow, Ask []string }
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf(".claude/settings.json: %v", err)
	}
	words := func(rules []string) map[string]bool {
		out := map[string]bool{}
		for _, r := range rules {
			cmd, ok := strings.CutPrefix(r, "Bash(")
			if !ok || !strings.HasSuffix(cmd, ")") {
				continue // other tools' rules (Read, WebFetch, …) have no Codex twin
			}
			cmd = strings.TrimSuffix(strings.TrimSuffix(cmd, ")"), "*")
			out[strings.TrimSpace(cmd)] = true
		}
		return out
	}
	return commandLists{allow: words(s.Permissions.Allow), ask: words(s.Permissions.Ask)}
}

var (
	ruleStart   = regexp.MustCompile(`(?m)^prefix_rule\(`)
	patternExpr = regexp.MustCompile(`pattern\s*=\s*\[`)
	decisionArg = regexp.MustCompile(`decision\s*=\s*"(\w+)"`)
)

// codexCommands reads the Codex rules. A pattern element that is a list holds
// alternatives, so ["gh", "pr", ["list", "view"]] stands for "gh pr list" and
// "gh pr view"; "prompt" is Codex's word for ask (QA-15).
func codexCommands(t *testing.T) commandLists {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".codex", "rules", "apptrol.rules"))
	if err != nil {
		t.Fatal(err)
	}
	lists := commandLists{allow: map[string]bool{}, ask: map[string]bool{}}
	starts := ruleStart.FindAllStringIndex(string(data), -1)
	for i, s := range starts {
		end := len(data)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		rule := string(data[s[1]:end])
		loc := patternExpr.FindStringIndex(rule)
		if loc == nil {
			t.Fatalf("apptrol.rules: a prefix_rule without a pattern: %.60q", rule)
		}
		pattern, err := parseList(rule[loc[1]:])
		if err != nil {
			t.Fatalf("apptrol.rules: %v in %.60q", err, rule)
		}
		target := lists.allow // Codex's default decision
		if m := decisionArg.FindStringSubmatch(rule); m != nil {
			switch m[1] {
			case "allow":
			case "prompt":
				target = lists.ask
			default:
				t.Fatalf("apptrol.rules: decision %q has no Claude Code twin", m[1])
			}
		}
		for _, cmd := range expand(pattern) {
			target[cmd] = true
		}
	}
	return lists
}

// parseList reads a Starlark list of strings and lists of strings, starting just
// after its "[", e.g. `"git", ["status", "log"]]`.
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

// expand turns a pattern with alternatives into every command it matches.
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

// difference lists what a has and b lacks, sorted.
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

// TestQA15_AgentCommandListsMatch keeps the two tools' lists equal, so Claude Code
// and Codex may run, and must ask about, the same commands (QA-15).
func TestQA15_AgentCommandListsMatch(t *testing.T) {
	claude, codex := claudeCommands(t), codexCommands(t)
	if len(claude.allow) < 10 || len(codex.allow) < 10 {
		t.Fatalf("found only %d (Claude) and %d (Codex) allowed commands; did a file change shape?",
			len(claude.allow), len(codex.allow))
	}
	for _, d := range []struct {
		name          string
		claude, codex map[string]bool
	}{
		{"allowed", claude.allow, codex.allow},
		{"asked about", claude.ask, codex.ask},
	} {
		if missing := difference(d.claude, d.codex); len(missing) > 0 {
			t.Errorf("%s for Claude, not for Codex: %s", d.name, strings.Join(missing, ", "))
		}
		if missing := difference(d.codex, d.claude); len(missing) > 0 {
			t.Errorf("%s for Codex, not for Claude: %s", d.name, strings.Join(missing, ", "))
		}
	}
}

// TestQA15_ChangesOutsideTheCheckoutAsk: pushing and changing GitHub need the
// owner's OK (AGENTS.md › Workflow), so both tools must ask, whatever else a
// personal settings file allows.
func TestQA15_ChangesOutsideTheCheckoutAsk(t *testing.T) {
	claude, codex := claudeCommands(t), codexCommands(t)
	for _, cmd := range []string{"git push", "gh pr create", "gh pr ready", "gh pr merge", "gh issue create", "gh api"} {
		if !claude.ask[cmd] || !codex.ask[cmd] {
			t.Errorf("%q must ask in both tools (Claude %v, Codex %v)", cmd, claude.ask[cmd], codex.ask[cmd])
		}
		if claude.allow[cmd] || codex.allow[cmd] {
			t.Errorf("%q must not be allowed", cmd)
		}
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
