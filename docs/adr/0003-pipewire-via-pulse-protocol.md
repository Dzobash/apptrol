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
