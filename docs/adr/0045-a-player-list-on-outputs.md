# 0045. A player list on outputs

- **Status:** Accepted
- **Date:** 2026-11-03

Supersedes ADR 0027 point 8 and the "R stays off" of ADR 0036 for outputs with a player
list; extends ADR 0018 points 3, 5 and 6. Requirements: MEDIA-12 (new), OUT-09,
OUT-10, CFG-14, CFG-15, CFG-20, MEDIA-06. Issue:
[#231](https://github.com/Dzobash/apptrol/issues/231).

## Context

Some clients play into their own virtual output instead of a sound card. One such client
shows as a sink, while its audio stream keeps the name of the app it replaces. Its volume
can then only be set on the output (`[outputs]`), and play/pause only works on an app
column, as R on an output is `off` or a launcher (OUT-09). One column cannot do both:

```text
[apps.x]    match ["<player>"]   R plays/pauses, LED follows   slider does nothing (no stream has that name)
[outputs.x] match ["<sink>"]     slider works                  R cannot play/pause; a launcher's LED never lights
```

The workarounds were a launcher running `playerctl -p <player> play-pause` (a toggle, no
LED), the `[media] player` pin (only for apps), or the app on the slider and the output
on a knob of the same column.

Deriving the player from the streams routed to the output was considered and dropped:
it needs a stream-to-player mapping through PipeWire, which is fragile and hard to log.

## Decision

1. **A `player` key on `[outputs.<id>]`**: a nonempty list of case-insensitive name
   fragments, matched against a media player as an app's `match` is (MEDIA-03): the bus
   name without `.instance…`, `Identity`, `DesktopEntry`. The output's `match` keeps
   choosing the device only; it is never compared with players.

   ```toml
   [outputs.speakers]
   name   = "Speakers"
   match  = ["<sink name>"]
   player = ["<player name>"]      # R plays and pauses it, and its LED follows
   ```

2. **Only a fixed output has it.** `player` is rejected in `[apps]` (their `match`
   already finds the player), in `[inputs]` (no players) and on an output with
   `device = "default"`: anything may play over the default output, so Apptrol cannot
   tell which player R would mean (owner decision on #231). Each error names the entry
   and says why. An empty list, or an empty fragment, is rejected as for `match`.
3. **First control wins**, as everywhere (CFG-12): a player that matches an app and an
   output, or two outputs, belongs to the first control, sliders 1–8, then knobs 1–8.
   No new warning (owner decision on #231): the info record `media player matched`
   already names the winner, with `apptrol.app.type=output` and
   `apptrol.player.matched_by`.

   ```text
   [outputs.speakers]  player = ["<player>"]   slider1   ← first control, gets the player
   [apps.music]        match  = ["<player>"]   slider2
   ```

4. **R on the column of an output with `player`** defaults to `play_pause` and acts as
   on an app column (MEDIA-07, MEDIA-08), with the LED of MEDIA-09 and its pause delay
   (ADR 0033); `off` and a launcher can be set. Without `player`, OUT-09 holds: `off` or
   a launcher, `play_pause` rejected with an error that says the output has no player
   list. R never touches the device, so it also works on a control whose device an
   earlier control owns (OUT-04, ADR 0035), while that control's M stays ignored
   (decided with the owner while building #231):

   ```text
   slider6 = also  (match "<sink>")                      owns the device
   slider7 = main  (match "<sink>", player "<player>")   M7 ignored, R7 plays/pauses <player>
   ```

5. **`[media] player` may name an output with `player`**: ◀◀ ▶▶ ■ ▶ then follow that
   output's players (MEDIA-06), whether or not it is on a control (owner's third item on
   #231). An output without `player`, or one that follows the default output, stays
   rejected, and the error says why.
6. **In the mixer** (pure, ADR 0015), one map holds the fragments players are matched
   against: an app's match list, an output's player list, none for inputs. Matching,
   the pin and R read it; the device code is unchanged. `apptrol check` shows the list
   after the output's match (`output: <sink>; player: <player>`).

### Logging

No new record and no new attribute. The records of ADR 0018 (point 9) now also name an
output, with `apptrol.app.type=output`:

| Level | Message | Attributes (besides layout, control and target, LOG-14) |
|---|---|---|
| info | `media player matched` | bus name, `Identity`, `DesktopEntry`, `matched_by` |
| debug | `media player not matched` | bus name, `Identity`, `DesktopEntry` |
| info | `paused`, `playing` (R) | button, bus name of each player commanded |
| info | `media key` (◀◀ ▶▶ ■ ▶) | button, bus name, `selection` (`pinned`) |
| debug | `button has no function: no media player for this app` | button |
| debug | `button has no function: its mode is off` | button (an output without `player`) |

Example, troubleshooting "why does R7 not pause the player?":

```text
INFO media player matched apptrol.component=mixer apptrol.layout=default apptrol.control=slider7 apptrol.app.id=speakers apptrol.app.name=Speakers apptrol.app.type=output apptrol.player.bus_name=org.mpris.MediaPlayer2.<player> apptrol.player.matched_by=bus_name
INFO paused apptrol.component=mixer apptrol.layout=default apptrol.control=slider7 apptrol.app.id=speakers apptrol.app.name=Speakers apptrol.app.type=output apptrol.button=R7 apptrol.player.bus_name=org.mpris.MediaPlayer2.<player>
```

A player matched to an app on an earlier control shows that app in `media player
matched` instead, which is the answer.

## Consequences

- One column controls both the volume of a client's own output and its playback.
- The messages `no media player for this app` and `the pinned app has no media player`
  keep their wording for outputs; `apptrol.app.type` tells which it is.
- A player list on the default output stays out: if it is wanted later (e.g. "whatever
  plays on the default"), it needs its own decision.
- The overlap warning of CFG-12 still compares `match` lists only; an app and an output
  that share a player fragment are not warned about (point 3).
