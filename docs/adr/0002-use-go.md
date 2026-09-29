# 0002. Write the service in Go

- **Status:** Accepted
- **Date:** 2026-09-28

## Context

Apptrol is a long-running background service. Candidates were Python, Go and Rust.

- **Python** is quickest to develop and has the best GUI options, but installing and
  removing Python applications cleanly is awkward for users.
- **Rust** produces a single binary and has an existing nanoKONTROL2 project (rustkorg)
  to learn from, but is more verbose and slower to develop in.
- **Go** produces a single binary, is simple to read, builds quickly, and has usable
  libraries for the PulseAudio protocol, TOML and logging.

## Decision

The service is written in Go.

The GUI (Phase 3) talks to the service and does not have to use the same language; its
toolkit is decided separately.

## Consequences

- One static-ish binary, easy to package as .deb/.rpm and to install or remove.
- Go's GUI options are weaker; this is acceptable because the GUI is a separate component.
- MIDI access may need cgo and the ALSA library (`libasound2`), depending on the MIDI
  library chosen during Phase 1. (Resolved by ADR 0015: raw MIDI in pure Go, no cgo.)
