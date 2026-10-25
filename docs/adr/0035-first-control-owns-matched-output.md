# 0035. The first control owns a matched output device

- **Status:** Accepted
- **Date:** 2026-10-21

Supersedes ADR 0027 point 4 for fixed outputs. Requirements: OUT-04, OUT-05,
OUT-06, CTRL-06. Issue: #85.

## Context

Two independent controls must not share a mute button. Display names do not decide
ownership: different match fragments can resolve to the same device, and identical
display names can describe different devices. The owner chose first-control ownership,
as for application matching.

## Decision

```text
slider1: match "test-output" ─┐
slider4: match "speakers"    ─┴─► same device: slider1 owns it; slider4 is ignored
```

Choose each fixed target's device using OUT-02, then resolve conflicts in control
order: sliders 1–8, followed by knobs 1–8. The first control owns the device's volume
and mute. A later conflicting control has no device and its M button is ignored.
It does not fall back to another matching device merely because its choice is owned.
Its physical position is still remembered; if a reload removes the earlier owner,
the newly owning control applies its own known position and saved mute.

An outside mute belongs only to the owner, including when that owner is a knob.
Re-plug applies that control's saved mute and known position. Releasing a device
removes its mute and leaves its volume. No shared OR of mute states remains.

Taking over an outside mute makes it the control's saved mute; its origin is not
retained. Releasing the device therefore unmutes it even if that mute was first set
in the desktop applet. On reconnect or snapshot resync, saved state is applied again;
an outside change made while Apptrol could not observe it may be overwritten. These
are the same restoration and release rules used for inputs, not a new distinction
between M and outside mutes. The default-output decision remains separate.

When one notification changes both description and mute, report the mute before
the identity change. The owner at the time of the read takes it over. If the device
was unowned and now matches, it ignores the old outside change and then receives
the new owner's saved state (OUT-05).

PR 2c must use the same ownership for output solo and LEDs; a conflicting control
must not solo the device. This PR does not implement either feature. Default-output
ownership and overlap remain a decision for PR 3; `device = "default"` stays inactive.

The existing adapter write serialization and own-mute attribution are reused. No new
guard or locking protocol is introduced. Extending the new-output guard to mute is
a separate follow-up: a late server mute restore can currently be taken as an outside
change after the first re-plug write. Full OUT-05 restoration under that race remains
pending that follow-up.

## Logging

Existing attributes in `internal/logattr` suffice; no new attribute or error type.

| Level | Message | Attributes and reason |
|---|---|---|
| Warn | `output match ignored: an earlier control owns the device` | Target/control of loser, device name, `apptrol.device.controls` with winner then loser; once at conflict start, and at reload |
| Debug | `button ignored: output belongs to an earlier control` | Target/control, button, device; conflicting M cannot change saved mute |
| Debug | `position saved without changing volume: output belongs to an earlier control` | Target/control, device; position remembered but device untouched |
| Info | `muted` / `unmuted` | Target/control and M button; deliberate toggle |
| Info | `muted outside Apptrol` / `unmuted outside Apptrol` | Owning target/control, device; outside state taken over |
| Info | `output released; no control holds the device` | Device name; mute released, volume left alone |
| Debug | `output mute request completed` | Device name, mute, result; server accepted or rejected the write; failures include `error.type=audio_change_failed` |
| Debug | `output mute echo ignored: matches Apptrol's write` | Device name, mute; own write is not an outside decision |

Config validation warns when match fragments visibly overlap. Runtime matching also
catches overlaps between unrelated fragments of the same device name or description.

## Consequences

Each device has one controlling mute. Later controls may be inactive, with a warning
that explains why. Changing labels cannot change ownership. Tests cover distinct
match strings resolving to one device, identical labels resolving to distinct devices,
ownership transfer, outside mute, saved mute, and re-plug.

## Notes

- 2026-10-23: Outputs have no solo ([ADR 0036](0036-no-solo-on-outputs.md)), so PR 2c
  uses this ownership only for M's LED: a control whose device an earlier control owns
  shows no mute.
- 2026-10-25: The follow-up named above is done (#177): the new-output guard defends
  the owning control's mute as well, so a late server mute restore is set again
  instead of being taken over. Ownership decides what it defends: `SetOutputMute`
  from the owner replaces the defended mute, `ReleaseOutput` for the newest identity
  clears it. Details and logging are in ADR 0027's note of the same date.
- 2026-10-25: The default-output overlap left open above is decided in
  [ADR 0038](0038-default-output-follows-the-server.md): the same rule, the first
  control owns the device, also when one of the two controls follows the default. That
  overlap is allowed, so it is logged at info instead of this ADR's warning.
