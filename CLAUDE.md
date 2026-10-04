# Apptrol — notes for Claude

Apptrol is a per-app volume mixer for PipeWire, driven by a Korg nanoKONTROL2. Go, pure
(no cgo), runs as a systemd user service. v0.1.0 (Phase 1) is released; the repository
is public.

Read these instead of guessing; this file only links to them:

| Topic | Where |
|---|---|
| What it does, install, Known issues | [README.md](README.md) |
| Branches, commits, project layout, log rules | [CONTRIBUTING.md](CONTRIBUTING.md) |
| Requirements with IDs | [docs/requirements.md](docs/requirements.md) |
| Packages and event flow | [docs/architecture.md](docs/architecture.md) |
| Phases and what is next | [docs/roadmap.md](docs/roadmap.md) |
| Tests, CI, hardware checklist | [docs/testing.md](docs/testing.md) |
| Releases and release candidates | [docs/releasing.md](docs/releasing.md) |
| Log format and attributes | [docs/logging.md](docs/logging.md), [ADR 0016](docs/adr/0016-log-records-follow-opentelemetry.md) |
| Decisions | [docs/adr/](docs/adr/) |

## Workflow

- Never work on `main`. Branch (`feat/`, `fix/`, `docs/`, …) → commit (Conventional
  Commits, with requirement IDs, e.g. `fix: … (MUTE-07)`) → PR → all 6 CI checks green
  (Lint, Test on two Go versions, Fuzz, Vulnerability check, Build) → squash merge.
  A ruleset on `main` enforces this.
- The owner reviews and merges PRs. **Ask before pushing, opening a PR or creating
  issues.**
- Suggest an issue for every bug found, every idea or task left for later, and every
  bug in another app (rules in [CONTRIBUTING.md](CONTRIBUTING.md#issues)); a fix's PR
  says `Fixes #n`.
- **Check the open issues** (`gh issue list`) when planning a feature or an ADR and
  before opening a PR: does the change make one obsolete, conflict with one, or finish
  one? Say so in the plan and the PR, and suggest updating or closing it (e.g. R's
  play/pause and launchers left no press for moving apps between outputs, #61).
- After a merge: `git switch main && git pull`, then delete the local branch.
- Releases: release candidates (`vX.Y.Z-rcN`) first, the hardware checklist in
  [docs/testing.md](docs/testing.md) on an installed candidate, then the release as in
  [docs/releasing.md](docs/releasing.md). Fixes ship as `0.1.x`.

## Code and docs

- Every behaviour has a requirement ID in [docs/requirements.md](docs/requirements.md);
  tests are named after it (QA-07), e.g. `TestSOLO05_PressingSoloAgainTurnsItOff`.
- Every decision gets an ADR ([docs/adr/](docs/adr/), copy `template.md`, next free
  number, add it to the index). Accepted ADRs are not rewritten, only given dated notes;
  a changed decision gets a new ADR that supersedes the old one.
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
- Before a PR: `make check`. For audio changes also `make test-audio` (runs against the
  owner's PipeWire; it adds a silent test output and inputs and removes them afterwards).
- `internal/mixer` stays pure: no I/O, events in, actions out
  ([ADR 0015](docs/adr/0015-service-architecture.md)).
- No cgo: the binary stays pure Go and static (ADR 0013). Never import
  `github.com/coreos/go-systemd/v22/sdjournal`; it needs cgo
  ([ADR 0017](docs/adr/0017-desktop-services-over-dbus.md)).

## How we work together

The owner wants to keep learning while working, not just receive finished results.

- **Plan first.** Before any non-trivial change, explain what you would do and why, and
  wait for the owner's OK. Don't edit files until they agree.
- **Explain decisions** in short steps, including the alternatives you considered.
- **The owner decides, Claude writes the code.** When something has several valid
  approaches (behaviour, error handling, data structures), stop and ask in the chat, like
  a small ADR: each option with a concrete example, what it costs, and a recommendation.
  Do not put `TODO(human)` in the code for the owner to write. Decisions that matter go
  in an ADR or a dated note on one.
- **Commands you may run yourself:** git (branch, commit, push), `make check`,
  `make test-audio`. Before each one, say in one or two sentences what you are doing and
  why; afterwards, explain the result: what passed, what failed and what it means.
  Pushing still needs the owner's OK first (see Workflow).
- **Hardware tests** (controller, LEDs, listening) need the owner: tell them exactly what
  to do and what to look for, then read the logs yourself and explain what they show.
- No workarounds for bugs in other apps (e.g. Spotify). Document them under
  *Known issues* in the README instead.
- Only access files inside this repository, never elsewhere in the home folder (not even
  Go's module cache; look at libraries through the GitHub API).
- **Privacy:** never publish data from the owner's system in commits, docs, issues or
  PRs: home paths, usernames, device names, hardware or device IDs, process numbers,
  media titles. Use placeholders (`<pid>`, `<phone name>`, `/home/you`); measured data
  keeps its structure, not its values. Check before every commit and push.
