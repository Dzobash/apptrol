# 0003. Talk to PipeWire through the PulseAudio protocol

- **Status:** Accepted
- **Date:** 2026-09-28

## Context

Apptrol needs to list playback streams and capture devices, set their volume and mute,
and be notified when streams appear or disappear. PipeWire offers this through its native
API and through `pipewire-pulse`, its PulseAudio-compatible server, which every mainstream
distribution with PipeWire ships.

## Decision

Apptrol uses the PulseAudio protocol, through a pure-Go client library
(`github.com/jfreymuth/pulse`), including its subscription events for new and removed streams.

## Consequences

- Works on PipeWire and, as a bonus, on systems still running PulseAudio.
- No C bindings are needed for audio.
- Features only available in the native PipeWire API (e.g. per-channel filters) are out of
  reach; none are needed for the current requirements. Revisit if that changes.

## Notes

- 2026-10-02: The library was reviewed and is kept. `jfreymuth/pulse` v0.1.3 (August
  2026) is maintained (commits from June to August 2026), MIT, about 3,700 lines of Go.
  Apptrol uses only its `proto` package, inside `internal/audio/pulse` behind the
  `service.Audio` interface; its open issues concern playback streams, which Apptrol does
  not use. Alternatives: `mafik/pulseaudio` (inactive since 2024) and its fork
  `lawl/pulseaudio` (deleted); `libpulse` through cgo (conflicts with ADR 0013);
  PipeWire's native API (no pure-Go client); running `pactl` (a process per change,
  parsing text). The PulseAudio protocol no longer changes much, so few releases are
  expected. If the library is abandoned, it is small enough to fork.
