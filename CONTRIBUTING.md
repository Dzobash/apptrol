# Contributing to Apptrol

Thanks for your interest! Apptrol is a small project; issues and pull requests are welcome.
Please follow the [code of conduct](CODE_OF_CONDUCT.md).

## Before you start

- Read the [requirements](docs/requirements.md) and the [roadmap](docs/roadmap.md).
  Features outside the current phase are welcome as ideas, but may wait for their phase.
- For a larger change, open an issue first so we can agree on the approach.
- Report security problems privately, see [SECURITY.md](SECURITY.md).
- Every decision gets an [ADR](docs/adr/): copy `template.md`, take the next free
  number, add it to the index. Accepted ADRs are not rewritten, only given dated notes;
  a changed decision gets a new ADR that supersedes the old one.

## Issues

Issues are the project's public memory: what users search for, and what waits to be
done. Open one for:

1. **A bug found in Apptrol**, even if it is fixed the same day: the fix's pull request
   says `Fixes #n`, and users of the released version find the cause and a workaround.
   A bug that was never in a release needs no issue.
2. **An idea or task that is not done right away**, so it is not lost. Ideas without a
   planned release get the label `backlog`.
3. **A problem caused by another program** that Apptrol does not work around (see the
   [known issues](docs/known-issues.md)), with the label `upstream`, closed right away as *not
   planned*: it can be found, without promising a fix.

A security problem is never a public issue: report it privately, as
[SECURITY.md](SECURITY.md) says.

An idea that is dropped is closed as *not planned*, with the reason, and removed from the
roadmap and requirements in the same change.

Small changes made right away (docs, refactoring) need no issue; the pull request is
enough.

### Milestones

A milestone says what is left in a release; the [roadmap](docs/roadmap.md) says why and
in what order.

- Every release has a milestone, a patch too. Its title is the version, then the
  phase's name, so milestones sort by version; a patch keeps its phase's name:
  ```text
  0.3.0 — These Go to Eleven     the phase's release
  0.3.1 — These Go to Eleven     a patch: same name, its own milestone
  ```
- An issue planned for a release gets its milestone; a `backlog` issue gets none. A
  milestone is made as soon as its release is planned or gets an issue.
- An issue that moves to another phase moves to that phase's milestone. A phase that
  moves to another version gets its milestone renamed.
- A pull request that finishes a planned issue gets the same milestone.
- Before the release candidate, each open issue is moved or kept, with the decision in
  the issue (e.g. "stays: checked on the candidate"). Before the release, none is open
  any more, and the milestone is closed after it ([releasing.md](docs/releasing.md)).

## Branches and pull requests

`main` must always build and pass CI, so a release can be tagged from it at any time.
All changes go through a branch and a pull request (GitHub Flow):

1. Start from an up-to-date `main` and create a branch:
   ```bash
   git switch main && git pull
   git switch -c feat/config-loading
   ```
   Branch names use the commit type as prefix: `feat/`, `fix/`, `docs/`, `ci/`, `chore/`,
   `refactor/`, `test/`.
2. Commit and push the branch:
   ```bash
   git push -u origin feat/config-loading
   ```
3. Open a pull request into `main` on GitHub. CI runs on it; fill in the PR template.
4. Merge with **Squash and merge** once the 8 required checks are green: Lint, Test (on
   two Go versions), Fuzz, Vulnerability check, Build, Packaging and Systemd in
   containers. The PR title
   becomes the single commit on `main`, so it must follow Conventional Commits (it
   feeds the release notes).
5. Update your local `main`, then delete the branch:
   ```bash
   git switch main
   git pull
   git branch -D feat/config-loading
   ```
   `-D`, because the squash merge put one new commit on `main`: git does not see the
   branch's own commits as merged, so `-d` refuses.

Repository settings that support this (**Settings → General → Pull Requests**): allow only
*squash merging*, set the default commit message to *pull request title*, and enable
*automatically delete head branches*. A ruleset on `main` requires pull requests and a
green CI (QA-02).

