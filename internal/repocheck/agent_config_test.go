package repocheck

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// overlapsCommand catches both broad allows ("gh cache" covers "gh cache delete")
// and narrower prefixes whose later arguments can change their meaning ("gh api
// --method GET" can still write), so neither can bypass an ask rule (QA-15,
// ADR 0040). This is not a shell parser.
func overlapsCommand(allowed map[string]bool, cmd string) bool {
	for prefix := range allowed {
		if cmd == prefix || strings.HasPrefix(cmd, prefix+" ") || strings.HasPrefix(prefix, cmd+" ") {
			return true
		}
	}
	return false
}

// vibeReaders are Vibe 3.0.4's own POSIX read-only defaults, kept because a
// configured allowlist replaces them. They run inside Vibe's native path and option
// checks; none of them becomes an unsandboxed Codex or Claude prefix (QA-15,
// ADR 0040).
var vibeReaders = strings.Fields(`basename cat comm cut date diff dirname du file find
fmt fold grep head join less ls md5sum more nl od paste pwd readlink sha1sum sha256sum
shasum sort stat sum tac tail tr tree uname uniq wc which`)

// vibeDefaultDenies are Vibe 3.0.4's POSIX deny defaults, which a configured
// denylist replaces too, so they must stay next to the container engines (QA-15,
// QA-16, ADR 0040).
var vibeDefaultDenies = []string{
	"gdb", "pdb", "passwd", "nano", "vim", "vi", "emacs", "bash -i", "sh -i",
	"zsh -i", "fish -i", "dash -i", "screen", "tmux",
}

// t3Granted are the T3 MCP tools the owner chose to run without a prompt: reads,
// thread tidying, orchestration, waiting and PR tracking. Starting agents is still
// governed by AGENTS.md › Set up (QA-15, ADR 0040).
var t3Granted = []string{
	// reads
	"orchestrator_capabilities", "t3_worktree_status", "t3_worktree_list",
	"t3_thread_list", "t3_thread_read", "t3_thread_search", "t3_project_list",
	"t3_project_read", "list_thread_pull_requests", "read_pull_request_guide", "task_status",
	"t3_environment_read", "t3_preview_list", "preview_status", "list_scheduled_tasks",
	// thread tidying
	"t3_thread_update", "t3_thread_organize",
	// orchestration
	"t3_thread_launch", "create_threads", "delegate_task", "task_cancel", "t3_thread_send",
	"t3_thread_interrupt", "t3_queue", "t3_workflow_stop", "t3_worktree_handoff",
	// waiting and PR tracking in T3 (no change on GitHub)
	"t3_thread_wait", "t3_background_wait", "link_pull_request", "unlink_pull_request",
	"watch_pull_request", "unwatch_pull_request",
}

// t3MustAsk are T3 tools that must keep asking, kept here on its own so adding one
// to t3Granted and both files together still fails: mixed tools, deleting,
// scheduling, secrets, T3 settings, the owner's screen and devices, browser actions
// and attachments. It follows T3's tool list as of 2026-11-04 (QA-15, ADR 0040).
var t3MustAsk = []string{
	"t3_thread_configure", "t3_pending_request", "t3_thread_transfer",
	"t3_project_create", "t3_project_clone", "t3_project_update", "t3_project_delete",
	"schedule_task", "run_scheduled_task_now", "delete_scheduled_task",
	"request_secret", "t3_environment_preferences_update", "write_pull_request_guides",
	// devices and the owner's screen
	"device_list", "device_open", "device_close", "device_screenshot",
	// browser actions
	"preview_open", "preview_navigate", "preview_act", "preview_evaluate", "preview_dialog",
	"preview_resize", "preview_set_appearance", "preview_snapshot", "preview_wait_for",
	"preview_recording_start", "preview_recording_stop", "t3_preview_close",
	"html_preview", "html_render",
	// attachments
	"t3_attachment_prepare_upload", "t3_attachment_discard", "t3_thread_send_attachments",
}

// vibeToolConfig holds only the fields this project sets. Any other key fails the
// parse, so a typo or a setting that switches a safeguard off cannot pass unnoticed
// (QA-15, ADR 0040).
type vibeToolConfig struct {
	Permission string   `toml:"permission"`
	Allowlist  []string `toml:"allowlist"`
	Denylist   []string `toml:"denylist"`
}

// parseVibeConfig reads .vibe/config.toml strictly and requires Bash to ask by
// default; everything it does not set keeps Vibe's defaults (QA-15, ADR 0040).
func parseVibeConfig(data []byte) (map[string]vibeToolConfig, error) {
	var cfg struct {
		Tools map[string]vibeToolConfig `toml:"tools"`
	}
	md, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return nil, err
	}
	if len(md.Undecoded()) != 0 {
		return nil, fmt.Errorf("unexpected Vibe keys: %v", md.Undecoded())
	}
	if cfg.Tools["bash"].Permission != "ask" {
		return nil, fmt.Errorf("Vibe's Bash must ask by default")
	}
	return cfg.Tools, nil
}

