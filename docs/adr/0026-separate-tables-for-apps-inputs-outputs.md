# 0026. Separate configuration tables for apps, inputs and outputs

- **Status:** Accepted
- **Date:** 2026-10-07

## Context

ADR 0004 defines every target once under `[apps.<id>]`, and an input differs from an app
only by `type = "input"`. Output devices are coming as a third kind of target
([ADR 0027](0027-output-volume.md)). In one table, `[apps.speakers]` would describe
something that is not an app, and the reader has to find the `type` line to know what an
entry is.

The configuration format should be final before `apptrol setup` (Phase 3) writes it, so
a change of shape is cheapest now. Version 0.2.0 is public, though, and people already
have `[apps.mic]` with `type = "input"` in their files; a package upgrade restarts the
service with that file.

The options were:

- **One table per kind** (chosen): `[apps.<id>]`, `[inputs.<id>]`, `[outputs.<id>]`.
- **Keep `[apps.<id>]` with `type`** and add `type = "output"`: no change for anyone,
  but it stays hard to read.

For files in the old form:

- **Still read them, with a warning, until 1.0** (chosen): the controller keeps working
  after an upgrade, and the warning says exactly what to change.
- **Reject them with a clear error**: one form in the code, but after an upgrade the
  controller does nothing until the user edits the file (the saved state is kept,
  STATE-08).

## Decision

1. **Three tables.** Apps are defined in `[apps.<id>]`, inputs in `[inputs.<id>]`,
   outputs in `[outputs.<id>]`. The key `type` is no longer needed.

   ```toml
   [apps.spotify]
   name  = "Spotify"
   match = ["spotify"]

   [inputs.mic]
   name  = "Microphone"
   match = ["Headset"]

   [outputs.main]
   name   = "Main volume"
   device = "default"

   [layouts.default]
   slider1 = "spotify"
   slider7 = "mic"
   slider8 = "main"
   ```

2. **Ids stay unique over all three tables.** Layouts and `[media] player` refer to
   targets by id alone, as before, so `[apps.mic]` and `[inputs.mic]` together are
   rejected, naming both tables. `[media] player` must name an entry of `[apps]`.
3. **The old form is still read until 1.0.** `[apps.<id>]` with `type = "input"` is read
   as `[inputs.<id>]`; `type = "app"` is read as if it were not there. Each such entry
   gets one warning at every configuration load and in `apptrol check`, naming the
   table to move it to. Any other `type` value in `[apps]`, and `type` in `[inputs]` or
   `[outputs]`, is rejected. Version 1.0 removes the old form; its release notes say
   so.
4. **Settings stay where they fit.** `talk_over_volume` is only accepted in `[inputs]`;
   `max_volume` in all three tables (ADR 0027 for outputs).
5. **Device names are explained where people look for them.** The example
   configuration and `docs/config.md` explain how to find the names of inputs and
   outputs (`apptrol list`, or `pactl list short sources` and `pactl list short sinks`
   without Apptrol), and how `match` works: a case-insensitive part of a device's name
   or description; the full name when a short part fits several devices; when several
   devices match, the first is used, with a warning.
6. **Monitors are pointed out.** A monitor (`<output>.monitor`) records what an output
   plays; Apptrol never matches it as an input or an output. A match fragment of an
   input or output that ends in `.monitor` therefore matches nothing, so `apptrol check`
   and every configuration load warn about it. The configuration stays valid.

### Logging

7. Following ADR 0016:

   | Level | Message | Attributes |
   |---|---|---|
   | warn | `config uses an old form; move the entry to its own table` | `apptrol.app.id`, `apptrol.config.table` (e.g. `inputs`) |
   | warn | `match names a monitor, which records an output and is never matched; use the output's name without .monitor` | `apptrol.app.id`, `apptrol.app.match` |
   | error | (validation) `the same id is defined in two tables` | `apptrol.app.id`, `error.type` = `config_invalid` |

   New attribute: `apptrol.config.table`. `apptrol.app.type` gains the value `output`.

## Consequences

- ADR 0004's point "Apps and inputs are defined once under `[apps.<id>]`" is replaced
  by this record; layouts and the rest of ADR 0004 stay.
- CFG-02, CFG-05, CFG-08, CFG-14, CFG-15 and CFG-16 change their wording when this is
  built: "app" becomes "app, input or output" where it means any target.
- The configuration reader has two ways to read inputs until 1.0, each with its tests.
- The saved state is not affected: it is kept per layout and control, not per id.
- Built in 0.3.0, before output volume (ADR 0027), so outputs only ever exist in the new
  form.
