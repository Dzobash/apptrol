# 0040. Where agent rules live

- **Status:** Accepted
- **Date:** 2026-10-27

Decided by the owner in [#200](https://github.com/Dzobash/apptrol/issues/200).

## Context

The rules for coding agents grew one incident at a time: a rule was added where the
problem showed up, in AGENTS.md, CONTRIBUTING.md, a skill, a command or a template. Some
rules ended up in two or three places, and the copies drifted apart (when to open an
issue, when to delete a branch). AGENTS.md itself was grouped by topic, so an agent had
to read all of it to find what applies to the step it is on.

Several agents work on the repository, from more than one provider. Each keeps private
notes that the others cannot see. A rule written only there holds for one agent and is
invisible to the rest.

#200 audited the rules as a whole. Two independent audits, by models of different
providers, each without the other's findings, were merged afterwards; a finding seen by
both was marked as such.

## Decision

Every rule has one home. Every other place links to it instead of repeating it.

```
AGENTS.md          how agents work, in the order of a task
CONTRIBUTING.md    what every contributor does: issues, commits, code, docs, logs
SECURITY.md        private reporting
docs/testing.md,   checks; the manual test run
  the how-to
docs/releasing.md  candidates, release, tags
ADRs               why; dated notes when a decision changes
CLAUDE.md, skills, entry points that link, no rules of their own
  commands
settings files     enforce the command list; tests keep them in step
templates          ask for evidence, link to the rules
agents' private    no rules
  notes
```

AGENTS.md follows the life of a task, with its boundaries (privacy, files, commands)
first, as they hold at every step:

```
start → plan → set up → build → review → publish → ready → clean up → hand over, end
```

A rule an agent learns in a session goes to its home in the repository, not only into
its own notes.

## Consequences

- An agent reads the section for the step it is on; boundaries hold at every step.
- A rule changes in one place. A copy found elsewhere is replaced by a link.
- Links to AGENTS.md name its sections (e.g. AGENTS.md › Publish); moving a rule means
  updating them.
- Where two copies said different things, the difference is decided on its own, in a
  later change, not while moving the text.

## 2026-11-03 — Local review before every push

The owner chose to finish all agent review rounds locally before pushing, including
routine docs and tests and later fixes. This lets review findings be addressed before
publishing each new commit. A clean verdict names the exact commit and carries into
the PR description, avoiding a duplicate review merely because it was pushed. GitHub
PRs and CI remain. The shared workflow lives in
[AGENTS.md › Review](https://github.com/Dzobash/apptrol/blob/main/AGENTS.md#review),
[Publish](https://github.com/Dzobash/apptrol/blob/main/AGENTS.md#publish) and
[Ready](https://github.com/Dzobash/apptrol/blob/main/AGENTS.md#ready).

## Permission configuration — 2026-11-04

The owner wanted fewer needless prompts, while pushing and changes on GitHub keep
asking. Decided:

```
short read-only pipelines   allowed when every part is on the list (AGENTS.md)
gh repo list, gh ruleset    allowed in all three files
  list/view/check,
  gh cache list
gh cache delete, gh api     ask; a prefix cannot tell gh api's reads from writes
Mistral Vibe                .vibe/config.toml: shared commands + Vibe's own readers
T3 tools                    chosen groups granted by exact name for Claude Code
                              and Vibe; the rest asks
```

- **Shell readers stay native.** `head`, `grep`, `sed` and the like get no global
  prefix in the Claude or Codex files; in Codex they keep running in its sandbox.
  `sed -n` can still write or run commands (`w`, `e`), so it is no read-only prefix.
  Codex checks each part of a simple chain on its own, so an allowed `git diff` at the
  start does not allow what follows. Claude Code does the same: its
  [permission docs](https://code.claude.com/docs/en/permissions#compound-commands)
  require every subcommand to be allowed, and an ask or deny rule applies when any
  subcommand matches; `head`, `tail`, `grep` and `wc` are in its built-in read-only
  set, so `git log -5 | head -n 3` runs without a prompt. How Grok matches a chain against `.claude/settings.json` was not
  checked, so Grok runs one command per call.
- **Vibe 3.0.4.** Its
  [Bash tool](https://github.com/mistralai/mistral-vibe/blob/v3.0.4/vibe/core/tools/builtins/bash.py)
  requires every part of a command to match and replaces its default lists with the
  configured ones. So the file repeats Vibe's POSIX readers and deny defaults and adds
  `docker`, `podman` and `buildah`. It leaves everything else at Vibe's defaults,
  including access outside the workdir, `denylist_standalone` and
  `sensitive_patterns`, and grants no paths. The
  [project loader](https://github.com/mistralai/mistral-vibe/blob/v3.0.4/vibe/core/config/layers/project.py)
  reads it only in a trusted folder.
- **T3 tools.** The owner chose five groups to run without a prompt, because T3
  orchestration asked about almost every step:
  ```
  reads           orchestrator_capabilities, t3_worktree_status/list,
                  t3_thread_list/read/search, t3_project_list/read,
                  list_thread_pull_requests, read_pull_request_guide, task_status,
                  t3_environment_read, t3_preview_list, preview_status,
                  list_scheduled_tasks
  thread tidying  t3_thread_update, t3_thread_organize
  orchestration   t3_thread_launch, create_threads, delegate_task, task_cancel,
                  t3_thread_send, t3_thread_interrupt, t3_queue, t3_workflow_stop,
                  t3_worktree_handoff
  waiting         t3_thread_wait, t3_background_wait
  PR tracking     link/unlink_pull_request, watch/unwatch_pull_request (T3 only,
                  no change on GitHub)
  ```
  Claude Code names them `mcp__t3-code__<tool>`, Vibe `t3-code_<tool>` (T3's server
  alias, then the tool). No wildcard. Still asking: deleting, scheduling, secrets,
  devices and screenshots, browser actions, T3 settings, `t3_thread_transfer` and
  the mixed `t3_thread_configure` and `t3_pending_request`. Without the prompt, only
  AGENTS.md › Set up keeps an agent from starting threads the owner did not ask for.

- **`git branch` read switches.** A rule sees only the switch right after
  `git branch`, and git still deletes after `-v`, `-vv` or `-r`
  (`git branch -v -d x`). So only `--list`, `--show-current` and `-a`, which refuse
  `-d`, are allowed; `git branch --list -vv` shows the same as `git branch -vv`.

Limits:

- **Codex** gets no T3 grants. Its MCP approvals live on the server entry, which T3
  supplies at runtime with the connection details; a project file holding only the
  approvals fails Codex's validation, and T3's own entry replaces it. So T3 tools in
  Codex keep T3's approval behaviour. Changing that is T3's work, not this repository's.
- **Host grants** are not capped by the project's: T3 may preapprove more for a
  session itself.
- **QA-15's tests check the files** (shared policy, asks, blocks, exact readers). They
  do not show that an installed tool loaded them, and a prefix rule still does not
  check arguments.

## OpenCode — 2026-11-09

OpenCode joins as a fourth client. Its rules live in `opencode.jsonc` at the
repository's root, with the same shared commands, asks, blocks and T3 grants as the
other three files (QA-15, QA-16).

OpenCode works differently from the other tools in two ways that shape the file:

- **It allows everything by default.** Every agent starts with a base policy of
  `{"action": "*", "resource": "*", "effect": "allow"}`, and access outside the
  project (`external_directory`) is allowed too. Without rules of its own, an agent
  could run any command and read any file in the home folder.
- **The last matching rule wins.** OpenCode puts the base policy first and the
  file's rules after it. So the file starts with broad asks and lists every exception
  after them; an allow placed after an ask would override it.

```
base policy  * allow                       OpenCode's own, before the file
1. ask       shell *                       any command
             external_directory *          any path outside the repository
             t3-code_*, t3_code_*          any T3 tool
2. allow     git status *, gh pr view *, … the shared commands
             t3-code_t3_thread_read, …     the chosen T3 tools, by exact name
3. ask       git push *, gh api *, …       pushing, branch deletes, GitHub changes
4. deny      docker *, podman *, buildah *

"git push origin" → matches 1 (ask) and 3 (ask)  → ask
"git status -s"   → matches 1 (ask) and 2 (allow) → allow
"podman ps"       → matches 1 (ask) and 4 (deny)  → deny
```

- **Shell patterns** match the whole command; `*` matches any text, and a pattern
  ending in ` *` also matches the bare command (`git status *` covers `git status`).
  OpenCode's docs say it checks each part of a chain on its own, like Claude Code,
  Codex and Vibe (not verified, see below).
- **Reads and edits inside the repository** keep OpenCode's defaults (option A in the
  owner's plan): no prompt, except reading `.env` files, which OpenCode asks about by
  itself. Shell readers (`cat`, `grep`, …) get no rule, as in the Claude Code and
  Codex files: OpenCode's own read, glob and grep tools stay in the project, while a
  shell rule would reach any path.
- **T3 tools.** OpenCode names an MCP tool `<server>_<tool>` and turns unsupported
  characters into `_`; its docs do not say whether `-` is one of them. So every T3
  rule is written twice, as `t3-code_…` and `t3_code_…`. Wildcards in action names
  are documented (the base policy uses `"action": "*"`), so `t3-code_*` and
  `t3_code_*` ask about every T3 tool, including ones T3 adds later, before the exact
  grants.

Not verified (only OpenCode's
[permissions](https://opencode.ai/docs/permissions/) and
[config](https://opencode.ai/docs/config/) pages were read, on 2026-11-09):

- **The version.** The file uses the V2 format (`permissions`, `shell`); V1 used
  `permission` and `bash`. The owner reports OpenCode v2.2.6 (2026-11-09). That is not
  confirmed to read the V2 `permissions` array: the docs call the format "V2" without
  tying it to a program version, do not say what 2.x does with an old
  `permission`/`bash` config, and show no wildcard action name for MCP tools
  (`server_*`). The release notes for v2.2.5 and v2.2.6 show permission rules in use
  and `bash` calls now running as `shell`, which fits V2 but proves nothing. If
  OpenCode does not read the file, it allows everything, with no error, so the live
  check [TC-QA-009](../testing/cases/qa.md) decides.
- **The T3 spelling** OpenCode actually uses; one of the two spellings is unused.
- **Chains and redirections.** A prefix rule cannot check arguments, so chains
  (`&&`, pipes, `;`, `$(...)`, backticks) are only safe if OpenCode itself splits a
  command and checks each part, which its docs describe but was not verified for
  v2.2.6. Redirections (`git status > file`) were not verified either: plain prefix
  matching would allow them, so they rely on OpenCode's own scanner.
- **Other grants.** "Allow always" saves project rules that can override an ask (not a
  deny), and per-agent rules in a personal or global config are appended after this
  file's. Like host grants elsewhere, these are not capped by the project's file.
- **Not covered by this decision:** other MCP servers, `webfetch`, `websearch` and
  `subagent` keep OpenCode's allow default.

QA-15's tests read the file strictly (comments stripped, only the known keys, the
catch-all ask first) and decide each shared command in rule order, the way OpenCode
does. As for the other tools, they do not show that an installed OpenCode loaded it.

## Vibe rewrites its own file — 2026-11-09

Mistral Vibe 3.0.4 rewrites `.vibe/config.toml` when it starts: it sorts the allowlist (the denylist keeps
its order), indents with four spaces, drops every comment and records the one-time upgrade it ran
as `applied_migrations`. The owner chose to keep Vibe's version in the repository
instead of fighting the tool, so the file no longer shows as modified after every
Vibe start. The command lists did not change.

Comments in that file would be lost again on the next rewrite, so what they said
lives here:

```
what the file says                          why
──────────────────────────────────────      ────────────────────────────────────
tools.bash.allowlist / denylist             Vibe replaces its default lists with
                                            these, so they repeat Vibe's readers
                                            and deny defaults (note 2026-11-04)
t3-code_<tool> = "always"                   "t3-code" is T3's server alias; Vibe
                                            publishes <alias>_<tool>. Only named
                                            tools skip approval, every other MCP
                                            tool keeps asking (QA-15)
no path grants, no denylist_standalone      access outside the workdir keeps Vibe's
  or sensitive_patterns                     own defaults
applied_migrations                          written by Vibe, not by us
```

`internal/repocheck` accepts `applied_migrations` only with the migration name Vibe
has written so far (`bash_read_only_defaults_v1`). A different one fails the test
until someone has read what that migration changed, so it cannot loosen a safeguard
unnoticed (QA-15).
