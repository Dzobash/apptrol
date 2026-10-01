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

## Notes

- 2026-09-30: Solo is now saved too, and restored on start (STATE-04). Testing
  v0.1.0-rc2 showed that a solo lost on restart is confusing: all apps suddenly play
  again. While Apptrol is stopped, solo is still ended (SVC-07), so no app stays
  silenced by it without Apptrol running.
- 2026-09-30: Mutes made outside Apptrol are now followed (MUTE-07). Found in testing
  v0.1.0-rc2: unmuting an app in the Plasma volume applet left its M LED lit, and Apptrol
  still treated it as muted. Now an outside mute or unmute becomes the control's mute (the
  M LED follows; it is saved), for the whole app. Solo still wins: an app unmuted outside
  Apptrol while another is soloed is muted again. Apps on knobs, which have no M button,
  keep an outside mute too.
- 2026-10-01: User mutes stay when Apptrol stops (decided in #26). Releasing them would
  unmute a muted microphone after every restart and login until Apptrol starts again;
  an app that stays muted is the safer failure, and the mute shows in the desktop's
  volume applet. The LEDs still go off on stop (LED-08); a restart takes about a second,
  and they come back on start (LED-07).
- 2026-10-02: S on an input column will not be a bleep: replacing the microphone signal
  with a tone is an audio effect, out of scope, and not possible through the PulseAudio
  protocol (ADR 0003). From Phase 1.5 it is talk-over (hold to turn apps down), and R on
  an input column is a cough button (hold to mute). Both are held states, kept apart from
  the M mute like solo is, and never saved.
