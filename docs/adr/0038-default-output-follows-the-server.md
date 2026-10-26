# 0038. The default output follows the server; the first control owns an overlap

- **Status:** Accepted
- **Date:** 2026-10-25

Supersedes [ADR 0027](0027-output-volume.md) point 4 for the default output, as
[ADR 0035](0035-first-control-owns-matched-output.md) did for fixed outputs.
Requirements: OUT-03, OUT-04, OUT-13. Issue: #85.

## Context

ADR 0027 decided that `device = "default"` follows the audio server's default output
(point 2), that the new default keeps its own volume until the control is moved
(point 3, a hearing-safety rule), and that the same device on two controls is allowed
(point 4). ADR 0035 then made the first control own a device that two fixed outputs
choose, and left the default output's overlap open. Three questions remained:

- Who owns the device when the default output is also a fixed output's device, and
  what happens when the default moves onto it or away from it?
- Does the default output ever get the control's saved position without a move?
- What happens to a slider move, a saved mute and the new-output guard around a change?

## Decision

1. **The audio server tells, the adapter reports.** The audio adapter subscribes to the
   server's own changes, reads the default output at every (re)connect into the snapshot
   (`AudioSnapshot.DefaultOutput`), and reports each change as `DefaultOutputChanged`.
   The same notice comes for other server changes (a new default input); only a change
   of the default output is reported. The server may name a default it has not reported
   as a device yet; the control then does nothing until the device is there. This works
   on any desktop and distribution with PipeWire or PulseAudio (ADR 0003).
2. **One ownership rule for every output: the first control owns the device, sliders
   1–8 then knobs 1–8** (ADR 0035), whether it chose the device by `match` or by
   `device = "default"`. The owner chose this on 2026-10-25; ADR 0035's rule and
   OUT-04 already preferred it. "A fixed output always wins" (a second rule beside
   control order), "the default always wins" (the fixed control is dead exactly when
   you listen on it) and a shared device (ADR 0035 dropped it) were rejected.

   ```toml
   [outputs.main]
   device = "default"           # slider1

   [outputs.headphones]
   match = ["Headphones"]       # knob1
   ```

   ```text
   speakers default ────────┤ headphones default ├────────┤ speakers default
   slider1 → speakers         slider1 → headphones          slider1 → speakers
   knob1   → headphones       knob1 does nothing            knob1 → headphones (its position, saved mute)
   ```

   The other way round (headphones on slider1, the default on knob1), slider1 keeps the
   headphones, and knob1 does nothing while they are the default; a mute key then
   mutes them and lights slider1's M. To make the main volume always win, put it on an
   earlier control. The control that yields acts as one in a fixed conflict: M does not
   change its saved mute, a move saves its position only, and M's LED is off (ADR 0035,
   ADR 0036). Unlike a fixed conflict, this overlap is allowed (ADR 0027 point 4), so it
   is logged at info, when it begins and at every reload.
3. **The default output's volume is set only by a move**: not at connect, reconnect or
   reload, not when the default changes and not when ownership moves to it. The saved
   position may belong to another device (ADR 0027 point 3); after a restart the default
   keeps the volume it has until the control is touched. The owner chose this over
   setting it at startup when it is the same device as last time, which would need the
   device saved in the state file.
4. **Mute and position stay with the control.** Both are saved per control as before
   (STATE-01); the state file does not change. The control's mute applies to each new
   default at once (OUT-03); the previous default is released: unmuted if Apptrol muted
   it, its volume left as it is, and its new-output guard cleared (`ReleaseOutput`). A
   fixed control that gets its device back applies its own position and saved mute, as
   any ownership transfer does (ADR 0035). A snapshot (a resync or a reconnect) can
   bring a new default without `DefaultOutputChanged`; the mixer remembers what the
   controls held before it, so the previous default is released in the same way and
   does not keep the control's mute (found in #189's review).
5. **No stale writes on the old device.** The mixer handles one event at a time: a move
   before the change goes to the old default, every move after it to the new one. After
   the release, nothing in the mixer names the old device, and the guard's queued
   read-back finds no guard and writes nothing (ADR 0027 note of 2026-10-21).
