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
  subcommand matches. How Grok matches a chain against `.claude/settings.json` was not
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
