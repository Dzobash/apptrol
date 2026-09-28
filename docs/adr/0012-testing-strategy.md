# 0012. Testing strategy

- **Status:** Accepted
- **Date:** 2026-09-28

## Context

Apptrol talks to two things that do not exist on CI machines: a USB MIDI controller and the
user's PipeWire session. Most of its behaviour, however, is plain logic: which stream belongs
to which control, what mute and solo do, which LEDs are lit, whether a config is valid, what
is saved. The project is largely AI-written, so automated checks carry more weight than usual.

## Decision

Testing happens in layers:

1. **Unit tests with fakes** — the controller and the audio server sit behind small Go
   interfaces. Tests use in-memory fakes, so all logic is tested without hardware.
   Table-driven tests; test names include requirement IDs where they apply.
2. **Fuzz tests** — Go's built-in fuzzing for config parsing and MIDI decoding. CI runs them
   briefly on every push; longer runs locally.
3. **Integration tests** — build tag `integration`; CI starts a headless PipeWire with
   `pipewire-pulse`, plays dummy streams and checks that volumes and mutes arrive.
4. **Static checks** — `gofmt`, `go vet`, `golangci-lint` (config in `.golangci.yml`) and
   `govulncheck`.
5. **Coverage gate** — CI fails below a minimum total coverage (70 % to start), raised as
   Phase 1 grows.
6. **Manual hardware checklist** — [docs/testing.md](../testing.md), completed on a real
   controller before each release.

CI runs on every push and pull request, against the minimum Go version in `go.mod` and the
latest stable Go. `make check` runs the same checks locally.

## Consequences

- Code must be written so that hardware and audio access can be swapped for fakes. This
  shapes Phase 1's package design and is cheaper to do from the start than later.
- Whether a virtual MIDI device can be created on GitHub's runners is unknown; until it is
  confirmed, the MIDI layer is covered by fakes and the manual checklist.
- Coverage numbers are a floor, not a goal: tests should check behaviour, not just run lines.
