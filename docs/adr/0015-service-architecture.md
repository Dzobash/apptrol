# 0015. Service architecture and controller access

- **Status:** Accepted
- **Date:** 2026-09-29

## Context

Phase 1 needs a structure in which all behaviour can be tested without hardware (QA-06),
without concurrency bugs, and which later phases (layouts, GUI) can extend.

For the controller there are two ways in on Linux: raw MIDI (`/dev/snd/midiC*D*`), which is
simple and pure Go but allows one reader at a time; and the ALSA sequencer
(`/dev/snd/seq`), which is shared between programs but considerably more work (or needs
the C library `libasound`, conflicting with ADR 0013). On the reference system (Kubuntu,
PipeWire 1.6.2), `amidi` reads the nanoKONTROL2 directly while PipeWire runs, and PipeWire's
sequencer bridge is not subscribed to it.

## Decision

- **Pure core, thin adapters:** `internal/mixer` holds all behaviour as a function from
  events to actions, with no I/O. Controller, audio, config, state and logging are
  adapters behind small interfaces. See `docs/architecture.md`.
- **One event loop** in `internal/service` owns all state; adapters communicate with it
  through channels.
- **Raw MIDI in pure Go** for the controller, found by its ALSA card id rather than card
  number, with hot-plug via watching `/dev/snd`.
- The controller interface is independent of the access method, so an ALSA sequencer
  backend can be added later without touching the mixer.

## Consequences

- Nearly all requirements are testable with unit tests against fakes.
- `CGO_ENABLED=0` and simple cross-compilation stay (ADR 0013).
- While Apptrol runs, no other program can use the controller; if one holds it, Apptrol
  reports it and retries. If this becomes a problem — for example PipeWire starting to
  claim the device — the sequencer backend is the planned answer.

## Implementation notes (2026-09-30)

- Hot-plug is detected by looking for the device once a second while it is away, not by
  watching `/dev/snd`: simpler, and just as quick in practice. Unplugging ends the pending
  read with an error.
- The controller and audio interfaces live in `internal/service` (`Controller`, `Audio`),
  next to the loop that uses them.
