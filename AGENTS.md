# Apptrol — notes for coding agents

Apptrol is a per-app volume mixer for PipeWire, driven by a Korg nanoKONTROL2. Go, pure
(no cgo), runs as a systemd user service. The repository is public; releases are listed
in [CHANGELOG.md](CHANGELOG.md).

This file holds the rules for every coding agent working on Apptrol (Claude Code, Codex,
…). [CLAUDE.md](CLAUDE.md) only imports it and adds what is specific to Claude Code.

Read these instead of guessing; this file only links to them:

| Topic | Where |
|---|---|
| What it does | [README.md](README.md) |
| Install, controller settings, permissions | [docs/install.md](docs/install.md) |
| Known issues | [docs/known-issues.md](docs/known-issues.md) |
| Branches, commits, project layout, log rules | [CONTRIBUTING.md](CONTRIBUTING.md) |
| Requirements with IDs | [docs/requirements.md](docs/requirements.md) |
| Packages and event flow | [docs/architecture.md](docs/architecture.md) |
| Phases and what is next | [docs/roadmap.md](docs/roadmap.md) |
| Tests, CI, hardware checklist | [docs/testing.md](docs/testing.md) |
| Manual test cases, fixtures, runs | [docs/testing/](docs/testing/strategy.md), [ADR 0031](docs/adr/0031-manual-test-cases-as-docs.md), [how to run them](docs/testing/how-to-run-release-tests.md) |
| Releases and release candidates | [docs/releasing.md](docs/releasing.md) |
| Log format and attributes | [docs/logging.md](docs/logging.md), [ADR 0016](docs/adr/0016-log-records-follow-opentelemetry.md) |
| Decisions | [docs/adr/](docs/adr/) |
| All docs by type (Diátaxis), docs rules | [docs/README.md](docs/README.md), [CONTRIBUTING.md › Documentation](CONTRIBUTING.md#documentation) |

## Workflow

- Never work on `main`. Branch (`feat/`, `fix/`, `docs/`, …) → commit (Conventional
  Commits, with requirement IDs, e.g. `fix: … (MUTE-07)`) → PR → all 7 CI checks green
  (Lint, Test on two Go versions, Fuzz, Vulnerability check, Build, Packaging) → squash merge.
  A ruleset on `main` enforces this.
- Every commit names the model that wrote it in a `Co-Authored-By:` line, Codex as well
  as Claude, e.g. `Co-Authored-By: GPT-6-Astra <noreply@openai.com>` or
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`, so it is clear which
  agent did what.
- The owner reviews and merges PRs. **Ask before pushing, opening a PR or creating
  issues.**
- Suggest an issue for every bug found, every idea or task left for later, and every
  bug in another app (rules in [CONTRIBUTING.md](CONTRIBUTING.md#issues)); a fix's PR
  says `Fixes #n`.
- **Check the open issues** (`gh issue list`) when planning a feature or an ADR and
  before opening a PR: does the change make one obsolete, conflict with one, or finish
  one? Say so in the plan and the PR, and suggest updating or closing it (e.g. R's
  play/pause and launchers left no press for moving apps between outputs, #61).
- **Small PRs.** Split a feature into 2–4 PRs, each one something the owner can see or
  try on its own (e.g. media buttons went in four: discovery, matching, R, media keys).
- After a merge: `git switch main && git pull`, then delete the local branch.
- Releases: release candidates (`vX.Y.Z-rcN`) first, the manual test run on an installed
  candidate ([ADR 0032](docs/adr/0032-patch-releases-test-what-changed.md) says which
  cases), then the release as in [docs/releasing.md](docs/releasing.md). Fixes ship as
  PATCH versions (`0.2.x`).
- `gh pr edit` can fail without changing the PR; edit a PR's text with
  `gh api -X PATCH repos/Dzobash/apptrol/pulls/<n> -F body=@file` and read it back.

## Finding the current state

This file holds rules, not status. A new session (or an agent taking over a task) starts
by looking the state up, instead of guessing or relying on an old chat:

```bash
git status --short --branch          # which branch, and is anything not committed?
git fetch origin                     # the latest main, without leaving your branch
gh pr list                           # open PRs: waiting for review, CI or merge?
gh issue list                        # open work; the next release's issues
git tag --sort=-creatordate | head   # last release and release candidates
git ls-tree --name-only origin/main docs/testing/runs/   # test run records, as on main
```

- Where to start depends on the checkout: in the main checkout, new work starts with
  `git switch main && git pull`, then a new branch. In a worktree you were handed, stay
  on its branch: `main` is checked out elsewhere, so `git switch main` fails there.
- Release state is read from `origin/main`, not from your branch, which may be older:
  e.g. `git show origin/main:CHANGELOG.md`. Your branch's files tell you about your task.
- What the next release contains: [CHANGELOG.md](CHANGELOG.md) › Unreleased, and the
  phase in [docs/roadmap.md](docs/roadmap.md).
- A release candidate is tagged but not released while its run record in
  `docs/testing/runs/` is missing or not passed; continue with the test run
  ([how-to](docs/testing/how-to-run-release-tests.md)), then
  [docs/releasing.md](docs/releasing.md).
- Agents' private notes (Claude's memory, Codex's sessions) are not shared between
  agents. Shared rules go in this file; status and handover details go in an issue, a
  PR or the handed-over task.
- The local privacy check lives in the main checkout's git hooks, which its worktrees
  share; a fresh clone does not have it. If `git commit` runs without it, tell the
  owner; don't recreate its pattern list.
- **At the end of a session,** when the owner asks whether everything is done, follow
  [`.agents/skills/wrap-up/SKILL.md`](.agents/skills/wrap-up/SKILL.md): other agents,
  code, GitHub, notes, handover, then a short report (Codex: `$wrap-up`, Claude Code:
  `/wrap-up`).

## Several agents at once

The owner may run more than one agent on this repository at the same time and hand tasks
from one to another.

- **One task, one branch.** Each agent works on its own branch, or in its own worktree,
  never on a branch another agent is using. A worktree stays where its tool makes it:
  T3 Code in `~/.t3/worktrees/apptrol/`, Claude Code in `.claude/worktrees/`; one made
  by hand (`git worktree add`) goes under `.worktrees/`, named after no tool. Git ignores
  both folders inside the repository. Check `git status` and the current branch before
  the first edit; if there are changes you did not make, stop and ask.
- **Start another agent only when the owner asks** (a child task or a new thread), and
  say which tool and model it runs on. It runs at its model's default reasoning level or
  lower; a lower level needs no OK. Anything above the default (high, extra high, max,
  …), another model than the table below names, or wider permissions need the owner's
  OK first: ask, with the reason and what it costs.
- **Waiting and small jobs stay cheap.** Waiting on something that changes nothing (CI,
  a long read-only check) runs in the background, so the chat stays free: say so in one
  line, no need to ask. Running in the background does not make a command allowed; only
  the commands in How we work together › Commands you may run yourself run without
  asking. A small helper job that needs an agent (e.g. summing up check
  results) runs on a small model, Claude **Haiku 5.5** or GPT **6-Luna**; plans, code
  and PR reviews follow the table below. Anything that changes something
  (push, merge, issues, edits) still needs the owner's OK.
  ```
  just waiting (CI, a long check)  →  background command   no model tokens
  a small job                      →  Haiku 5.5 or GPT-6-Luna
  plans, code, reviews             →  the models in the table below
  ```
- **A handed-over task is written out in full.** The receiving agent has none of the
  chat: give it the issue number, the requirement IDs, the branch name, what "done"
  means and which rules matter most. It reads this file by itself. Commit first: a new
  worktree starts from the last commit, so changes that are not committed never reach it.
- **The other provider reviews.** A PR written by Claude is reviewed by Codex, and one
  written by Codex by Claude, before the owner merges it: a different model finds
  different bugs. Review against the requirements and this file, not only the diff. If
  the other provider is not available, tell the owner and leave the review open.
- **Split the work by provider.** When the owner hands a release's issues to both
  providers, some go to Codex and some to Claude, and each reviews the other's PRs.
  Codex writes code with GPT-6-Astra. The reviewer's model depends on the PR (as of
  2026-10; the owner names newer models when they come):

  | Implementation | Routine PR review | Complex or important PR review |
  |---|---|---|
  | **GPT / Codex implements** | Claude **Sonnet 5.5** | Claude **Opus 5.5** |
  | **Claude Code implements** | GPT **6.1-Sol** | GPT **6-Astra** |

  Astra is the stronger GPT model, like Opus. **Routine:** docs, tests, small fixes.
  **Complex or important:** a change of behaviour, a release, anything touching audio,
  the controller, privacy or security. When unsure, ask the owner which one it is.
- **Draft until reviewed.** Open a PR as a draft (`gh pr create --draft`) and mark it
  ready (`gh pr ready <n>`) only after the other provider's review is done and its
  findings are fixed, so it cannot be merged halfway. Fixes after a review are new
  commits, not a rewritten history.

## Code and docs

- Every behaviour has a requirement ID in [docs/requirements.md](docs/requirements.md);
  tests are named after it (QA-07), e.g. `TestSOLO05_PressingSoloAgainTurnsItOff`.
- Every decision gets an ADR ([docs/adr/](docs/adr/), copy `template.md`, next free
  number, add it to the index). Accepted ADRs are not rewritten, only given dated notes;
  a changed decision gets a new ADR that supersedes the old one.
- **Code comments explain the why** and cite the requirement ID and the ADR, e.g.
  `(LAUNCH-13, ADR 0024)`, on every doc comment of a type or function and on comments
  that explain a decision.
- Logs follow ADR 0016 (OpenTelemetry conventions): attribute names from
  `internal/logattr`, `apptrol.component` on every record, `error.type` on errors, one
  line per record with a fixed message. New attributes and error types also go in
  [docs/logging.md](docs/logging.md) (a test checks this).
- **Every feature must be troubleshootable from its logs alone.** Plan its logs with the
  design: log each decision with the reason (e.g. `apptrol.player.matched_by`), and
  rejected or ignored candidates at debug. A feature's ADR has a logging table: level,
  message, attributes ([ADR 0018](docs/adr/0018-media-players-through-mpris.md), point 9).
- With every change, update what it affects: [CHANGELOG.md](CHANGELOG.md) (Unreleased),
  requirements, [docs/testing.md](docs/testing.md), architecture, README.
- **[docs/architecture.md](docs/architecture.md) grows with every feature.** Re-read it
  and update every part the change touches (overview diagram, service interfaces, the
  mixer's responsibilities, event flow, adapter sections, testing list), not just a
  table row.
- **Whenever a new feature or behaviour change is added, also add or update its test
  cases and fixtures in [docs/testing/](docs/testing/cases/README.md)** (same format), in
  addition to docs, roadmap, changelog and ADR. Every roadmap phase must have test cases
  before release (QA-14, ADR 0031).
- `docs/` is the website's source: follow
  [CONTRIBUTING.md › Documentation](CONTRIBUTING.md#documentation) (plain Markdown, links,
  a place in docs/README.md, phase name + Goal + Done when).
- **Text diagrams.** Wherever a setting or a behaviour can be shown visually (a
  timeline, LED picture, small graph), add a plain-text diagram in a code block on top
  of the written explanation, never instead of it:
  ```
  keep:     you keep it ─────┤ released ─────┤ you take it again
  release:  released ────────────────────────┤ you take it again
  ```
- Before a PR: `make check`. For audio changes also `make test-audio` (runs against the
  owner's PipeWire; it adds a silent test output and inputs and removes them afterwards).
  CI's PipeWire is older than the owner's and behaves differently, so audio tests act
  like real apps (send data, wait for state) and CI is the real check.
- `internal/mixer` stays pure: no I/O, events in, actions out
  ([ADR 0015](docs/adr/0015-service-architecture.md)).
- No cgo: the binary stays pure Go and static (ADR 0013). Never import
  `github.com/coreos/go-systemd/v22/sdjournal`; it needs cgo
  ([ADR 0017](docs/adr/0017-desktop-services-over-dbus.md)).
- **Leave room for later**, without building it now: OS-specific code only in adapters,
  so the core keeps building for other systems (#92, #93); facts about the controller
  only in `internal/controller` profiles, so other controllers stay possible (#64).

## How we work together

The owner wants to keep learning while working, not just receive finished results.

- **Plan first.** Before any non-trivial change, explain what you would do and why, and
  wait for the owner's OK. Don't edit files until they agree.
- **Explain simply, with examples.** Lead with the plain difference in a sentence or
  two, then a concrete example (the log lines, a config snippet), then what needs
  deciding. Few options, short; dense tables confuse more than they help.
- **The owner decides, the agent writes the code.** When something has several valid
  approaches (behaviour, error handling, data structures), stop and ask in the chat, like
  a small ADR: each option with a concrete example, what it costs, and a recommendation.
  When a rule already prefers one option, put it first and say which rule. Do not put
  `TODO(human)` in the code for the owner to write. Decisions that matter go in an ADR
  or a dated note on one.
- **The owner often dictates.** When a word in an answer looks odd (speech-to-text
  writes "Flatback" for Flatpak), ask what was meant before acting on it.
- **Commands you may run yourself:**
  ```
  read only     read and search files in this repository and its worktrees; git status,
                log, diff, show; GitHub read only (gh pr / issue list and view, pr diff,
                pr checks, run view)
  build, test   make check, make test-audio
  git           branch, commit (push only after the owner's OK, see Workflow)
  waiting       any of these in the background, e.g. gh pr checks --watch for CI
  ```
  Before a command that does more than read, say in one or two sentences what you are
  doing and why; afterwards, explain the result: what passed, what failed and what it
  means. Pushing, opening a PR, creating issues and any other change on GitHub still
  need the owner's OK first (see Workflow).
  **Anything not on this list needs the owner's OK first**, even when it would help
  (e.g. Docker to rebuild CI's system, a VM, installing a package): a missing rule is
  not permission. Say what you would run and why, then wait. A sub-agent's task names
  the commands it may run.
  **During a manual test run only**
  ([how-to](docs/testing/how-to-run-release-tests.md)): also
  `docs/testing/apply-fixture.sh` and reading Apptrol's journal
  (`journalctl --user -u apptrol`).
- **Hardware tests** (controller, LEDs, listening) need the owner: tell them exactly what
  to do and what to look for, then read the logs yourself and explain what they show.
- No workarounds for bugs in other apps (e.g. Spotify). Document them in
  [docs/known-issues.md](docs/known-issues.md) instead; the README lists only the most
  common ones in one line each.
- Only access files inside this repository and its worktrees in
  `~/.t3/worktrees/apptrol/`, never elsewhere in the home folder (not even the rest of
  `~/.t3/`, nor Go's module cache; look at libraries through the GitHub API).
- **Privacy:** never publish data from the owner's system in commits, docs, tests,
  issues or PRs: home paths, usernames, device names, hardware or device IDs, process
  numbers, media titles. Use placeholders (`<pid>`, `<phone name>`, `/home/you`);
  measured data keeps its structure, not its values. This holds for values from logs
  the owner pastes too: made-up values in tests and comments, never the real ones.
  Check the diff before every commit and push. A local check also runs on commit and
  push; if it blocks, tell the owner where and let them look. Never bypass it
  (`--no-verify`), and never open its pattern list.
