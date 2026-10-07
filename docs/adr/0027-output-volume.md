# 0027. Output volume: a control for a whole output device

- **Status:** Accepted
- **Date:** 2026-10-07

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
