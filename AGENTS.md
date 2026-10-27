# Apptrol — notes for coding agents

Apptrol is a per-app volume mixer for PipeWire, driven by a Korg nanoKONTROL2. Go, pure
(no cgo), runs as a systemd user service. The repository is public; releases are listed
in [CHANGELOG.md](CHANGELOG.md).

This file holds the rules for every coding agent working on Apptrol (Claude Code, Codex,
…). [CLAUDE.md](CLAUDE.md) only imports it and adds what is specific to Claude Code.
Where agent rules live, and why: [ADR 0040](docs/adr/0040-where-agent-rules-live.md).

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
| Log format and attributes | [docs/logging.md](docs/logging.md), [docs/log-attributes.md](docs/log-attributes.md), [ADR 0016](docs/adr/0016-log-records-follow-opentelemetry.md) |
| Decisions | [docs/adr/](docs/adr/) |
| All docs by type (Diátaxis), docs rules | [docs/README.md](docs/README.md), [CONTRIBUTING.md › Documentation](CONTRIBUTING.md#documentation) |

## The life of a task

The sections below follow a task from start to end. [Boundaries](#boundaries) hold at
every step.

```
start → plan → set up → build → review ⇄ publish → ready → clean up → hand over, end
         │                           └ rounds ┘
         └ the owner's OK

routine:              build → push → draft PR → review rounds → ready
complex or important: build → local review rounds → push → draft PR → ready
```

1. [Boundaries](#boundaries): privacy, files, commands
2. [Start](#start): look up the state
3. [Plan](#plan): issues, decisions, the builder, the owner's OK
4. [Set up](#set-up): branch or worktree, starting other agents, the live setup
5. [Build](#build): code, docs, tests, checks
6. [Review](#review): another model, one commit per round, local first when it matters
7. [Publish](#publish): the owner's OK, push, draft PR
8. [Ready](#ready): one definition, then the owner merges
9. [Clean up](#clean-up): delete the merged branch
10. [Hand over and end](#hand-over-and-end): one owner per task

## Boundaries

- **Privacy:** never publish data from the owner's system in commits, docs, tests,
  issues or PRs: home paths, usernames, device names, hardware or device IDs, process
  numbers, media titles. Use placeholders (`<pid>`, `<phone name>`, `/home/you`);
  measured data keeps its structure, not its values. This holds for values from logs
  the owner pastes too: made-up values in tests and comments, never the real ones.
  Check the diff before every commit and push. A local check also runs on commit and
  push; if it blocks, tell the owner where and let them look. Never bypass it
  (`--no-verify`), and never open its pattern list.
- The local privacy check lives in the main checkout's git hooks, which its worktrees
  share; a fresh clone does not have it. If `git commit` runs without it, tell the
  owner; don't recreate its pattern list.
- Only access files inside this repository and its worktrees in
  `~/.t3/worktrees/apptrol/`, never elsewhere in the home folder (not even the rest of
  `~/.t3/`, nor Go's module cache; look at libraries through the GitHub API).
  One narrow exception: an agent's own private notes for this project (Claude Code's
  memory folder for it, Codex's own memories), to read and write those notes, nothing
  else there.
- **Commands you may run yourself:**
  ```
  read only     read and search files in this repository and its worktrees; git and
                GitHub commands that only read (status, log, diff, PR and issue views,
                CI checks)
  build, test   make check, make test-audio, make test-systemd, make test-install,
                make test-clean, make container-run RECIPE=<name>
  git           list branches, git switch (also -c), git pull, git add, commit;
                delete your own merged branch as Clean up says (no question in the
                chat); deleting or renaming any other branch, and push, only after
                the owner's OK, see Publish
  waiting       any of these in the background, e.g. gh pr checks --watch for CI, or
                the tool's own way to watch a PR where it has one (T3 Code)
  ```
  The exact commands are in [.claude/settings.json](.claude/settings.json) for Claude
  Code and [.codex/rules/apptrol.rules](.codex/rules/apptrol.rules) for Codex: those run
  without a prompt, and pushing or changing GitHub prompts. Both lists match how a
  command starts, not its arguments (`make check` lets `make check other-target`
  through), so this list still decides which arguments are fine. Codex applies its rules
  to commands outside its sandbox, and only when the project is trusted in Codex, from
  its next start. A new command goes into both files (a test checks they match). When
  the folder is trusted, Grok (Grok Build) reads `.claude/settings.json` and AGENTS.md
  by itself, so it needs no file of its own; `grok inspect --json` shows
  `projectTrusted` and how many rules it loaded (its `sources` line may name only the
  personal `.claude/settings.local.json`, but the count includes the shared file). Run
  commands from the checkout's folder, one per call (no `cd … &&`, no `git -C`), so
  they match the lists.
  Before a command that does more than read, say in one or two sentences what you are
  doing and why; afterwards, explain the result: what passed, what failed and what it
  means. Pushing, opening a PR, creating issues and any other change on GitHub still
  need the owner's OK first (see [Publish](#publish)).
  **Anything not on this list needs the owner's OK first**, even when it would help
  (e.g. a VM, installing a package): a missing rule is not permission. Say what you
  would run and why, then wait. A sub-agent's task names the commands it may run.
  **Containers** ([ADR 0034](docs/adr/0034-containers-through-one-script.md), QA-16):
  rootless Podman only, never Docker and never `sudo podman`, and every run through
  the script includes `AGENT=<model-name>` for audit attribution (ADR 0034's dated
  clarification; this label never reaches Podman). Every run goes through
  the repository's container script (`make test-systemd`, `make test-install`,
  `make container-run RECIPE=<name>`), which records it in `.audit/`.
  `make test-clean` removes the folders and logs that failed runs keep (`.tmp/run-*`,
  `.audit/logs/`) through the same script, in this checkout only, and records itself
  too (QA-18). The script mounts only the checkout,
  read-only, and `.tmp/<run>/` inside it as the one writable folder (ignored by git):
  never `~/.ssh`, the rest of the home folder or `/run/user/…`, and no
  `SSH_AUTH_SOCK`. A flag the script lacks is a pull request to the script. Typed
  `docker`, `podman` and `buildah` are blocked in both command lists. The block
  matches only a command's first word, so never wrap one to get past it (`sudo`,
  `env`, `nice`, `bash -c`); the owner answers such a request no.
  **During a manual test run only**
  ([how-to](docs/testing/how-to-run-release-tests.md)): also
  `docs/testing/apply-fixture.sh` and reading Apptrol's journal
  (`journalctl --user -u apptrol`).

## Start

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
  `git switch main`, `git pull`, then a new branch. In a worktree you were handed, stay
  on its branch: `main` is checked out elsewhere, so `git switch main` fails there.
- Release state is read from `origin/main`, not from your branch, which may be older:
  e.g. `git show origin/main:CHANGELOG.md`. Your branch's files tell you about your task.
- What the next release contains: [CHANGELOG.md](CHANGELOG.md) › Unreleased, and the
  phase in [docs/roadmap.md](docs/roadmap.md).
- A release candidate is tagged but not released while its run record in
  `docs/testing/runs/` is missing or not passed; continue with the test run
  ([how-to](docs/testing/how-to-run-release-tests.md)), then
  [docs/releasing.md](docs/releasing.md).
- Agents' private notes (Claude's memory, Codex's memories) are not shared between
  agents. Shared rules go in this file; status and handover details go in an issue, a
  PR or the handed-over task.

## Plan

The owner wants to keep learning while working, not just receive finished results.

- **Plan first.** Before any non-trivial change, explain what you would do and why, and
  wait for the owner's OK. Don't edit files until they agree.
- **The plan names the builder:** the tool, the model and the worktree that will write
  the code (e.g. "Claude Opus 5.5, a delegated task in its own worktree"). The owner's
  OK on the plan also starts it, as a delegated task in its own worktree
  ([Set up](#set-up)). Reasoning above the model's default, pushing and changes on
  GitHub still need their own OK.
- **Explain simply, with examples.** Lead with the plain difference in a sentence or
  two, then a concrete example (the log lines, a config snippet), then what needs
  deciding. Few options, short; dense tables confuse more than they help.
- **The owner decides, the agent writes the code.** When something has several valid
  approaches (behaviour, error handling, data structures), stop and ask in the chat, like
  a small ADR: each option with a concrete example, what it costs, and a recommendation.
  When a rule already prefers one option, put it first and say which rule. Do not put
  `TODO(human)` in the code for the owner to write. The decision goes in an ADR or a
  dated note on one.
- **The owner often dictates.** When a word in an answer looks odd (speech-to-text
  writes "Flatback" for Flatpak), ask what was meant before acting on it.
- **Check the open issues** (`gh issue list`) when planning a feature or an ADR and
  before opening a PR: does the change make one obsolete, conflict with one, or finish
  one? Say so in the plan and the PR, and suggest updating or closing it (e.g. R's
  play/pause and launchers left no press for moving apps between outputs, #61).
- Suggest an issue whenever [CONTRIBUTING.md › Issues](CONTRIBUTING.md#issues) asks for
  one (a bug in a released version, an idea or task left for later, a bug in another
  app); a fix's PR says `Fixes #n`. A security problem is never a public issue or PR
  text first: tell the owner, who reports it as [SECURITY.md](SECURITY.md) says.
- ADRs follow [CONTRIBUTING.md › Before you start](CONTRIBUTING.md#before-you-start).
  Several agents may write one at the same time, so the plan **reserves the next ADR
  number** and says so in the issue or the PR (e.g. "ADR number 0040 is reserved for
  this PR"); a second agent takes the number after it.
- **Small PRs.** Split a feature into 2–4 PRs, each one something the owner can see or
  try on its own (e.g. media buttons went in four: discovery, matching, R, media keys).
- No workarounds for bugs in other apps (e.g. Spotify). Document them in
  [docs/known-issues.md](docs/known-issues.md) instead; the README lists only the most
  common ones in one line each.

## Set up

The owner may run more than one agent on this repository at the same time and hand tasks
from one to another.

- Never work on `main`. Branches, commits and the required checks:
  [CONTRIBUTING.md › Branches and pull requests](CONTRIBUTING.md#branches-and-pull-requests).
  On top of that, agents review before the owner merges ([Review](#review)), open PRs as
  drafts and ask before pushing ([Publish](#publish)).
- **One task, one branch.** Each agent works on its own branch, or in its own worktree,
  never on a branch another agent is using. A worktree stays where its tool makes it:
  T3 Code in `~/.t3/worktrees/apptrol/`, Claude Code in `.claude/worktrees/`; one made
  by hand (`git worktree add`) goes under `.worktrees/`, named after no tool. Git ignores
  both folders inside the repository. Check `git status` and the current branch before
  the first edit; if there are changes you did not make, stop and ask.
- **One owner per task.** Each task and its PR have one named owner, the agent that
  changes the branch. A read-only reviewer may work on the same branch alongside it
  ([Review](#review)). Ownership moves only by a handover
  ([Hand over and end](#hand-over-and-end)).
- **Start another agent only when the owner asks** (a child task or a new thread; an
  OK'd plan that names the builder counts, see [Plan](#plan)), and say which tool and
  model it runs on. It runs at its model's default reasoning level or
  lower; a lower level needs no OK. Anything above the default (high, extra high, max,
  …), another model than the table in [Review](#review) names, or wider permissions
  need the owner's OK first: ask, with the reason and what it costs.
- **Waiting and small jobs stay cheap.** Waiting on something that changes nothing (CI,
  a long read-only check) runs in the background, so the chat stays free: say so in one
  line, no need to ask. Running in the background does not make a command allowed; only
  the commands in [Boundaries](#boundaries) › Commands you may run yourself run without
  asking. A small helper job that needs an agent (e.g. summing up check
  results) runs on a small model, Claude **Haiku 5.5** or GPT **6-Luna**; plans, code
  and PR reviews follow the table in [Review](#review). Anything that changes something
  (push, merge, issues, edits) still needs the owner's OK.
  ```
  just waiting (CI, a long check)  →  background command   no model tokens
  a small job                      →  Haiku 5.5 or GPT-6-Luna
  plans, code, reviews             →  the models in the table in Review
  ```
- **One agent at a time owns the live setup.** Worktrees are separate, but the owner's
  PipeWire, the running Apptrol service and the controller are one. Before
  `make test-audio`, a test run with `docs/testing/apply-fixture.sh` or anything else
  that changes the running service, say in the chat that you take the live setup; when done, hand it back restored (after a test
  run: `docs/testing/apply-fixture.sh --restore`) and say so. Wait while another agent
  holds it.
  ```
  agent A: takes it ── make test-audio ── hands it back ┤
  agent B:                    waits ────────────────────┤ takes it ── apply-fixture …
  ```

## Build

- Every commit names the model that wrote it in a `Co-Authored-By:` line, Codex as well
  as Claude, e.g. `Co-Authored-By: GPT-6.1-Sol <noreply@openai.com>` or
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`, so it is clear which
  agent did what.
- Every behaviour has a requirement ID in [docs/requirements.md](docs/requirements.md);
  tests are named after it (QA-07), e.g. `TestSOLO05_PressingSoloAgainTurnsItOff`.
- **Code comments explain the why** and cite the requirement ID and the ADR, e.g.
  `(LAUNCH-13, ADR 0024)`, on every doc comment of a type or function and on comments
  that explain a decision.
- `internal/mixer` stays pure: no I/O, events in, actions out
  ([ADR 0015](docs/adr/0015-service-architecture.md)).
- No cgo, and no `sdjournal`: [CONTRIBUTING.md › Code](CONTRIBUTING.md#code).
- **Leave room for later**, without building it now: OS-specific code only in adapters,
  so the core keeps building for other systems (#92, #93); facts about the controller
  only in `internal/controller` profiles, so other controllers stay possible (#64).
- Logs, and planning them so every feature is troubleshootable from its logs alone:
  [CONTRIBUTING.md › Log messages](CONTRIBUTING.md#log-messages).
- With every change, update what it affects: requirements,
  [docs/testing.md](docs/testing.md), architecture, README, and
  [CHANGELOG.md](CHANGELOG.md) (Unreleased) for user-visible changes
  ([CONTRIBUTING.md › Commits and pull requests](CONTRIBUTING.md#commits-and-pull-requests)).
- **[docs/architecture.md](docs/architecture.md) grows with every feature.** Re-read it
  and update every part the change touches (overview diagram, service interfaces, the
  mixer's responsibilities, event flow, adapter sections, testing list), not just a
  table row.
- Manual test cases and fixtures for every new feature or behaviour change:
  [CONTRIBUTING.md › Commits and pull requests](CONTRIBUTING.md#commits-and-pull-requests).
- `docs/` is the website's source: follow
  [CONTRIBUTING.md › Documentation](CONTRIBUTING.md#documentation) (plain Markdown, links,
  a place in docs/README.md, phase name + Goal + Done when).
- **Text diagrams** wherever a setting or a behaviour can be shown visually:
  [CONTRIBUTING.md › Documentation](CONTRIBUTING.md#documentation). This holds for
  agent rules, PRs and issues too.
- **Before a PR:** fill in the checklist of
  [.github/pull_request_template.md](.github/pull_request_template.md); it lists what
  every contributor checks. Agents add only this:
  - check the diff for private data ([Boundaries](#boundaries));
  - choose the review order: routine → push first; complex or important → review on
    the local branch first ([Review](#review)).
- For audio changes also `make test-audio` (runs against the owner's PipeWire; it adds
  a silent test output and inputs and removes them afterwards; it needs the live setup,
  see [Set up](#set-up)). CI's PipeWire is older than the owner's and behaves
  differently, so audio tests act like real apps (send data, wait for state) and CI is
  the real check.
- **Hardware tests** (controller, LEDs, listening) need the owner: tell them exactly what
  to do and what to look for, then read the logs yourself and explain what they show.

## Review

- **The other provider reviews.** A PR written by Claude is reviewed by Codex, and one
  written by Codex by Claude, before the owner merges it: a different model finds
  different bugs. Review against the requirements and this file, not only the diff.
- **When only one provider is available,** a fresh agent of the same provider on
  another model reviews, with an independent brief (see below: no chat history, no
  earlier findings). The author's provider decides which:
  ```
  Claude implements, no Codex:   routine → Claude Sonnet 5.5
                                 complex or important → a new Claude Opus 5.5 agent
  Codex implements, no Claude:   ask the owner which GPT model reviews
  ```
  The PR says which review route was used (e.g. "Reviewed by Claude Sonnet 5.5, Codex
  not available").
- **Split the work by provider.** When the owner hands a release's issues to both
  providers, some go to Codex and some to Claude, and each reviews the other's PRs.
  Codex writes code with GPT-6.1-Sol (GPT-6-Astra used up its tokens too fast and ran
  out in the middle of tasks). The reviewer's model depends on the PR (as of 2026-10;
  the owner names newer models when they come):

  | Implementation | Routine PR review | Complex or important PR review |
  |---|---|---|
  | **GPT / Codex implements** | Claude **Sonnet 5.5** | Claude **Opus 5.5** |
  | **Claude Code implements** | GPT **6.1-Sol** | GPT **6.1-Sol** |

  **Routine:** docs, tests, small fixes. **Complex or important:** a change of
  behaviour, a release, anything touching audio, the controller, privacy or security.
  When unsure, ask the owner which one it is.
- **Local first when it matters.** The repository is public, so a flaw is published the
  moment its commit is pushed (#199: PR #198's two script flaws were public until
  fixed). A **complex or important** PR is reviewed on the local branch **before its
  first push**; the owner's OK to push ([Publish](#publish)) is asked after the clean
  local round. A **routine** PR is pushed first, so CI runs alongside the review.
  ```
  routine:              commit → OK, push → draft PR → review rounds → ready
  complex or important: commit → local review rounds → OK, push → draft PR → ready
  ```
- **One commit per round.** The author tells the reviewer the exact commit, and the
  reviewer names it in its verdict: "Reviewed `<sha>`: no findings". During a round the
  author does not change the branch, or the reviewer works in its own checkout of that
  commit. A clean verdict counts only for that commit: a new commit needs a new round
  before it is pushed (complex or important) or marked ready.
  ```
  commit A → round on A → findings → commit B → round on B → clean → B may go on
  ```
- **Rounds:**
  ```
  round with findings     →  fix in new commits → next round
                             (pushed already: push the fix, still draft)
  round with no findings  →  not pushed yet: ask to push, open the draft
                             pushed already: Ready
  ```
  Pushed history is never rewritten. Commits that were never pushed are kept as they
  are too, with one exception: when a local round finds a **security flaw**, the
  unpushed commits may be squashed into one, with the owner's OK, so the flawed version
  never reaches GitHub. The squashed commit is new, so it gets its own round. Other
  findings stay their own commits. A flaw found after the
  first push: tell the owner first, and write nothing about it in public text (commits,
  PR, issues) until the owner has reported it as [SECURITY.md](SECURITY.md) says.
- **Independent second opinion.** When a second agent checks the same thing (a review,
  an audit, a design), its brief gives the task, the files and the facts the files
  don't show yet, never the first agent's findings. The lead merges both lists and
  marks each finding as found by both or by one; a finding only one found is checked
  against the files before it is used (as in #200).

## Publish

- **Ask before pushing, opening a PR or creating issues.** For a complex or important PR,
  ask after the clean local round ([Review](#review)). Once the owner has OK'd a PR,
  its review fixes and marking it ready need no new question ([Ready](#ready)).
- **Draft until reviewed.** Open a PR as a draft (`gh pr create --draft`), so it cannot
  be merged halfway; it is reviewed in rounds ([Review](#review)).
- `gh pr edit` can fail without changing the PR; edit a PR's text with
  `gh api -X PATCH repos/Dzobash/apptrol/pulls/<n> -F body=@file` and read it back.
- Releases: release candidates (`vX.Y.Z-rcN`) first, the manual test run on an installed
  candidate ([ADR 0032](docs/adr/0032-patch-releases-test-what-changed.md) says which
  cases), then the release as in [docs/releasing.md](docs/releasing.md). Fixes ship as
  PATCH versions (`0.2.x`).

## Ready

- **Ready means, for every PR:**
  ```
  ✓ the PR's current commit was reviewed, with a clean round (Review)
  ✓ the 7 required checks are green (CONTRIBUTING.md › Branches and pull requests)
  ✓ any other failing check is explained in the PR
  ✓ no open findings
  ```
  A fix pushed after a clean round (e.g. for CI) is a new commit, so it needs its own
  round on the draft PR first.
- Then the agent that owns the PR marks it ready itself (`gh pr ready <n>`), without
  asking in the chat again: the owner's OK for the PR covers pushing review fixes, the
  next rounds and marking it ready. The tools' own prompts for `git push` and
  `gh pr ready` stay (QA-15); the owner answers them with a click, not a new decision.
- **Rules files.** Before a PR that changes AGENTS.md or another rules file (CLAUDE.md,
  CONTRIBUTING.md, a skill or command, the settings files) is marked ready, re-read the
  whole resulting file on top of current `main`: two PRs can merge cleanly and still
  contradict each other.
- The owner reviews and merges PRs.

## Clean up

The agent that owns the task deletes its branch itself, without asking in the chat,
once all three hold:

```
✓ its PR is merged          gh pr list --state merged --head <branch>
✓ its content is in main    git fetch origin, then compare with origin/main
✓ no agent uses the branch  (Hand over and end; the wrap-up's point 1)
```

The tool's own prompt for deleting a branch stays (QA-15), one click.

- **In the main checkout:** `git switch main`, `git pull`, then `git branch -D <branch>`.
  It needs `-D`: a squash merge puts one new commit on `main`, so git does not see the
  branch's own commits as merged and `-d` refuses.
- **In a worktree:** stay there (`main` is checked out elsewhere, so `git switch main`
  fails). The tool that made the worktree removes it. Git cannot delete a branch that a
  worktree still has checked out, so the branch is deleted after that, from the main
  checkout (`git branch -D <branch>`), e.g. during the wrap-up.

## Hand over and end

- **A handed-over task is written out in full.** The receiving agent has none of the
  chat: give it the issue number, the requirement IDs, the branch name, what "done"
  means and which rules matter most. It reads this file by itself. Commit first: a new
  worktree starts from the last commit, so changes that are not committed never reach it.
- **A handover moves ownership** ([Set up](#set-up) › One owner per task). It records:
  ```
  commit        the branch's last commit (sha)
  uncommitted   what is not committed, and why
  running       jobs still running (CI watch, a review, the live setup)
  findings      review findings still open
  OKs           the owner's OKs so far (plan, push, PR)
  ```
  The sender stops changing the branch before the receiver starts, and says so.
- **At the end of a session,** when the owner asks whether everything is done, follow
  [`.agents/skills/wrap-up/SKILL.md`](.agents/skills/wrap-up/SKILL.md): other agents,
  code, GitHub, notes, handover, then a short report (Codex: `$wrap-up`, Claude Code:
  `/wrap-up`).
