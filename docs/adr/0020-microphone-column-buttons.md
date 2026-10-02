# 0020. Microphone column buttons: M talks, S coughs or talks over

- **Status:** Accepted
- **Date:** 2026-10-02

## Context

ADR 0019 (points 8–10) gave R and S on an input column held modes: `cough`,
`talk_over` and `push_to_talk`, with M staying the mute toggle. Planning the code showed
three problems:

- **Push-to-talk on R fights with M.** In push-to-talk the microphone is muted unless
  the button is held, so M has nothing left to do. With M on, either holding R does
  nothing (M wins) or M does nothing (holding wins).
- **Push-to-talk and talk-over are the same idea**: something happens *while you
  talk*. One makes the microphone live, the other turns the music down. Users who talk
  with a button usually want both from one press.
- **Cough has no use with push-to-talk**: to stop being heard, you let go of the
  button.

M is already the microphone's on/off button, and its LED already shows whether the
microphone is live (LED-04). Push-to-talk is another way to use that same button.

## Decision

1. **M on an input column** has a mode:
   - `mute` (default): press to mute, press again to go live. The state is saved, and
     a mute made outside Apptrol is taken over (MUTE-07), as before. Used this way, it
     is a push-to-talk toggle.
   - `hold_to_talk`: the microphone is live only while M is held, and muted otherwise:
     from the moment the mode is set, after a restart, and while Apptrol is stopped (a
     microphone that turns live by itself, e.g. on an upgrade or a crash, would send
     what the user says without them knowing). Apptrol alone decides the
     microphone's mute in this mode: a change made outside Apptrol is undone and
     logged. A saved M mute of the control is dropped.

   The name `hold_to_talk` says what the user does; the example configuration
   describes both modes (CFG-17).
2. **`talk_over = true` on M** (default `false`): while the microphone is live through
   M (`hold_to_talk`: while M is held; `mute`: while not muted with M), every app
   target of the layout goes down to the input's talk-over volume. A cough does not end
   it, so the music does not jump up for a moment.
3. **S on an input column** has a mode: `cough` (default with `mute`; muted while held),
   `talk_over` (while held, apps go down; the microphone is not changed, e.g. to hear
   someone else in a call) or `off`. With `hold_to_talk` the default is `off`, and
   `cough` is rejected: letting go of M does the same.
4. **R on an input column** is `off` (default) or a launcher (ADR 0019, points 1–7).
   `play_pause` is rejected: an input has no media player.
5. **LEDs** (LED-04): S and R stay lit on an input column, marking it as an input,
   whatever their mode. M is lit only while the microphone is live, whatever muted it.
6. **Talk-over** (from S or from M):
   - Apps go down to the input's `talk_over_volume` (0–100, default 25; CFG-16), but
     never up: an app below it stays where it is. With talk-over active from several
     inputs, the lowest volume applies.
   - An app whose control has not been moved yet (position unknown, PRIO-04) is muted
     instead: Apptrol does not know its volume, so it cannot tell whether the
     talk-over volume would turn it down or up, and music played loud would stay loud.
     Mute is separate from volume, so unmuting brings back exactly the volume the app
     had.
   - A control moved during talk-over is remembered and applied when talk-over ends; a
     stream appearing during talk-over gets the lower of the two volumes.
   - When talk-over ends, every app returns to its control's position.
   - Inputs are not changed.
   The input app's setting is named `talk_over_volume` (not `talk_over` as in ADR 0019),
   so it is not confused with `talk_over = true` on M.
7. **Held states** (M in `hold_to_talk`, S in `cough` or `talk_over`) end when the
   button is released, when the controller disconnects (the release would never
   arrive), when a configuration reload changes the button's mode or the column's
   input, and when Apptrol stops. They are never saved. Apptrol acts on the release
   message (value 0) of M and S on input columns; the buttons must be Momentary (HW-01).
8. **Configuration**, per layout as in ADR 0019 point 11, with `m1` … `m8` added:

   ```toml
   [apps.mic]
   type             = "input"
   match            = ["goxlr"]
   talk_over_volume = 20                                    # default 25

   [layouts.default.buttons]
   m8 = { mode = "hold_to_talk", talk_over = true }         # hold M8: live, music down
   s8 = { mode = "talk_over" }                              # hold S8: music down only
   r8 = { app = "org.kde.plasma-systemmonitor" }            # R8 starts an app
   ```

   `apptrol check` rejects: an `m` or `s` setting on a column without an input, a mode
   that does not fit the button, `talk_over` on anything but M, `cough` with
   `hold_to_talk`, and `talk_over_volume` on an app or outside 0–100.

### Logging

9. Following ADR 0016 and the rule that every decision is logged with its reason:

   | Level | Message | Attributes (besides layout, control, app and button, LOG-14) |
   |---|---|---|
   | info | `cough on`, `cough off` | |
   | info | `talk-over on`, `talk-over off` | `apptrol.talk_over_percent` |
   | info | `talk-over mutes app: its control has not been moved yet` | (the muted app's control) |
   | info | `hold-to-talk live`, `hold-to-talk muted` | |
   | info | `changed outside Apptrol, but hold-to-talk decides` | `apptrol.device.name` |
   | info | `hold-to-talk input stays muted while Apptrol is stopped` | |
   | info | `held state ended` | `apptrol.held.reason` (`controller_disconnected`, `config_changed`, `stopping`) |
   | debug | `button has no function: its mode is off` | |

   New attributes: `apptrol.held.reason`, `apptrol.talk_over_percent`.

## Consequences

- Points 8–10 of ADR 0019, and its input-column parts of points 11, 12 and 15, are
  replaced by this record.
- M on an input column no longer always toggles: in `hold_to_talk` it is held.
  MUTE-01, MUTE-07, SOLO-07 and LED-04 change with it.
- Holding M and S together (talk and cough) works but has no use with
  `hold_to_talk`; the configuration rejects it rather than guessing.
- The on-screen display (Phase 1.6) can show these states and hints, e.g. "cough has
  no use with hold-to-talk: release M to mute".