// commandSet turns a list into a set, for comparisons that ignore order (QA-15).
func commandSet(commands []string) map[string]bool {
	out := make(map[string]bool, len(commands))
	for _, cmd := range commands {
		out[cmd] = true
	}
	return out
}

// TestQA15_VibeSharedPolicy: Vibe allows exactly the shared commands plus its own
// readers, asks about every push and GitHub change, and blocks the container
// engines on top of its own deny defaults (QA-15, QA-16, ADR 0040).
func TestQA15_VibeSharedPolicy(t *testing.T) {
	tools, err := parseVibeConfig(readRepoFile(t, ".vibe", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	codex, err := codexCommands(readRepoFile(t, ".codex", "rules", "apptrol.rules"))
	if err != nil {
		t.Fatal(err)
	}
	bash := tools["bash"]
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

// TestQA15_RequiredReadersAndSandboxScope: the added GitHub readers run without a
// prompt in Claude Code and Codex, and no shell reader (sed, rg, grep, …) becomes a
// global prefix there, so Codex's readers stay in its sandbox (QA-15, ADR 0040).
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

// TestQA15_NoBranchSwitchBeforeADelete: a rule sees only the switch right after
// "git branch", and git still deletes after -v, -vv or -r (`git branch -v -d x`);
// --merged, --no-merged and --contains take a value, then the same. So none of
// these may be allowed in any file; `git branch --list …` refuses -d (QA-15).
func TestQA15_NoBranchSwitchBeforeADelete(t *testing.T) {
	claude, _, err := claudeCommands(readRepoFile(t, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	codex, err := codexCommands(readRepoFile(t, ".codex", "rules", "apptrol.rules"))
	if err != nil {
		t.Fatal(err)
	}
	tools, err := parseVibeConfig(readRepoFile(t, ".vibe", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	vibe := commandSet(tools["bash"].Allowlist)
	for _, sw := range []string{"-v", "-vv", "-r", "--merged", "--no-merged", "--contains"} {
		cmd := "git branch " + sw + " -d"
		if overlapsCommand(claude.allow, cmd) || overlapsCommand(codex.allow, cmd) || overlapsCommand(vibe, cmd) {
			t.Errorf("%q must ask: an allow rule covers it", cmd)
		}
	}
}

// TestQA15_MustAskRejectsBroadAndNarrowAllows: overlapsCommand catches the unsafe
// grants it exists for, each against a command in the independent mustAsk list
// (QA-15, ADR 0040).
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

// TestQA15_ExactT3Grants: Claude Code and Vibe preapprove exactly the chosen T3
// tools, by name; a wildcard or any other MCP grant fails, and no tool in t3MustAsk
// may be granted (QA-15, ADR 0040).
func TestQA15_ExactT3Grants(t *testing.T) {
	for _, name := range t3MustAsk {
		if slices.Contains(t3Granted, name) {
			t.Errorf("T3 tool %s must keep asking", name)
		}
	}
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
	want := make([]string, 0, len(t3Granted))
	for _, name := range t3Granted {
		want = append(want, "mcp__t3-code__"+name)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("Claude MCP grants = %v, want exactly %v", got, want)
	}
	tools, err := parseVibeConfig(readRepoFile(t, ".vibe", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	delete(tools, "bash")
	for _, name := range t3Granted {
		key := "t3-code_" + name
		cfg := tools[key]
		if cfg.Permission != "always" || len(cfg.Allowlist)+len(cfg.Denylist) != 0 {
			t.Errorf("Vibe %s must grant only this exact tool", key)
		}
		delete(tools, key)
	}
	if len(tools) != 0 {
		t.Errorf("unexpected Vibe tool grants: %v", tools)
	}
}

// TestQA15_MalformedVibeConfig: the strict parse refuses broken TOML, Bash that
// stops asking, and keys that would replace or switch off Vibe's safeguards
// (QA-15, ADR 0040).
func TestQA15_MalformedVibeConfig(t *testing.T) {
	valid := "[tools.bash]\npermission = 'ask'\n"
	if _, err := parseVibeConfig([]byte(valid)); err != nil {
		t.Fatalf("minimal valid config refused: %v", err)
	}
	for _, bad := range []string{
		"[tools.bash", "",
		strings.ReplaceAll(valid, "'ask'", "'always'"),
		valid + "allowlist = 'git'\n", valid + "allow_list = []\n",
		valid + "denylist_standalone = []\n", valid + "sensitive_patterns = []\n",
		"bypass_tool_permissions = true\n" + valid,
	} {
		if _, err := parseVibeConfig([]byte(bad)); err == nil {
			t.Errorf("accepted malformed or unsafe Vibe config %q", bad)
		}
	}
}
