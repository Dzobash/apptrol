# 0005. The controller always has priority; no pick-up in Phase 1

- **Status:** Accepted
- **Date:** 2026-09-28

## Context

A slider's physical position and an app's actual volume can differ: after a change in the
desktop mixer, when an app starts at its own volume, or after starting without saved state.
Options were to jump to the slider's position on the next touch, or to wait until the slider
passes the current level ("pick-up", as the GoXLR does after a profile change).

The point of Apptrol is to stop using the desktop mixer altogether.

## Decision

- Moving a control always sets its target to the control's position (jump, not pick-up).
- Changes made elsewhere are not reverted actively; they are overridden on the next touch.
  Actively reverting would fight apps that lower other apps on purpose (voice-chat ducking).
- A newly appearing stream immediately gets its control's known position.
- Without saved state, positions are unknown and volumes are left alone until a control is moved.

Pick-up is introduced in Phase 2, where layout switches make it necessary.

## Consequences

- Simple, predictable behaviour in Phase 1, and no need to show a "waiting" state.
- The first touch after an external change can cause an audible jump. Accepted.

## Notes

- 2026-09-30: This decision is about volume. Mute follows changes made outside Apptrol
  (MUTE-07, see the note in ADR 0006): unlike a slider, a button's LED can show the new
  state, so the controller and the desktop never disagree about it.
