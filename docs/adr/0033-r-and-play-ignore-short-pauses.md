# 0033. R and ▶ ignore pauses shorter than half a second

- **Status:** Accepted
- **Date:** 2026-10-21

## Context

R (mode `play_pause`) and ▶ show the player's real state: lit while a player reports
`Playing` (MEDIA-09, LED-03, LED-06; ADR 0018, note of 2026-10-02). They followed every
status change at once.

When you seek in a browser video with the mouse, many web players pause the video while
you scrub, and the browser reports it over MPRIS (#163). R and ▶ blink:

```
seek inside the loaded part   →  Paused  57–100 ms  → a blink without meaning
seek beyond the loaded part   →  Paused  0.7–1.2 s  → buffering: nothing plays
```

The pause is real and no app has a bug (nothing for *Known issues*). Players that do not
pause while you seek (Spotify, VLC, Elisa, Video.js, SoundCloud) never show it.

## Decision

1. **Going dark waits.** R and ▶ go dark only when a player has stayed `Paused` for
   **500 ms**. Lighting up stays immediate. Buffering, which is longer, still darkens the
   LEDs. `Stopped` and a player that goes away darken them at once.
2. **A pause Apptrol asked for shows at once.** After R or ▶ sent `Pause`, the player's
   next `Paused` darkens the LEDs without the wait, so a press does not feel slow.
3. **Buttons act on the real status.** Only the LEDs wait. R and ▶ decide between *Play*
   and *Pause* from the player's real status, so a press during the 500 ms resumes.
4. **The mixer stays pure (ADR 0015).** It has no clock: a pause that Apptrol did not
   ask for makes the mixer emit `HoldPauseLED{BusName, Seq}`; the service starts a timer
   and, after the delay, sends `PlayerPauseSettled{BusName, Seq}`. The mixer darkens the
   LEDs only if `Seq` is still the player's latest status change, so a timer for a pause
   that has ended, or for a newer one, does nothing.

```
status:  Playing ──┐ ~80 ms ┌── Playing        (seek inside the loaded part)
LED old: ●●●●●●●●●○○○○○○○●●●●●●●●
LED new: ●●●●●●●●●●●●●●●●●●●●●●●●

status:  Playing ──┐ 1 s, buffering ┌── Playing
LED new: ●●●●●●●●●●●●●○○○○○○○○○○○●●●●●
                  └ 500 ms ┘
```

5. **Logging** follows ADR 0016 (component `mixer`; no new attributes):

   | Level | Message | Attributes |
   |---|---|---|
   | debug | `short pause not shown on the LEDs` | bus name, `Identity`, `DesktopEntry` |
   | debug | `pause shown on the LEDs` | bus name, `Identity`, `DesktopEntry` |

   Together with the existing `playback status changed` (debug, desktop), the log shows
   every pause, and whether the LEDs skipped it or went dark.

   ```text
   DEBU playback status changed apptrol.component=desktop apptrol.player.bus_name=org.mpris.MediaPlayer2.chromium.instance<pid> apptrol.player.status=Paused
   DEBU playback status changed apptrol.component=desktop apptrol.player.bus_name=org.mpris.MediaPlayer2.chromium.instance<pid> apptrol.player.status=Playing
   DEBU short pause not shown on the LEDs apptrol.component=mixer apptrol.player.bus_name=org.mpris.MediaPlayer2.chromium.instance<pid> apptrol.player.identity=Chrome
   ```

## Consequences

- Mouse seeking in a browser no longer blinks R and ▶. The same holds for any pause
  shorter than 500 ms, wanted or not.
- A pause made in the browser's own window darkens the LEDs half a second late. A
  pause made with R or ▶ does not.
- If Apptrol sends `Pause` and the player ignores it, the next real `Paused` (for
  example a seek) darkens the LEDs at once, once.
- The delay is a constant in the service (`playerPauseDelay`). A setting can come later
  if someone needs another value.