6. **A new device that becomes the default at once** (plugging in headphones) needs the
   new-output guard to defend the control's mute on it although the device first
   appeared with no owner. Within the same 3 seconds, the guard can also still defend
   a volume an earlier owner set: a fixed output sets a new device to 80 %, the default
   moves onto it and an earlier default control takes it over without a volume, and a
   restore to 30 % is set back to 80 %. Both are guard work: a separate pull request,
   after this one (#85).

### Logging

Following ADR 0016; records about a control name the layout, control and target
(LOG-14).

| Level | Message | Attributes |
|---|---|---|
| info | `default output changed` | `apptrol.device.name` (empty: none), `apptrol.device.previous_name` |
| info | `output matched` | as for fixed outputs, with `apptrol.device.selected_by=default` |
| info | `default output keeps its volume until the control is moved` | `apptrol.device.name` |
| info | `no default output; the control does nothing until there is one` | none beyond the control; at connect and reload, and when the default goes |
| info | `default output is the same device as another output; the earlier control owns it` | `apptrol.device.name`, `apptrol.device.controls` (owner, then the control that yields); about the control that yields |
| info | `output released; no control holds the device` | `apptrol.device.name` (ADR 0027 note of 2026-10-20) |

New attribute: `apptrol.device.previous_name`. `apptrol.device.controls` now also
names a default-output overlap. The fixed-output conflict keeps its warning
`output match ignored: an earlier control owns the device` (ADR 0035). The
inactive-output warning `following the default output is not implemented yet; this
target is inactive` (ADR 0027 note of 2026-10-20) is gone.

```text
INFO default output changed apptrol.device.name=<headphones> apptrol.device.previous_name=<speakers>
INFO output released; no control holds the device apptrol.device.name=<speakers>
INFO output matched apptrol.control=slider1 apptrol.device.name=<headphones> apptrol.device.selected_by=default
INFO default output keeps its volume until the control is moved apptrol.control=slider1 apptrol.device.name=<headphones>
INFO default output is the same device as another output; the earlier control owns it apptrol.control=knob1 apptrol.device.name=<headphones> apptrol.device.controls="slider1, knob1"
```

## Consequences

- One rule decides every output overlap; the place of a control decides who wins.
- A main-volume control on a later control does nothing while the default is a device
  an earlier control holds; the log says why.
- After a switch, the first move of the control puts its position on the new device:
  a slider left high for the speakers sets the headphones high when it is touched.
  A control that takes over only once it passes the device's volume could avoid that;
  it is not part of this decision.
- `apptrol list` shows the owning control on the `*` device.
- One volume is applied to all channels, as for fixed outputs (#156).
- `make test-audio` switches the real default output between two test outputs and
  restores the one it found (the owner chose this, 2026-10-19).

## Notes

- 2026-10-26: Point 6 is done (#85, PR 3b). The owner chose options 1A and 2A. Both
  cases are guard work in the audio adapter; the mixer gains one action and stays
  pure (ADR 0015).

  **A new device that becomes the default at once.** `SetOutputMute` already gave a
  released guard a mute to defend again, and the other order (the server names the
  default before the device is reported) was already right: the mixer holds the
  device from its first read. One gap was left. The device first appears with no
  owner, the mixer releases it, and a restore read before the mixer hears of the new
  default was outside by the guard's rules; it reached the mixer after the default
  control took the device over, and that control's saved mute flipped (MUTE-07).

  ```text
  adapter: appears ─ OutputAdded ─ DefaultOutputChanged ─ restore: muted (read) ───────┐
  mixer:            ReleaseOutput ┤            ├ SetOutputMute(slider1's)               │
  before:  restore reported ─► reaches the mixer after slider1 owns it ─► saved mute flips
  now:     restore held (released) ─► not reported; slider1's mute is written and defended
  ```

  1A: within its window, the guard also holds the mute changes of a released device
  from the mixer, and defends nothing on it (`released` in `apptrol.audio.guard.state`);
  after the window they are reported as before. Nothing visible is lost: the mixer
  ignores mute changes on a device no control holds, and a control that takes it over
  writes its own mute. 1B, tagging the change as read while released for the mixer to
  drop, was rejected: guard knowledge in the mixer. ADR 0027's note of this date
  records the protocol change.

  **A stale defended volume after an ownership move.** A fixed control sets a new
  device to 80 %; the default moves onto it within the guard's window and an earlier
  default control takes it over, without a volume (point 3). The guard still defended
  80 %, so a restore to 30 % was set back. 2A: when a default control takes a device
  another control held, the mixer sends `ForgetOutputVolume`, and the guard stops
  defending the volume and keeps defending the mute. It carries no generation: it only
  narrows defense, never resumes it, and actions are applied in order, so the next
  `SetOutputVolume` (a move) is defended again. A field on `KeepOutput` (2B) was
  rejected: an answer to a read is not an ownership move, and its generation check
  could drop it as stale and leave 80 % defended.

  ```text
  knob1: 80 % ─ default: slider1 owns ─ ForgetOutputVolume ─ server: 30 % ─► stays
                                                             server: muted ─► slider1's mute again
  ```

  Logging, following ADR 0016 (no new attribute):

  | Level | Message | Attributes |
  |---|---|---|
  | debug | `output mute change not taken over: the new-output guard holds the mute` (audio adapter) | as before; `apptrol.audio.guard.state` gets the value `released` |
  | debug | `output volume no longer defended; the new owner sets none until it is moved` (audio adapter) | `apptrol.device.name` |

  ```text
  INFO default output changed apptrol.device.name=<headphones> apptrol.device.previous_name=<speakers>
  INFO output matched apptrol.control=slider1 apptrol.device.name=<headphones> apptrol.device.selected_by=default
  DEBUG output mute change not taken over: the new-output guard holds the mute apptrol.device.name=<headphones> apptrol.muted=true apptrol.audio.guard.state=released
  DEBUG output volume no longer defended; the new owner sets none until it is moved apptrol.device.name=<headphones>
  ```