Coding agents follow [AGENTS.md](AGENTS.md) on top of these steps: reviews
([Review](AGENTS.md#review)), asking before a push and draft pull requests
([Publish](AGENTS.md#publish)), and deleting a merged branch, also in a worktree
([Clean up](AGENTS.md#clean-up)).

## Commits and pull requests

- Use [Conventional Commits](https://www.conventionalcommits.org/):
  `feat: …`, `fix: …`, `docs: …`, `refactor: …`, `test: …`, `ci: …`, `chore: …`.
- Reference requirement IDs where they apply, e.g. `feat: exclusive solo toggle (SOLO-04, SOLO-05)`.
- A commit written with an AI agent names its model in a `Co-Authored-By:` line, e.g.
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`; see
  [AGENTS.md › Build](AGENTS.md#build).
- Add an entry under **Unreleased** in [CHANGELOG.md](CHANGELOG.md) for user-visible changes.
- **Whenever a new feature or behaviour change is added, also add or update its test
  cases and fixtures in [docs/testing/](docs/testing/cases/README.md)** (same format), in
  addition to docs, roadmap, changelog and ADR. Every roadmap phase must have test cases
  before release (QA-14, [ADR 0031](docs/adr/0031-manual-test-cases-as-docs.md)).
- Keep pull requests focused on one thing.

## Log messages

Follow [ADR 0016](docs/adr/0016-log-records-follow-opentelemetry.md): a short, fixed
message; values as attributes named by constants in `internal/logattr`; the component on
every record (packages get it from their logger); `logattr.Error(errorType, err)` for
errors. Records about a control use the mixer's `about` helper, so they name the layout,
control and app. A new attribute or error type also needs a row in
[`docs/log-attributes.md`](docs/log-attributes.md). `go test ./internal/logattr` fails on log calls that
break these rules and on attributes missing from the reference.

A new feature must be troubleshootable from its logs alone. Plan the logs together with
the design: log every decision together with its reason (for example, why a media player
belongs to a column: `apptrol.player.matched_by`), and log candidates that were rejected
or ignored at debug level, so `level = "debug"` shows everything Apptrol saw. A feature's
ADR lists its records (level, message, attributes); see ADR 0018, point 9.

## Code

Requires Go 1.24 or newer. Building and installing without changing the code is described
in the installation guide, under [Building from source](docs/install.md#building-from-source).

```bash
make check    # format, vet, lint, tests, AppStream and portable build
make cover    # tests with coverage report (coverage.html)
make test-audio  # integration tests against your running PipeWire
make test-desktop  # D-Bus integration tests in a private session bus
make test-launcher # starts harmless test units in your systemd user manager
make test-systemd # Status line in rootless Ubuntu and Fedora containers
make snapshot     # release packages into dist/ (needs GoReleaser)
make test-install # install those packages with apt and dnf in the containers
make container-run RECIPE=sample # audited recipe, fixed mounts and limits
make test-clean   # remove the folders and logs that failed container runs keep
make build    # bin/apptrol with version information
make help     # all targets
```

`make lint` needs [golangci-lint](https://golangci-lint.run/welcome/install/). How to test,
and what CI checks, is described in [docs/testing.md](docs/testing.md).

Project layout:

| Path | Contents |
|---|---|
| `cmd/apptrol/` | The `apptrol` command: `run`, `list`, `check`, `test`, `version` |
| `internal/service/` | The event loop that ties everything together |
| `internal/mixer/` | All behaviour, without I/O: matching, volume, mute, solo, LEDs |
| `internal/controller/`, `…/rawmidi/` | MIDI decoding and the nanoKONTROL2 map; the raw MIDI device |
| `internal/audio/pulse/` | Connection to PipeWire through the PulseAudio protocol |
| `internal/desktop/` | The D-Bus session bus: finding and following media players |
| `internal/launcher/` | Installed apps and their desktop files, for the launcher buttons |
| `internal/config/` | Loading, validating and watching the configuration file |
| `internal/state/` | Saved positions, mutes and solo |
| `internal/logging/` | Log outputs and formats |
| `internal/logattr/` | Log attribute names and error types (ADR 0016) |
| `internal/version/` | Build information (set at link time) |
| `internal/repocheck/` | Tests for agent command-list parity and container restrictions (QA-15, QA-16) |
| `test/container/` | Rootless container runner, distro images, limit and Status checks (QA-16) |
| `.audit/recipes/`, `.audit/runs.log` | Reviewed container recipes and one record per attempt; committed |
| `.audit/logs/`, `.tmp/` | Local failure logs and writable container workspaces; ignored |
| `docs/` | The documentation, grouped in [docs/README.md](docs/README.md); also the website's source |
| `docs/assets/` | Logo files (`brand/`, MIT) and third-party photos (`photos/`, each with its own license) |
| `examples/` | Example configuration (built into the binary for the first start) |
| `packaging/` | systemd unit, package scripts, Debian copyright and AppStream metadata (what software centers show) |

How the packages work together: [docs/architecture.md](docs/architecture.md).

- Go, formatted with `gofmt`; CI runs `go vet`, `golangci-lint` and the tests.
- No cgo: Apptrol is built with `CGO_ENABLED=0` (ADR 0013). Never import
  `github.com/coreos/go-systemd/v22/sdjournal`; it needs cgo (ADR 0017).
- OS-specific code lives only in adapters. Keep the shared packages building for
  other systems: `make check` cross-compiles `mixer`, `config`, `state`, `controller`
  (without `rawmidi`), `logattr` and `version` for Windows without cgo (NFR-11,
  ADR 0013). This guards the shared code; Windows adapters and path changes wait for #92.
- Settings describe what the user wants, not how Linux implements it: use
  `at_login_screen`, for example, rather than a setting named after logind. The
  session adapter translates the OS's information into that state (NFR-11, ADR 0015).
- New behaviour comes with tests; see [docs/testing.md](docs/testing.md).
- A bug fix comes with a test that fails without the fix (QA-11).
- Images you did not make yourself go in `docs/assets/photos/`, only under a license that
  allows it, with author, source, license and changes listed in its README.

## Documentation

`docs/` is also the source of the project's website, which pulls it at build time and
renders it ([ADR 0028](docs/adr/0028-docs-as-code-for-the-website.md)). So that it reads
the same on GitHub and on the website:

- **Plain Markdown.** HTML only for images and line breaks (`<p align="center">`,
  `<img>`, `<br>`); never `<script>`, `<iframe>`, `<style>` or `on…=` attributes.
- **Relative links inside `docs/`** (`config.md#hearing-safety`, `assets/photos/…`).
  A link to a file outside `docs/` is a full GitHub URL
  (`https://github.com/Dzobash/apptrol/blob/main/LICENSE`), as that file is not on the
  website.
- **Every new page gets a place in [docs/README.md](docs/README.md)**, in one group:
  tutorial, how-to guide, reference, explanation or project. Not sure where it belongs?
  Ask what the reader has in their hand: a problem → how-to; a word or value they are
  missing → reference; curiosity → explanation; nothing yet, they want to learn →
  tutorial.
- **Show it, then explain it.** Wherever a setting or a behaviour can be shown visually —
  a timeline, the controller's LEDs, a sequence of states, a small graph — add a
  plain-text diagram in a fenced code block on top of the written explanation, never
  instead of it (e.g. *Several users* in
  [docs/config.md](docs/config.md#several-users)). Keep it under about 100 characters
  wide. This holds outside `docs/` too (README, rules, pull requests):
  ```
  keep:     you keep it ─────┤ released ─────┤ you take it again
  release:  released ────────────────────────┤ you take it again
  ```
- **[docs/roadmap.md](docs/roadmap.md) is the only roadmap.** Every new phase gets a
  pop-culture name (its topic stays as the subtitle), a one-line **Goal** and a
  **Done when** line. Dropped ideas go to *Out of roadmap* with the reason and a link.

## AI-assisted contributions

AI-assisted contributions are fine. You are responsible for understanding and testing
what you submit.

## Versioning

[Semantic Versioning](https://semver.org). Until `1.0.0`, the configuration format may
change between minor versions; such changes are called out in the changelog.
