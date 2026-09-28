# 0006. Mute and solo semantics

- **Status:** Accepted
- **Date:** 2026-09-28

## Context

Each slider column has S, M and R buttons with LEDs. Mute and solo interact, and mixing
desks handle multiple solos differently (additive vs. exclusive).

## Decision

- **M** toggles a *user mute* on the slider's target. Knobs have no mute.
- **S** is an exclusive toggle: pressing it solos that column; pressing S on another column
  moves the solo; pressing S on the soloed column turns solo off. It never returns to an
  earlier solo.
- Solo silences all other **app** targets in the layout (sliders and knobs), never inputs.
- User mute and solo are kept separate. An app is muted if it is user-muted or silenced by
  solo. Ending solo restores each app's own state; M pressed during solo takes effect after.
- LEDs: S lit on the soloed column only; M lit only for user mutes. Input columns show
  S, M and R lit, with M turning off when the input is muted. S on an input column is
  reserved for a future bleep function.
- User mutes are saved across restarts; solo is not.

## Consequences

- Ending solo never loses a user's own mutes.
- The M LED never reflects solo silencing, so its meaning is always "you muted this".
