# 0027. Output volume: a control for a whole output device

- **Status:** Accepted
- **Date:** 2026-10-07

**2026-10-21 decision update:** [ADR 0035](0035-first-control-owns-matched-output.md)
supersedes point 4 for fixed outputs: the first control whose chosen device matches
owns that device. Later conflicting controls cannot set its volume or mute.
The shared-device info record in point 12 becomes a conflict warning for fixed
outputs, naming the winning and ignored controls.

**2026-10-23 decision update:** [ADR 0036](0036-no-solo-on-outputs.md) supersedes
point 7 and the solo part of point 9: outputs have no solo, as next to app solo it
made everything too complicated. S on an output column does nothing; M's LED works as
point 9 says.

## Context

Apptrol sets the volume of apps and of inputs, but not of an output device (#85). To
turn everything down at once (the speakers, the main channel of an audio interface such
as a GoXLR) the desktop's volume applet or a knob on the device is still needed.

Setups differ widely: one output on a laptop, two or three on a desktop, five on a GoXLR
Mini (its outputs show up as separate devices). Some users want a control for one fixed
device; others want "the main volume", whatever output the desktop uses right now, which
changes when headphones are plugged in or another output is chosen in the applet.

PipeWire multiplies each stream's volume by its output's volume, so an output control
turns the whole mix up or down and keeps the balance the app controls set.

Apptrol talks to the audio server, not to the desktop (ADR 0003). The server knows the
default output and reports every change of it, on any desktop and distribution with
PipeWire or PulseAudio.

## Decision

1. **Outputs are a third kind of target**, defined in `[outputs.<id>]`
   ([ADR 0026](0026-separate-tables-for-apps-inputs-outputs.md)) and assigned to a
   slider or knob like an app or input. The control sets the output device's volume,
   mapped as for any target, up to its `max_volume` (CTRL-02, CTRL-03). Stream volumes
   are not changed. `max_volume` above 100 % is allowed, as on apps, but weighs more:
   an app's volume is still scaled by its output afterwards, while an output's is the
   last software volume before the speakers or headphones. The hearing-safety warning
   (CFG-23) therefore covers outputs explicitly in the example configuration and in
   `docs/config.md`.
2. **A fixed device or the system's default**, exactly one of the two:

   ```toml
   [outputs.main]
   name   = "Main volume"
   device = "default"          # the output the system uses right now

   [outputs.speakers]
   name  = "Speakers"
   match = ["Speaker"]         # always this device
   ```

   - `match` works as for inputs: a case-insensitive part of the output's name or
     description. Monitors are never outputs. When several outputs match, the first by
     name is used and a warning names all of them; the chosen output is kept while it
     still matches.
   - `device = "default"` follows the audio server's default output. When it changes,
     the control moves to the new default at once. `"default"` is the only value;
     `device` is not accepted on apps or inputs (a default input may use it later,
     without a change of format).
   - Both keys, or neither, are rejected.
   - The name `device = "default"` was chosen over `match = ["@default"]` (one key with
     two meanings, open to mixups like `["@default", "Speaker"]`) and over
     `follow_default = true` (only yes or no, no room for later values).
3. **Volume when the default changes.** The new default output keeps its own volume
   until the control is moved; the control's mute applies at once. Switching from
   loud speakers to headphones must not put the speakers' level on the headphones; this is a
   hearing-safety rule, and the documentation explains it.
   The previous default keeps its volume; if Apptrol muted it and no other control
   holds it, it is unmuted. Without a default output (none reported), the control does
   nothing.
4. **The same device on two controls** (a `default` output that is currently also a
   fixed output) is allowed and logged once at info. Each control sets the volume when
   it is moved; the device is muted while any of its controls mutes it.
5. **Outside changes**: volume keys and the desktop's applet change the default
   output's volume often. As for apps, the controller has priority but does not revert
   them; the next move of the control sets its position (PRIO-01, PRIO-02). An outside
   mute or unmute is taken over (MUTE-07).
6. **M** mutes and unmutes the output (MUTE-01); the mute is saved per control as for
   every target.
7. **S** solos the output: every other output on the layout's controls is muted while it
   is on. It works like app solo (one soloed output at a time, pressing S again ends it,
   saved and restored, ended on shutdown so no output stays muted), but separately:
   output solo touches only outputs, app solo only apps, and one of each can be on at
   the same time. The soloed output's device is never muted by its own solo, even when
   another control holds it too. Inputs are never touched.
8. **R** is `off` (default) or a launcher, as on an input column. `play_pause` is
   rejected. R never changes the output or the system's default output.
9. **LEDs** as on an app column: S lit while soloed, M lit while muted with M (not when
   only silenced by solo), R off. There is no mark for outputs and no blinking: an
   output that is not muted looks like an empty column, and the documentation says so.
   A steady mark and an optional flash on R were considered and dropped as more
   complicated than they are worth.
10. **Talk-over** (INPUT-04) does not change outputs.
11. **`apptrol list`** gets a section of output devices like the one of input devices:
    description, name, the control each is on, and `*` on the system's default.

### Logging

12. Following ADR 0016; every record about a control names the layout, control and
    target (LOG-14), with `apptrol.app.type` = `output`:

    | Level | Message | Attributes |
    |---|---|---|
    | info | `output matched` | `apptrol.device.name`, `apptrol.device.description`, `apptrol.device.selected_by` (`match` or `default`) |
    | warn | `several output devices match; using the first (make the match more specific: apptrol list shows each device's unique name)` | `apptrol.device.name`, `apptrol.device.matches` |
    | warn | `no output device matches` | `apptrol.app.match` |
    | info | `default output changed` | `apptrol.device.name`, `apptrol.device.previous_name` |
    | info | `default output keeps its volume until the control is moved` | `apptrol.device.name` |
    | info | `no default output; the control does nothing until there is one` | |
    | info | `two controls set the same output` | `apptrol.device.name`, `apptrol.device.controls` |
    | info | `solo on`, `solo off` (as for apps) | |
    | debug | `output device not on a control` | `apptrol.device.name`, `apptrol.device.description` |

    New attributes: `apptrol.device.selected_by`, `apptrol.device.previous_name`,
    `apptrol.device.controls`. `apptrol.device.name` and `apptrol.device.description`
    now describe input and output devices.

## Consequences

- New requirements OUT-01 to OUT-13 and CFG-18 to CFG-22 (CFG-23, the
  hearing-safety warning, applies to `max_volume` already today); when built, CTRL-06, SOLO-02,
  SOLO-03, SVC-07, LED-05 and CFG-10 name outputs too.
- The mixer tracks output devices and the default output next to input devices; the
  audio adapter reports sinks, their volume and mute, and changes of the default.
- An integration test needs a second test output to switch the default between.
- The hardware checklist gets rows for a fixed output, the default output while
  switching outputs in the desktop's applet, output solo, and an output on a knob.
- Built in 0.3.0, after the separate tables (ADR 0026).

## Notes

- 2026-10-20: Built in steps (#85). The first one, a fixed output (`match`) on a slider
  or knob setting its device's volume (OUT-01, OUT-02), adds records the logging table
  (point 12) does not have, approved by the owner:

  | Level | Message | Attributes |
  |---|---|---|
  | info | `output device gone; the control does nothing until a device matches` | `apptrol.device.name` |
  | debug | `button has no function: not built yet on an output column` | `apptrol.button` |
  | debug | `audio server changed the volume of a new output; setting it again` (audio adapter) | `apptrol.device.name`, `apptrol.volume_percent`, `apptrol.wanted_percent` |

  The first is how a fixed output's log explains a control that does nothing after its
  device is unplugged, as `no default output; …` does for the default. The second is
  temporary: M and S on an output column do nothing until mute (OUT-06) and output
  solo (OUT-07) follow; R works as point 8 says. The third mirrors the new-stream and
  new-input guard (PRIO-03): for 3 seconds after an output appears, a volume the audio
  server restores over Apptrol's is set again. It defends only the latest position (a
  slider move waits for a guard write in progress) and only while a control holds the
  output: a reload or a device change that leaves the output on no control releases it
  (`ReleaseOutput`, CTRL-06), so a volume changed elsewhere stays. `two controls set the same output`
  (point 4) also covers two fixed outputs that match the same device, and names the
  first of the controls in `apptrol.control`. Every channel gets the same volume, as for
  streams and inputs, so a left/right balance set elsewhere is lost (known issue).
  `device = "default"` stays left out of the mixer, with the narrower warning
  `following the default output is not implemented yet; this target is inactive`.
- 2026-10-20: More records for the logging table (point 12), from #155's review: the
  release record (at info, as the owner agreed) and the audio adapter's diagnostics for
  the new-output guard, at debug:

  | Level | Message | Attributes |
  |---|---|---|
  | info | `output released; no control holds the device` | `apptrol.device.name` |
  | debug | `output device read` (audio adapter) | `apptrol.device.index`, `apptrol.device.name`, `apptrol.device.description`, `apptrol.audio.channels`, `apptrol.volume_percent`, `apptrol.audio.guard.state`, `apptrol.audio.guard.remaining_ms`, `apptrol.wanted_percent` |
  | debug | `output volume request completed` (audio adapter) | `apptrol.device.name`, `apptrol.audio.channels`, `apptrol.wanted_percent`, `apptrol.audio.result`, `error.type` on failure |
  | debug | `output kept by a control; checking the volume last read` (audio adapter) | `apptrol.device.name`, `apptrol.volume_percent`, `apptrol.audio.guard.state` |

  The release record comes once per release, from the mixer, so the log explains why a
  volume set elsewhere now stays. The debug records show every read of an output with
  what the guard made of it and why it set nothing, and the server's answer to each
  write: a volume that reads wrong after a successful write was changed afterwards.
  A read that sees an output new, or with a new name or description, is not defended at
  once, because the match may be on the description: the mixer decides first and
  answers with `KeepOutput` when a control still holds the output (the adapter then
  checks the volume of that read without waiting for another change) or with
  `ReleaseOutput` when none does.
- 2026-10-20: The owner chose generation-tagged ownership for #155's remaining
  CTRL-06 race (option A). Pausing just the read that changed the description was
  insufficient: another notification could restore the server's volume before the
  mixer's buffered release arrived.

  ```text
  rename -> report generation 2 -> second notification -> guard skips
  old answer (1) -> ignored      -> ReleaseOutput(2)    -> volume stays
  ```

  Each reported output identity (including snapshots) carries a generation. The pure
  mixer echoes it in `KeepOutput` or `ReleaseOutput` (ADR 0015), even when it also sets
  the volume or the output was already unassigned. The adapter owns the counter and
  the pending state. It accepts only an answer for the newest reported generation;
  intervening volume-only notifications update the latest volume but leave ownership
  pending. Generations are not reused after removal, resync or reconnect. An explicit
  volume write is not an ownership answer. `KeepOutput` checks the latest volume once
  accepted; `ReleaseOutput` clears the defended volume. The adapter never waits for
  the mixer (option B was rejected), and keeps the `wire` then `mu` lock order.

  The existing debug record `output device read` uses
  `apptrol.audio.guard.state=ownership_pending` to explain why it skipped defense.
  No new attribute is needed. Deterministic unit tests force the rename, second
  notification and late release order, and check stale answers (QA-11).
- 2026-10-20: From the review of generation-tagged ownership on #155, two follow-ups.
  The note above said no new attribute was needed; an ignored answer was silent,
  though, so the owner approved one more record for the logging table (point 12),
  with two new attributes:

  | Level | Message | Attributes |
  |---|---|---|
  | debug | `output ownership answer ignored; a newer identity was reported` (audio adapter) | `apptrol.device.name`, `apptrol.action` (`KeepOutput` or `ReleaseOutput`), `apptrol.audio.ownership.answer_generation`, `apptrol.audio.ownership.generation` |

  `output kept by a control; …` logs `apptrol.volume_percent` only when the guard has
  a read; an output known at connect has none. A volume write the server accepted
  now also counts as the guard's last read, under `wire` as before. Otherwise the
  `KeepOutput` that follows the mixer's first volume for a new output compared the
  read from before that write, set the volume twice and logged a restore that never
  happened. A failed write keeps the old read, and a later restore is still read and
  defended:

  ```text
  read 100 % ─► Set 30 % (written, read = 30 %) ─► KeepOutput: at wanted ─► read 100 % ─► 30 % again
  ```
- 2026-10-21: A write the server accepted can still be lost (#166). On CI's PipeWire
  1.0.5, Apptrol set a test output to 30 % one millisecond after it appeared; the
  server answered success, but the output kept 100 % and no change was ever reported,
  so the guard, which acts on reported changes, had nothing to answer. PipeWire's
  pulse server answers once it has passed the request on to the device, not once the
  device has it. The owner chose to have the guard check its own writes (option B)
  over making only the test wait for a settled device (option A): OUT-05 promises that
  a new output gets the control's volume, on any PipeWire version.

  Within the guard's window, every volume write the server accepted is followed by a
  read of the output 200 ms later, in turn with the change notifications (under
  `wire`). The read is defended like a reported volume change: a lost write is set
  again, and that write is checked again, until the window ends. An output that is
  gone, or reads with a new name or description, is left to its own notification, so
  the mixer still decides first (CTRL-06). One record is added to the logging table
  (point 12), with no new attribute; the `output device read` that follows it shows
  what the guard made of the read:

  | Level | Message | Attributes |
  |---|---|---|
  | debug | `output read again after a volume write` (audio adapter) | `apptrol.device.name` |
  | debug | `output read again skipped; too many waiting` (audio adapter) | `apptrol.device.name` |

  The second, from the PR's review, logs a read-back dropped because 64 are already
  waiting (only when the adapter is stuck), so that an unchecked write is not silent.

  ```text
  write 30 % ─► success ─► (lost) ─► 200 ms: read 100 % ─► 30 % again ─► 200 ms: read 30 %
  ```
- 2026-10-25: The new-output guard defends the mute too (#177, OUT-05). Until now it
  defended only the volume: after a re-plug, Apptrol wrote the control's saved mute,
  saw its echo, and then WirePlumber restored the mute it remembered, which the adapter
  reported as an outside change, so the saved mute flipped. The owner chose option A:
  within the guard's 3 seconds, a mute change other than the echo of Apptrol's write is
  not reported to the mixer; the guard sets the mute it holds again, as the new-stream
  and new-input guards do (PRIO-03, MUTE-07). The protocol does not say who changed a
  mute, so a real outside mute in those 3 seconds is set back too; after them it is
  taken over. A shorter window just for mute (option B) was rejected as a guess about
  WirePlumber's timing.

  ```text
  appears ─ write muted ─ echo ─ server: unmuted ─► not reported; muted again
                                  ┤ 3 s ├─ applet: unmuted ─► OutputMuteChanged ─► taken over
  ```

  The guard holds a mute once the owning control set one (`SetOutputMute`), or while
  the mixer has not answered the newest identity; a newer `SetOutputMute` (an M press,
  ownership moving to another control) replaces it, and `ReleaseOutput` for the newest
  generation clears it with the volume, so a released output's mute changes reach the
  mixer at once. Defense waits for the ownership answer as for the volume; `KeepOutput`
  checks the last mute read. Removal drops the guard. Each accepted mute write within
  the window is read back after 200 ms like a volume write, since accepted is not
  applied (#166). Whether the guard holds a change is decided when it is read, before
  the guard writes: a window that ends during that write must not turn the restore
  it answered into an outside change (found in the PR's review). Every change it
  holds that differs from the control's mute is owed: it is hidden from the mixer, so
  the guard outlives its window until it has set the mute again or found it right; a
  held read that matches settles it. A guard kept past its window is never read back,
  so a change made after the window always goes through the outside check first. The read's own defense settles it; while
  ownership is pending (a rename and a mute change in one notification), the mixer's
  answer does: `KeepOutput` sets the mute once, `ReleaseOutput` lets it go. A mute
  change taken over after the window, or removal, cancels what is owed. Without this,
  a window ending between the decision and the defense, or before the answer, left
  the device with the restored mute. The mixer is unchanged (ADR 0015).

  ```text
  rename + unmuted (held, window open) ─ window ends ─ KeepOutput ─► muted again, guard dropped
  ```

  Records added to or changed in the logging table (point 12), with no new attribute:

  | Level | Message | Attributes |
  |---|---|---|
  | debug | `audio server changed the mute of a new output; setting it again` (audio adapter) | `apptrol.device.name`, `apptrol.muted`, `apptrol.wanted_muted` |
  | debug | `output mute change not taken over: the new-output guard holds the mute` (audio adapter) | `apptrol.device.name`, `apptrol.muted`, `apptrol.audio.guard.state` (`restore_mute`, `at_wanted_mute` or `ownership_pending`), `apptrol.audio.guard.remaining_ms` |
  | debug | `output read again after a write` (audio adapter; was `… after a volume write`) | `apptrol.device.name` |
  | debug | `output kept by a control; checking the volume and mute last read` (audio adapter; was `… the volume last read`) | adds `apptrol.muted` |
  | debug | `output device read` (audio adapter) | adds `apptrol.wanted_muted` while the guard holds a mute |

  The two renamed records were never in a release. `apptrol.audio.guard.state` gets
  the values `restore_mute` and `at_wanted_mute`.
