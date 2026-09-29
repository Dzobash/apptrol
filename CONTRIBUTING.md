# Contributing to Apptrol

Thanks for your interest! Apptrol is a small project; issues and pull requests are welcome.

## Before you start

- Read the [requirements](docs/requirements.md) and the [roadmap](docs/roadmap.md).
  Features outside the current phase are welcome as ideas, but may wait for their phase.
- For a larger change, open an issue first so we can agree on the approach.
- Report security problems privately, see [SECURITY.md](SECURITY.md).
- Decisions that affect the design are recorded as [ADRs](docs/adr/). If your change
  alters one, add a new record that supersedes it.

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
4. Merge with **Squash and merge** once CI is green. The PR title becomes the single commit
   on `main`, so it must follow Conventional Commits (it feeds the release notes).
5. Delete the branch, then update your local `main`:
   ```bash
   git switch main && git pull
   git branch -d feat/config-loading
   ```

Repository settings that support this (**Settings → General → Pull Requests**): allow only
*squash merging*, set the default commit message to *pull request title*, and enable
*automatically delete head branches*. Once the repository is public, a branch rule for
`main` requires pull requests and a green CI (QA-02).

## Commits and pull requests

- Use [Conventional Commits](https://www.conventionalcommits.org/):
  `feat: …`, `fix: …`, `docs: …`, `refactor: …`, `test: …`, `ci: …`, `chore: …`.
- Reference requirement IDs where they apply, e.g. `feat: exclusive solo toggle (SOLO-04, SOLO-05)`.
- Add an entry under **Unreleased** in [CHANGELOG.md](CHANGELOG.md) for user-visible changes.
- Keep pull requests focused on one thing.

## Code

Requires Go 1.24 or newer.

```bash
make check    # the same checks CI runs: format, vet, lint, tests
make cover    # tests with coverage report (coverage.html)
make test-audio  # integration tests against your running PipeWire
make build    # bin/apptrol with version information
make help     # all targets
```

`make lint` needs [golangci-lint](https://golangci-lint.run/welcome/install/). How to test,
and what CI checks, is described in [docs/testing.md](docs/testing.md).

Project layout:

| Path | Contents |
|---|---|
| `cmd/apptrol/` | The `apptrol` command: argument parsing and wiring |
| `internal/` | All application code, not importable by other projects |
| `internal/version/` | Build information (set at link time) |
| `docs/` | Requirements, roadmap, configuration reference, ADRs |
| `examples/` | Example configuration (built into the binary for the first start) |
| `packaging/` | systemd unit and other packaging files |

Further packages under `internal/` are added in Phase 1 (MIDI, audio, config, state, logging).

- Go, formatted with `gofmt`; CI runs `go vet`, `golangci-lint` and the tests.
- New behaviour comes with tests; see [docs/testing.md](docs/testing.md).

## AI-assisted contributions

AI-assisted contributions are fine. You are responsible for understanding and testing
what you submit.

## Versioning

[Semantic Versioning](https://semver.org). Until `1.0.0`, the configuration format may
change between minor versions; such changes are called out in the changelog.
