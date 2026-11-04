package repocheck

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// overlapsCommand catches both broad allows and narrower API prefixes whose later
// arguments can change their meaning (QA-15, ADR 0040). This is not a shell parser.
func overlapsCommand(allowed map[string]bool, cmd string) bool {
	for prefix := range allowed {
		if cmd == prefix || strings.HasPrefix(cmd, prefix+" ") || strings.HasPrefix(prefix, cmd+" ") {
			return true
		}
	}
	return false
}

// Native Vibe POSIX readers remain separate from shared command parity. No entry
// here becomes an unsandboxed Codex permission (QA-15, ADR 0040).
var vibeReaders = strings.Fields(`basename cat comm cut date diff dirname du file find
fmt fold grep head join less ls md5sum more nl od paste pwd readlink sha1sum sha256sum
shasum sort stat sum tac tail tr tree uname uniq wc which`)

// Preserve the POSIX 3.0.4 deny defaults when replacing the list, independently of
// the configuration being checked (QA-15, QA-16, ADR 0040).
var vibeDefaultDenies = []string{
	"gdb", "pdb", "passwd", "nano", "vim", "vi", "emacs", "bash -i", "sh -i",
	"zsh -i", "fish -i", "dash -i", "screen", "tmux",
}

// Dedicated readers only: mixed read/write tools must never enter this independent
// inventory just because they have a read mode (QA-15, ADR 0040).
var t3Readers = []string{
	"orchestrator_capabilities", "t3_worktree_status", "t3_worktree_list",
	"t3_thread_list", "t3_thread_read", "t3_thread_search", "t3_project_list",
	"t3_project_read", "list_thread_pull_requests", "read_pull_request_guide", "task_status",
}

// vibeToolConfig models only the fields this project deliberately sets. Undecoded
// keys fail, so typos or new bypass settings cannot silently pass (QA-15, ADR 0040).
type vibeToolConfig struct {
	Permission string   `toml:"permission"`
	Allowlist  []string `toml:"allowlist"`
	Denylist   []string `toml:"denylist"`
}

// vibeConfig keeps outside-file access closed without project path grants
// (QA-15, ADR 0040).
type vibeConfig struct {
	OutsideWorkdir string                    `toml:"outside_workdir"`
	Tools          map[string]vibeToolConfig `toml:"tools"`
}

// parseVibeConfig checks the repository's intentional subset, not the whole native
// schema; omitted fields keep Vibe's defaults (QA-15, ADR 0040).
func parseVibeConfig(data []byte) (vibeConfig, error) {
	var cfg vibeConfig
	md, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return cfg, err
	}
	if len(md.Undecoded()) != 0 {
		return cfg, fmt.Errorf("unexpected Vibe keys: %v", md.Undecoded())
	}
	if cfg.OutsideWorkdir != "deny" || cfg.Tools["bash"].Permission != "ask" {
		return cfg, fmt.Errorf("Vibe must deny outside paths and ask by default for Bash")
	}
	return cfg, nil
}

// commandSet keeps prefix comparisons independent of config ordering (QA-15,
// ADR 0040).
func commandSet(commands []string) map[string]bool {
	out := make(map[string]bool, len(commands))
	for _, cmd := range commands {
		out[cmd] = true
	}
	return out
}

