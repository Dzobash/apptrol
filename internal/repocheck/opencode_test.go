package repocheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"testing"
)

// opencodeRule is one entry of OpenCode's V2 "permissions" array: which tool
// (action), on what (resource: a command, a path, "*"), and the effect (QA-15,
// ADR 0040).
type opencodeRule struct {
	Action, Resource, Effect string
}

// t3Prefixes are the two ways OpenCode may name T3 Code's server ("t3-code"): its
// docs say unsupported characters become "_" but not whether "-" is one of them, so
// opencode.jsonc covers both (QA-15, ADR 0040).
var t3Prefixes = []string{"t3-code_", "t3_code_"}

// parseOpenCodeConfig reads opencode.jsonc strictly: only "$schema" and
// "permissions", each rule exactly action, resource and effect, with a known effect.
// Go's encoding/json matches keys without regard to case, so the keys are compared
// by hand; the V1 key "permission" fails as unknown. OpenCode allows everything
// before this file, so the first rule must be the catch-all shell ask (QA-15,
// ADR 0040).
func parseOpenCodeConfig(data []byte) ([]opencodeRule, error) {
	clean, err := stripJSONC(data)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(clean))
	var top map[string]json.RawMessage
	if err := dec.Decode(&top); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("data after the configuration")
	}
	if keys := slices.Sorted(maps.Keys(top)); !slices.Equal(keys, []string{"$schema", "permissions"}) {
		return nil, fmt.Errorf("top-level keys %v, want only $schema and permissions", keys)
	}
	var raw []map[string]string
	if err := json.Unmarshal(top["permissions"], &raw); err != nil {
		return nil, err
	}
	rules := make([]opencodeRule, 0, len(raw))
	for _, r := range raw {
		if keys := slices.Sorted(maps.Keys(r)); !slices.Equal(keys, []string{"action", "effect", "resource"}) {
			return nil, fmt.Errorf("rule keys %v, want action, resource and effect", keys)
		}
		if !slices.Contains([]string{"allow", "ask", "deny"}, r["effect"]) || r["action"] == "" || r["resource"] == "" {
			return nil, fmt.Errorf("bad rule %v", r)
		}
		rules = append(rules, opencodeRule{r["action"], r["resource"], r["effect"]})
	}
	if len(rules) == 0 || rules[0] != (opencodeRule{"shell", "*", "ask"}) {
		return nil, fmt.Errorf("the first rule must ask about every shell command")
	}
	return rules, nil
}

// opencodeMatches reports whether a rule applies. A shell pattern ending in " *"
// also matches the bare command, as OpenCode's docs say ("git status *" covers
// "git status") (QA-15, ADR 0040).
func opencodeMatches(r opencodeRule, action, resource string) bool {
	if !wildcardMatch(r.Action, action) {
		return false
	}
	if base, ok := strings.CutSuffix(r.Resource, " *"); ok && action == "shell" && wildcardMatch(base, resource) {
		return true
	}
	return wildcardMatch(r.Resource, resource)
}

// opencodeDecide returns what OpenCode does: the effect of the last matching rule,
// or "allow" from its base policy when none of the file's rules matches (QA-15,
// ADR 0040).
func opencodeDecide(rules []opencodeRule, action, resource string) string {
	effect := "allow"
	for _, r := range rules {
		if opencodeMatches(r, action, resource) {
			effect = r.Effect
		}
	}
	return effect
}

// opencodeCommands turns the shell rules into the same lists as the other files,
// each command as the words it starts with. A pattern other than "<command> *" (or
// the catch-all) is a problem: it would match differently from a prefix rule
// (QA-15, ADR 0040).
func opencodeCommands(rules []opencodeRule) (commandLists, []string) {
	lists := commandLists{allow: map[string]bool{}, ask: map[string]bool{}, deny: map[string]bool{}}
	var problems []string
	for i, r := range rules {
		if r.Action != "shell" || (i == 0 && r.Resource == "*") {
			continue
		}
		base, ok := strings.CutSuffix(r.Resource, " *")
		if !ok || strings.ContainsAny(base, "*?") {
			problems = append(problems, fmt.Sprintf("OpenCode shell rule %q: write it as \"<command> *\"", r.Resource))
			continue
		}
		map[string]map[string]bool{"allow": lists.allow, "ask": lists.ask, "deny": lists.deny}[r.Effect][base] = true
	}
	return lists, problems
}

