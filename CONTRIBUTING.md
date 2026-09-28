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

Requires Go 1.24 or newer.

```bash
make check    # the same checks CI runs: format, vet, lint, tests
make cover    # tests with coverage report (coverage.html)
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
| `examples/` | Example configuration |
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