// TestQA15_VibeSharedPolicy preserves shared policy and native defaults without
// granting Codex global file readers (QA-15, ADR 0040).
func TestQA15_VibeSharedPolicy(t *testing.T) {
	cfg, err := parseVibeConfig(readRepoFile(t, ".vibe", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	codex, err := codexCommands(readRepoFile(t, ".codex", "rules", "apptrol.rules"))
	if err != nil {
		t.Fatal(err)
	}
	bash := cfg.Tools["bash"]
	allowed := commandSet(bash.Allowlist)
	if len(allowed) != len(bash.Allowlist) {
		t.Error("duplicate Vibe allow entries")
	}
	expected := commandSet(vibeReaders)
	for cmd := range codex.allow {
		expected[cmd] = true
	}
	if missing, extra := difference(expected, allowed), difference(allowed, expected); len(missing)+len(extra) != 0 {
		t.Errorf("Vibe allowlist: missing %v, extra %v", missing, extra)
	}
	for _, cmd := range mustAsk {
		if overlapsCommand(allowed, cmd) {
			t.Errorf("Vibe must ask about %q", cmd)
		}
	}
	wantDenied := commandSet(append(slices.Clone(vibeDefaultDenies), "docker", "podman", "buildah"))
	denied := commandSet(bash.Denylist)
	if missing, extra := difference(wantDenied, denied), difference(denied, wantDenied); len(missing)+len(extra) != 0 {
		t.Errorf("Vibe denylist: missing %v, extra %v", missing, extra)
	}
	for cmd := range denied {
		if overlapsCommand(allowed, cmd) {
			t.Errorf("Vibe allow overlaps denied command %q", cmd)
		}
	}
}

// TestQA15_RequiredReadersAndSandboxScope requires the approved additions without
// expanding reader file scope to achieve parity (QA-15, ADR 0040).
func TestQA15_RequiredReadersAndSandboxScope(t *testing.T) {
	claude, _, err := claudeCommands(readRepoFile(t, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	codex, err := codexCommands(readRepoFile(t, ".codex", "rules", "apptrol.rules"))
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"git diff", "gh repo list", "gh ruleset list", "gh ruleset view", "gh ruleset check", "gh cache list"} {
		for _, cfg := range []commandLists{claude, codex} {
			if !cfg.allow[cmd] || overlapsCommand(cfg.ask, cmd) || overlapsCommand(cfg.deny, cmd) {
				t.Errorf("%s must be an allowed reader", cmd)
			}
		}
	}
	for _, reader := range append(slices.Clone(vibeReaders), "sed", "rg") {
		if overlapsCommand(codex.allow, reader) || overlapsCommand(claude.allow, reader) {
			t.Errorf("%s must keep native reader scope, without a shared global prefix", reader)
		}
	}
}

// TestQA15_MustAskRejectsBroadAndNarrowAllows exercises unsafe prefixes against the
// independent write inventory, including cache deletion (QA-15, ADR 0040).
func TestQA15_MustAskRejectsBroadAndNarrowAllows(t *testing.T) {
	for _, tc := range []struct{ allow, protected string }{
		{"gh cache", "gh cache delete"}, {"gh", "gh pr create"},
		{"gh api --method GET", "gh api"}, {"git push origin", "git push"},
	} {
		if !slices.Contains(mustAsk, tc.protected) || !overlapsCommand(commandSet([]string{tc.allow}), tc.protected) {
			t.Errorf("unsafe grant %q escaped independent mustAsk %q", tc.allow, tc.protected)
		}
	}
}

// TestQA15_ExactT3ReaderGrants rejects wildcards and mixed/write tools even if both
// configurations acquire them together (QA-15, ADR 0040).
func TestQA15_ExactT3ReaderGrants(t *testing.T) {
	var claude struct{ Permissions struct{ Allow []string } }
	if err := json.Unmarshal(readRepoFile(t, ".claude", "settings.json"), &claude); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, grant := range claude.Permissions.Allow {
		if strings.HasPrefix(grant, "mcp") {
			got = append(got, grant)
		}
	}
	want := make([]string, 0, len(t3Readers))
	for _, name := range t3Readers {
		want = append(want, "mcp__t3-code__"+name)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("Claude MCP grants = %v, want exact readers %v", got, want)
	}
	vibe, err := parseVibeConfig(readRepoFile(t, ".vibe", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	delete(vibe.Tools, "bash")
	for _, name := range t3Readers {
		key := "t3-code_" + name
		cfg := vibe.Tools[key]
		if cfg.Permission != "always" || len(cfg.Allowlist)+len(cfg.Denylist) != 0 {
			t.Errorf("Vibe %s must grant only this exact reader", key)
		}
		delete(vibe.Tools, key)
	}
	if len(vibe.Tools) != 0 {
		t.Errorf("unexpected Vibe tool grants: %v", vibe.Tools)
	}
}

// TestQA15_CodexHostSnippet checks the unapplied host policy without adding a
// partial project server or committing transport credentials (QA-15, ADR 0040).
func TestQA15_CodexHostSnippet(t *testing.T) {
	var cfg struct {
		Servers map[string]struct {
			Default string `toml:"default_tools_approval_mode"`
			Tools   map[string]struct {
				Approval string `toml:"approval_mode"`
			} `toml:"tools"`
		} `toml:"mcp_servers"`
	}
	md, err := toml.Decode(string(readRepoFile(t, ".codex", "t3-mcp-policy.toml.example")), &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(md.Undecoded()) != 0 || len(cfg.Servers) != 1 {
		t.Fatalf("unexpected host snippet keys: %v", md.Undecoded())
	}
	server := cfg.Servers["t3-code"]
	if server.Default != "prompt" || len(server.Tools) != len(t3Readers) {
		t.Fatalf("host must ask by default and grant exactly %d readers", len(t3Readers))
	}
	for _, name := range t3Readers {
		if server.Tools[name].Approval != "approve" {
			t.Errorf("host snippet missing reader %s", name)
		}
	}
}

// TestQA15_MalformedVibeConfig refuses syntax/type errors, typos and changes that
// override safety defaults, rather than silently ignoring them (QA-15, ADR 0040).
func TestQA15_MalformedVibeConfig(t *testing.T) {
	valid := "outside_workdir = 'deny'\n[tools.bash]\npermission = 'ask'\n"
	for _, bad := range []string{
		"[tools.bash", "", "outside_workdir = 7",
		strings.ReplaceAll(valid, "'ask'", "'always'"),
		strings.ReplaceAll(valid, "'deny'", "'ask'"),
		valid + "allowlist = 'git'\n", valid + "allow_list = []\n",
		valid + "denylist_standalone = []\n", valid + "sensitive_patterns = []\n",
		"bypass_tool_permissions = true\n" + valid,
	} {
		if _, err := parseVibeConfig([]byte(bad)); err == nil {
			t.Errorf("accepted malformed or unsafe Vibe config %q", bad)
		}
	}
}