// opencodeCatchAlls are the asks that keep everything no rule names from OpenCode's
// allow-everything default: any shell command, any path outside the repository, any
// T3 tool in either spelling, including tools T3 adds later. Sampled commands, paths
// and tools cannot show that they are whole, so each is required exactly (QA-15,
// ADR 0040).
var opencodeCatchAlls = []opencodeRule{
	{"shell", "*", "ask"},
	{"external_directory", "*", "ask"},
	{"t3-code_*", "*", "ask"},
	{"t3_code_*", "*", "ask"},
}

// requireCatchAlls returns an error unless every catch-all ask is in the rules, exactly,
// and before every allow it covers: the last match wins, so a catch-all after an allow
// would override it (QA-15, ADR 0040).
func requireCatchAlls(rules []opencodeRule) error {
	for _, c := range opencodeCatchAlls {
		at := slices.Index(rules, c)
		if at < 0 {
			return fmt.Errorf("OpenCode catch-all %v is missing or narrowed", c)
		}
		for _, r := range rules[:at] {
			if r.Effect == "allow" && wildcardMatch(c.Action, r.Action) {
				return fmt.Errorf("OpenCode catch-all %v comes after allow %v and overrides it", c, r)
			}
		}
	}
	return nil
}

// openCodeRules reads the repository's opencode.jsonc, requires its catch-all asks and
// returns its shell lists (QA-15, ADR 0040).
func openCodeRules(t *testing.T) ([]opencodeRule, commandLists) {
	t.Helper()
	rules, err := parseOpenCodeConfig(readRepoFile(t, "opencode.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	if err := requireCatchAlls(rules); err != nil {
		t.Error(err)
	}
	lists, problems := opencodeCommands(rules)
	for _, p := range problems {
		t.Error(p)
	}
	return rules, lists
}

// TestQA15_OpenCodeSharedPolicy: OpenCode allows, asks about and blocks the same
// shared commands as Codex (and so Claude Code), with or without arguments; any other
// shell command and any path outside the repository asks; reads and edits inside it
// keep OpenCode's defaults (QA-15, QA-16, ADR 0040).
func TestQA15_OpenCodeSharedPolicy(t *testing.T) {
	rules, lists := openCodeRules(t)
	codex, err := codexCommands(readRepoFile(t, ".codex", "rules", "apptrol.rules"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []struct {
		name            string
		opencode, codex map[string]bool
	}{
		{"allowed", lists.allow, codex.allow},
		{"asked about", lists.ask, codex.ask},
		{"blocked", lists.deny, codex.deny},
	} {
		if missing, extra := difference(d.codex, d.opencode), difference(d.opencode, d.codex); len(missing)+len(extra) != 0 {
			t.Errorf("OpenCode %s: missing %v, extra %v", d.name, missing, extra)
		}
	}
	// What OpenCode decides, in rule order: the lists alone would not show a broad
	// allow placed after an ask.
	for cmd := range codex.allow {
		for _, c := range []string{cmd, cmd + " --example"} {
			if got := opencodeDecide(rules, "shell", c); got != "allow" {
				t.Errorf("OpenCode %q = %s, want allow", c, got)
			}
		}
	}
	for _, cmd := range mustAsk {
		for _, c := range []string{cmd, cmd + " --example"} {
			if got := opencodeDecide(rules, "shell", c); got != "ask" {
				t.Errorf("OpenCode %q = %s, want ask", c, got)
			}
		}
	}
	unlisted := append(slices.Clone(vibeReaders), "sed -n 1p x", "rg x", "rm -rf x", "curl x",
		"git branch -v -d x", "git branch -r -D x", "sudo podman ps", "bash -c x")
	for _, c := range unlisted {
		if got := opencodeDecide(rules, "shell", c); got != "ask" {
			t.Errorf("OpenCode %q = %s, want ask", c, got)
		}
	}
	if got := opencodeDecide(rules, "external_directory", "/elsewhere/*"); got != "ask" {
		t.Errorf("OpenCode outside the repository = %s, want ask", got)
	}
	for _, r := range rules {
		isT3 := slices.ContainsFunc(t3Prefixes, func(p string) bool { return strings.HasPrefix(r.Action, p) })
		switch {
		case r.Action == "shell", isT3:
		case r.Action == "external_directory" && r.Effect == "ask":
		default:
			t.Errorf("OpenCode rule %v: only shell, T3 and an external_directory ask belong here", r)
		}
	}
}

// TestQA15_OpenCodeExactT3Grants: OpenCode allows exactly the chosen T3 tools, by
// exact name in both spellings; every other T3 tool, known or new, asks (QA-15,
// ADR 0040).
func TestQA15_OpenCodeExactT3Grants(t *testing.T) {
	rules, _ := openCodeRules(t)
	want := map[string]bool{}
	for _, p := range t3Prefixes {
		for _, name := range t3Granted {
			want[p+name] = true
			if got := opencodeDecide(rules, p+name, "*"); got != "allow" {
				t.Errorf("OpenCode %s = %s, want allow", p+name, got)
			}
		}
		for _, name := range append(slices.Clone(t3MustAsk), "a_tool_added_later") {
			if got := opencodeDecide(rules, p+name, "*"); got != "ask" {
				t.Errorf("OpenCode %s = %s, want ask", p+name, got)
			}
		}
	}
	got := map[string]bool{}
	for _, r := range rules {
		if r.Action != "shell" && r.Effect == "allow" {
			got[r.Action] = true
		}
	}
	if missing, extra := difference(want, got), difference(got, want); len(missing)+len(extra) != 0 {
		t.Errorf("OpenCode T3 grants: missing %v, extra %v", missing, extra)
	}
}

// TestQA15_OpenCodeCatchAllsAreRequired: the real file asks about commands, paths and
// T3 tools no other test names; narrowing, dropping or moving any catch-all in its text
// fails the check, and where the change opens a hole, the evaluator shows it allowed
// (QA-15, ADR 0040).
func TestQA15_OpenCodeCatchAllsAreRequired(t *testing.T) {
	data := string(readRepoFile(t, "opencode.jsonc"))
	rules, err := parseOpenCodeConfig([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := requireCatchAlls(rules); err != nil {
		t.Fatal(err)
	}
	type probe struct{ action, resource string }
	unsampled := map[string]probe{
		"shell":     {"shell", "some-future-command --flag"},
		"outside":   {"external_directory", "/opt/unsampled/data/*"},
		"t3-code_*": {"t3-code_some_future_tool", "*"},
		"t3_code_*": {"t3_code_some_future_tool", "*"},
	}
	for name, p := range unsampled {
		if got := opencodeDecide(rules, p.action, p.resource); got != "ask" {
			t.Errorf("%s: OpenCode %s %q = %s, want ask", name, p.action, p.resource, got)
		}
	}
	line := func(action, resource, effect string) string {
		return fmt.Sprintf("    { \"action\": %q, \"resource\": %q, \"effect\": %q },\n", action, resource, effect)
	}
	shell, ext := line("shell", "*", "ask"), line("external_directory", "*", "ask")
	dash, under := line("t3-code_*", "*", "ask"), line("t3_code_*", "*", "ask")
	const last = "    // 4."
	for _, m := range []struct {
		name    string
		changes [][2]string // old, new, applied in order
		hole    string      // a key of unsampled that the change leaves allowed, or ""
	}{
		{"shell narrowed", [][2]string{{shell, line("shell", "git *", "ask")}}, ""},
		{"shell dropped", [][2]string{{shell, ""}}, ""},
		{"outside narrowed", [][2]string{{ext, line("external_directory", "/elsewhere/*", "ask")}}, "outside"},
		{"outside allowed", [][2]string{{ext, line("external_directory", "*", "allow")}}, "outside"},
		{"outside dropped", [][2]string{{ext, ""}}, "outside"},
		{"outside allow before the ask", [][2]string{{ext, line("external_directory", "/opt/*", "allow") + ext}}, ""},
		{"t3-code_* as exact asks", [][2]string{{dash, line("t3-code_delete_scheduled_task", "*", "ask") +
			line("t3-code_schedule_task", "*", "ask")}}, "t3-code_*"},
		{"t3_code_* as exact asks", [][2]string{{under, line("t3_code_delete_scheduled_task", "*", "ask")}}, "t3_code_*"},
		{"t3-code_* resource narrowed", [][2]string{{dash, line("t3-code_*", "none", "ask")}}, "t3-code_*"},
		{"t3_code_* dropped", [][2]string{{under, ""}}, "t3_code_*"},
		{"t3-code_* after the allows", [][2]string{{dash, ""}, {last, dash + last}}, ""},
		{"t3_code_* after the allows", [][2]string{{under, ""}, {last, under + last}}, ""},
	} {
		t.Run(m.name, func(t *testing.T) {
			text := data
			for _, c := range m.changes {
				if strings.Count(text, c[0]) != 1 {
					t.Fatalf("the file no longer has exactly one %q; update this test", c[0])
				}
				text = strings.Replace(text, c[0], c[1], 1)
			}
			changed, err := parseOpenCodeConfig([]byte(text))
			if err == nil {
				err = requireCatchAlls(changed)
			}
			if err == nil {
				t.Error("the check passed")
			}
			if m.hole != "" && changed != nil {
				if p := unsampled[m.hole]; opencodeDecide(changed, p.action, p.resource) != "allow" {
					t.Errorf("the change should leave %s %q allowed; the test no longer shows the hole", p.action, p.resource)
				}
			}
		})
	}
}

// TestQA15_OpenCodeLastMatchWins: the evaluator follows OpenCode's documented
// example, so the repository's checks mean what OpenCode does (QA-15, ADR 0040).
func TestQA15_OpenCodeLastMatchWins(t *testing.T) {
	docs := []opencodeRule{
		{"shell", "*", "ask"},
		{"shell", "git status *", "allow"},
		{"shell", "git push *", "deny"},
	}
	for cmd, want := range map[string]string{
		"git status": "allow", "git status --short": "allow", "git statusx": "ask",
		"git push": "deny", "git push origin main": "deny", "ls": "ask",
	} {
		if got := opencodeDecide(docs, "shell", cmd); got != want {
			t.Errorf("%q = %s, want %s", cmd, got, want)
		}
	}
	reversed := []opencodeRule{docs[2], docs[1], docs[0]}
	if got := opencodeDecide(reversed, "shell", "git push"); got != "ask" {
		t.Errorf("a later catch-all must win, got %s", got)
	}
	if got := opencodeDecide(nil, "read", "x"); got != "allow" {
		t.Errorf("no rule must fall back to OpenCode's allow, got %s", got)
	}
	for _, tc := range []struct {
		pattern, s string
		want       bool
	}{
		{"t3-code_*", "t3-code_device_open", true}, {"t3-code_*", "t3_code_device_open", false},
		{"a?c", "abc", true}, {"a?c", "ac", false}, {"*", "", true}, {"a*b*c", "a-b-x-c", true},
	} {
		if got := wildcardMatch(tc.pattern, tc.s); got != tc.want {
			t.Errorf("wildcardMatch(%q, %q) = %v", tc.pattern, tc.s, got)
		}
	}
}

// TestQA15_MalformedOpenCodeConfig: the strict parse keeps comments apart from
// strings and refuses broken JSONC, the V1 format, unknown keys and a file that does
// not start by asking (QA-15, ADR 0040).
func TestQA15_MalformedOpenCodeConfig(t *testing.T) {
	const first = `{"action": "shell", "resource": "*", "effect": "ask"}`
	valid := "// note\n{\"$schema\": \"https://opencode.ai/config.json\", /* x */ \"permissions\": [" + first + "]}\n"
	rules, err := parseOpenCodeConfig([]byte(valid))
	if err != nil || len(rules) != 1 {
		t.Fatalf("minimal valid config: %v, %v", rules, err)
	}
	for _, bad := range []string{
		"", "{", valid + "{}", strings.Replace(valid, "/* x */", "/* x", 1),
		strings.Replace(valid, "]}", ",]}", 1),
		strings.Replace(valid, `"permissions"`, `"permission"`, 1),
		strings.Replace(valid, `"permissions"`, `"Permissions"`, 1),
		strings.Replace(valid, `"$schema": "https://opencode.ai/config.json",`, "", 1),
		strings.Replace(valid, "[", `[{"action": "shell", "resource": "*", "effect": "allow"},`, 1),
		strings.Replace(valid, `"ask"`, `"always"`, 1),
		strings.Replace(valid, `"effect"`, `"Effect"`, 1),
		strings.Replace(valid, `"ask"}`, `"ask", "agent": "build"}`, 1),
		strings.Replace(valid, "]}", `], "agents": {}}`, 1),
		`{"$schema": "https://opencode.ai/config.json", "permissions": []}`,
	} {
		if _, err := parseOpenCodeConfig([]byte(bad)); err == nil {
			t.Errorf("accepted malformed or unsafe OpenCode config %q", bad)
		}
	}
	lists, problems := opencodeCommands([]opencodeRule{{"shell", "*", "ask"}, {"shell", "git *push *", "ask"}, {"shell", "git log", "allow"}})
	if len(problems) != 2 || len(lists.allow)+len(lists.ask) != 0 {
		t.Errorf("patterns other than \"<command> *\" passed: %v, %v", lists, problems)
	}
}

// stripJSONC removes // and /* */ comments outside strings, so encoding/json can read
// opencode.jsonc without a new dependency. Newlines in comments are kept, so a parse
// error still points at the right line (QA-15, ADR 0040).
func stripJSONC(data []byte) ([]byte, error) {
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		c := data[i]
		switch {
		case c == '"':
			start := i
			for i++; i < len(data) && data[i] != '"'; i++ {
				if data[i] == '\\' {
					i++
				}
				if i < len(data) && data[i] == '\n' {
					return nil, fmt.Errorf("line break inside a string")
				}
			}
			if i >= len(data) {
				return nil, fmt.Errorf("unclosed string")
			}
			out = append(out, data[start:i+1]...)
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			if i < len(data) {
				out = append(out, '\n')
			}
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			i += 2
			for ; i+1 < len(data) && (data[i] != '*' || data[i+1] != '/'); i++ {
				if data[i] == '\n' {
					out = append(out, '\n')
				}
			}
			if i+1 >= len(data) {
				return nil, fmt.Errorf("unclosed comment")
			}
			i++
		default:
			out = append(out, c)
		}
	}
	return out, nil
}

// wildcardMatch matches a whole value against an OpenCode pattern: "*" stands for
// any text, "?" for one character, everything else for itself (QA-15, ADR 0040).
func wildcardMatch(pattern, s string) bool {
	p, v := 0, 0
	star, mark := -1, 0
	for v < len(s) {
		switch {
		case p < len(pattern) && (pattern[p] == '?' || pattern[p] == s[v]):
			p++
			v++
		case p < len(pattern) && pattern[p] == '*':
			star, mark = p, v
			p++
		case star >= 0:
			p = star + 1
			mark++
			v = mark
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}
