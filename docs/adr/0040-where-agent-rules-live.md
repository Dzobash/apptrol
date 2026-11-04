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

## Permission configuration — 2026-11-04

The owner approved compact read-only pipelines, missing GitHub readers, Vibe project
permissions and exact T3 reader grants. Shared wording remains about agents in
general; provider syntax belongs in settings. Subscription plans are not policy.

```text
AGENTS.md: every component, argument and path must be authorized
  ├─ Claude settings (also consumed by trusted Grok): shared commands + exact MCP names
  ├─ Codex rules: shared commands; native readers retain sandbox scope
  └─ Vibe config: shared commands + native readers; Bash ask, outside paths deny
T3 host: injected server/approvals → separate host policy, not a project transport
```

`head`, `grep`, `tail`, `wc` and `sed` print/range filtering do not need global
unsandboxed prefixes. `sed -n` can still write or execute; shell redirections and
write expressions are outside the read-only policy. Each component of a simple
Codex chain is evaluated separately; a leading allowed `git diff` does not approve
the rest. Prefix grants still cannot constrain every later flag. In particular,
all `gh api` stays gated, including explicit GET prefixes. The installed GitHub
CLI's cache commands are `list` and `delete`; only `list` is preapproved.

Vibe 3.0.4 was checked with `vibe --version`. Its
[Bash implementation](https://github.com/mistralai/mistral-vibe/blob/v3.0.4/vibe/core/tools/builtins/bash.py)
splits command parts, requires every part to match, checks outside paths before
unconditional approval and replaces configured lists. The project preserves the
POSIX reader list and all POSIX deny defaults, adds the container-engine denies,
and leaves `denylist_standalone` and `sensitive_patterns` unset. It grants no home
paths. `outside_workdir = "deny"` is deliberately conservative: sibling worktrees
and private notes do not automatically become accessible. Native readers still
have options that write or execute; the agent must follow AGENTS.md's arguments
rule, and Vibe's native path/option checks remain enabled. No custom shell parser
or classifier is added. The
[project loader](https://github.com/mistralai/mistral-vibe/blob/v3.0.4/vibe/core/config/layers/project.py)
requires trust; global trust and credentials were not inspected or changed.

### Exact T3 readers

The grants are `orchestrator_capabilities`, `t3_worktree_status`,
`t3_worktree_list`, `t3_thread_list`, `t3_thread_read`, `t3_thread_search`,
`t3_project_list`, `t3_project_read`, `list_thread_pull_requests`,
`read_pull_request_guide` and `task_status`. Thread/project reads must still obey
the task's scope and privacy rules. No mixed tool is included, even if it has a
read-only mode.

Verified integration names:

- Claude: `mcp__t3-code__<tool>`, from T3's
  [Claude adapter](https://github.com/pingdotgg/t3code/blob/main/apps/server/src/orchestration-v2/Adapters/ClaudeAdapterV2.ts)
  (`claudeMcpQueryOverrides`, server key `t3-code`). Grok consumes the shared
  settings; no separate Grok config is added. Its MCP matching was not exercised.
- Vibe: `[tools."t3-code_<tool>"]`, `permission = "always"`. T3's
  [ACP adapter](https://github.com/pingdotgg/t3code/blob/main/packages/provider-acp/src/server/adapter.ts)
  supplies alias `t3-code`; Vibe's
  [ACP projection](https://github.com/mistralai/mistral-vibe/blob/v3.0.4/vibe/acp/agent.py)
  preserves that name, and its
  [MCP proxies](https://github.com/mistralai/mistral-vibe/blob/v3.0.4/vibe/core/tools/mcp/tools.py)
  publish `<alias>_<remote.name>`. Other MCP tools keep the native ask default.
- Codex: the named config key is `mcp_servers.t3-code`, not the normalized model
  tool prefix `mcp__t3_code__`. The official
  [MCP configuration](https://developers.openai.com/codex/mcp/)
  documents `default_tools_approval_mode = "prompt"` with per-tool
  `approval_mode = "approve"`. A project MCP server needs a transport definition;
  T3's [Codex adapter](https://github.com/pingdotgg/t3code/blob/main/apps/server/src/orchestration-v2/Adapters/CodexAdapterV2.ts)
  supplies session URL and authentication at runtime in `codexThreadRuntimeParams`.
  No partial `.codex/config.toml` is added. The concrete
  [host merge snippet](https://github.com/Dzobash/apptrol/blob/main/.codex/t3-mcp-policy.toml.example)
  targets that function's `config["mcp_servers.t3-code"]`: merge its policy fields
  after the host supplies transport/authentication. It is **unapplied**, and its
  documented schema has not been validated against the installed Codex runtime.

Project grants are not a host permission cap. At the source checked on this date,
T3's Claude adapter also injects a broader MCP preapproval for writable sandboxes;
the repository's exact grants alone cannot remove it. The Codex host merge and
any restriction of host-injected grants remain host work. Do not switch permission
modes globally to hide prompts: T3's
[permission-mode guide](https://github.com/pingdotgg/t3code/blob/main/docs/user/permission-modes.md)
distinguishes provider-native rules from T3's approval handling.

QA-15 checks file policy with independent safety inventories, including cache
deletion, platform denies and exact MCP readers. It does not claim runtime loading,
trust, argument-level enforcement or host integration was tested.
