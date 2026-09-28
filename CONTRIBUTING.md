# Contributing to Apptrol

Thanks for your interest! Apptrol is a small project; issues and pull requests are welcome.

## Before you start

- Read the [requirements](docs/requirements.md) and the [roadmap](docs/roadmap.md).
  Features outside the current phase are welcome as ideas, but may wait for their phase.
- For a larger change, open an issue first so we can agree on the approach.
- Decisions that affect the design are recorded as [ADRs](docs/adr/). If your change
  alters one, add a new record that supersedes it.

## Commits and pull requests

- Use [Conventional Commits](https://www.conventionalcommits.org/):
  `feat: …`, `fix: …`, `docs: …`, `refactor: …`, `test: …`, `ci: …`, `chore: …`.
- Reference requirement IDs where they apply, e.g. `feat: exclusive solo toggle (SOLO-04, SOLO-05)`.
- Add an entry under **Unreleased** in [CHANGELOG.md](CHANGELOG.md) for user-visible changes.
- Keep pull requests focused on one thing.

## Code

- Go, formatted with `gofmt`; CI runs `go vet`, `golangci-lint` and the tests.
- New behaviour comes with tests where it can be tested without hardware
  (matching, mute/solo logic, config validation, state handling).

## AI-assisted contributions

AI-assisted contributions are fine. You are responsible for understanding and testing
what you submit.

## Versioning

[Semantic Versioning](https://semver.org). Until `1.0.0`, the configuration format may
change between minor versions; such changes are called out in the changelog.
