# 0039. Output volume keeps each channel's balance

- **Status:** Accepted
- **Date:** 2026-10-26

Supersedes the flat-channel decision in [ADR 0027](0027-output-volume.md)'s note of
2026-10-20 and the flat-channel consequence in
[ADR 0038](0038-default-output-follows-the-server.md). Requirements: OUT-14 to
OUT-16, OUT-01, OUT-05, OUT-12, OUT-13, CTRL-02. Issue: #156.

## Context

An output slider currently puts the same volume on every channel. It loses a
balance chosen in the desktop's sound settings on the first move. The owner wants
output balance preserved in Phase 1.6, before v0.3.0-rc1. Inputs and applications
keep their current behaviour; the owner does not need their balance preserved.
Read-back for apps and inputs (#174) and soft takeover (#190) remain separate.

## Decision

### The loudest channel follows the control

```text
before:             left 80%    right 60%
control sets 50%:    left 50%    right 37.5%
centred beforehand: left 50%    right 50%
```

Every fixed or default output preserves its channel proportions, always, without
a setting. The control still maps 0–127 linearly to 0–`max_volume` (CTRL-02).
That percentage is the loudest channel's percentage. Every other channel gets
the same scaling factor; none exceeds the requested volume, including with boost.
The output percentage in `apptrol list` also becomes the maximum, not the average.

This is the PulseAudio protocol's UI volume scale, which the adapter already uses
through `proto.NormVolume`, not a linear amplitude scale. For nonzero channel
values `c`, the new values are `requested × c / max(c)`, rounded to protocol units.
On the server's cubic software-volume curve this preserves relative channel gains
too. It does not promise that 50% sounds half as loud. Hardware may quantize the
requested values. See [PulseAudio's volume documentation](https://www.freedesktop.org/software/pulseaudio/doxygen/volume.html).

The alternative, the average following the control, would give about 57.14/42.86
for 80/60 at 50%. That is the example originally in #156, but contradicts its
proposal that the loudest channel follows the slider and could put a channel above
`max_volume`. The owner chose maximum. KDE's main volume uses the maximum too;
other applets may summarize channels differently. Per-channel readings are the
test oracle, not an applet's single percentage.

### Balance belongs to the device and stays in the adapter

```text
connect / device appears → server's channels and positions → adapter remembers ratios
outside change          → update ratios                  → next move uses them
control changes device  → use new device's ratios         → no balance carried over
```

The adapter reads channel volumes and their positions from the connection
snapshot and subscription reads. It keeps the latest usable ratios per live
device, separately from the last levels read and the volume the guard defends.
A write without cached channel information first reads it; a failed read does not
guess a stereo layout or flatten the device. Normal moves use the cache rather
than adding a server round trip to every move. Concurrent outside changes and
moves follow the adapter's serialized processing order; this is not an atomic
read-modify-write transaction with other clients.

Ratios survive Apptrol's own zero writes and must not gradually drift through
rounding on successive moves or their echoes. An accepted write is not evidence
that the server applied it; the existing read-back still checks that (OUT-16).
Removal, reconnect and a full resync after event overflow discard remembered
balance and obtain it from the server again. A channel-layout change invalidates
ratios for the old positions; the
adapter uses the newly read layout and its balance, never old array positions.
Releasing ownership stops defense, but does not make a live device's balance
belong to another control. Notifications continue to update it.

Saving balance across restarts was rejected: it needs persistent device identity
and stale-state rules, and could override a balance changed while Apptrol was off.
The pure mixer still chooses the owner and emits a scalar `SetOutputVolume`.
Channel conversion, server reads and the existing guard stay in the audio adapter
(ADR 0015); the service interface and state-file format do not change.

### Zero, mute and channel counts

```text
80/60 → 0/0 → 50/37.5             last nonzero ratio remembered while connected
start at 0/0 → 50/50              no ratio available: equal channels
80/0 → 50/0                      a silent channel stays silent
mono 80 → 50                     one channel follows the control
80/60/40/40/20/0 → 50/37.5/25/25/12.5/0   six channels keep their proportions
```

All-zero levels reveal no ratio, so keep the last usable balance for that live
device and layout. If none is known, use equal channels when a nonzero volume is
requested. A balance adjustment that leaves every server channel zero cannot be
observed. Restarting, reconnecting or a full overflow resync at zero therefore
loses the former balance.
Mute is independent: neither M nor an outside mute changes channel ratios.
Mono and surround use the same maximum-channel rule, without a stereo special case.

```text
80/60 → 0/0 → event overflow → full resync at 0/0 → control 50 → 50/50
             full resync at 80/60               → control 50 → 50/37.5
```

The owner chose this simple resync rule after PR #192's second review on
2026-10-26. It follows the adapter's existing full reset. Keeping cached ratios
when name, server index and channel positions match was rejected: missed removal
and reappearance can make cached identity uncertain. Nonzero snapshot values
supply fresh ratios; zero snapshots fall back to equal channels. This does not
change ownership generations, snapshot volume policy or start a new guard window.

### The guard defends the level and accepts the server's balance

```text
appears 100/100 → write 50/50 → server restores 80/60 → guard writes 50/37.5
write 50/37.5 → lost, no notification → 200 ms read-back → same defense
within 3 s: applet sets 50/25 → balance accepted, no level correction needed
after 3 s: applet sets 40/80 → stays → control sets 50 → 25/50
```

Once a volume has been requested, the new-output guard defends its scalar level
throughout the existing three-second window, while accepting usable balance from
server reads. Notifications, the 200 ms read-back and `KeepOutput`'s check of the
latest read held while ownership was pending use the same balance and level rules: distinguish
own rounded write echoes from new server values, update usable balance, then compare
the maximum channel percentage with the requested level using the existing 0.5
percentage-point tolerance. A correction scales the latest balance to that level.
A balance-only change at the requested maximum needs no correction; it still updates
the ratios for the next move. All-zero reads keep the last usable ratios (OUT-15).
A new move replaces the requested level; the latest move still wins.

The owner chose this on 2026-10-26 after PR #192's first review. The earlier draft
defended the full vector, rejecting restored balance as well. A new output can
first read equal channels before the server restores its remembered balance;
defending the first ratio would erase the very balance this feature should keep.
This is a design correction before implementation, not a change to shipped behaviour.

The protocol cannot distinguish a deliberate applet change from a server restore,
so both may change the balance during the window. Their overall level is corrected
while the guard owns a requested volume. A partially applied or lost write can
likewise supply a new balance; preserving the server's latest usable ratio takes
priority over enforcing an older vector. After the window, the entire outside
change is left alone until the next move (PRIO-02). Mute defense is unchanged.

The existing ownership protocol remains in force: a pending generation cannot
trigger defense; only the newest Keep/Release answer counts; release clears the
requested level. `ForgetOutputVolume` also clears it without clearing mute defense
(ADR 0038). A default takeover still sets no volume until a move. If the channel
layout changes, discard the obsolete vector and use the new layout's balance for
any volume that is still authorized to be defended. No old channel positions may
be written to the new layout. Keep the `wire` then `mu` lock order.

```text
rename read 80/60 → ownership pending: no correction → KeepOutput → 50/37.5
```

`KeepOutput` uses the latest held channel read and the latest requested level,
not a cached channel average or a flat-channel write. Stale answers do nothing;
if the guard window has ended, a keep answer does not restart volume defense.

### Logging

All records below are in the audio adapter at debug, with
`apptrol.component=audio` and the existing device identity attributes (ADR 0016).
Control decisions retain their layout, control and target attributes (LOG-14).
The implementation adds the new attributes to `internal/logattr` and
`docs/log-attributes.md` together. These are planned records, not yet emitted.

| Level | Message | Attributes and reason |
|---|---|---|
| debug | `output balance selected` | `apptrol.audio.channel_positions`, `apptrol.audio.balance.ratios`, `apptrol.audio.balance.source` (`server`, `remembered`, `equal`), `apptrol.audio.balance.reason` (`snapshot`, `resync`, `new_device`, `outside_change`, `guard_read`, `zero`, `layout_changed`, `missing_cache`); explains selection or fallback; `resync` with source `equal` explains zero-history loss; `guard_read` does not claim who made the change |
| debug | `output balance retained` | Same positions, ratios and source; reason `own_write` or `zero`; explains why a read did not replace the balance |
| debug | `output device read` | Existing attributes plus `apptrol.audio.channel_positions` and `apptrol.audio.channel_volumes_percent`; `apptrol.volume_percent` is the maximum; existing guard state explains skipped defense |
| debug | `output volume request completed` | Existing result, requested percentage and channel count, plus positions, ratios, source and `apptrol.audio.wanted_channel_volumes_percent` for the requested vector; failure includes `error.type=audio_change_failed` |
| debug | `audio server changed the volume of a new output; setting it again` | Existing attributes plus read channel percentages and `apptrol.audio.wanted_channel_volumes_percent`; shows the corrected level with the accepted balance |

Use a fixed message per record. Ratios and levels are ordered by the accompanying
channel positions. Examples, fixtures, tests and published evidence use synthetic
device names and levels, never the owner's system values.

## Consequences

- Centred outputs behave as before; inputs and application streams are unchanged.
- Balance at zero is remembered only for the current live device and layout,
  until removal, reconnect or a full overflow resync discards it.
- A server-restored or deliberate applet balance adjustment during active
  new-output volume defense is kept; only its overall level is corrected. An own
  rounded echo must not gradually alter the retained ratio.
- Implementation and guard support ship together: a separate flat guard would
  undo the feature. Requirements, this ADR and the planned test case land first.
- Tests cover mono, stereo, surround, silent channels, zero, boost, rounding drift,
  snapshots, reconnect, overflow resync at zero and nonzero levels, layout changes,
  outside edits, write failures and loss,
  ownership generations, release, default takeover and the latest move winning.
  Real-server tests use silent outputs with active silent streams, as existing
  audio integration tests do. Controller movement and listening remain manual.
- #156 belongs to 0.3.0 before rc1. The current known issue is removed only when
  implementation lands; #174 and #190 remain open and out of scope.

## Implementation note — 2026-10-28

The output adapter now implements this decision for 0.3.0. The logging table
above describes emitted records; its original "planned records" wording records
the design-only stage. Channel-position attributes use PulseAudio position codes.
Unit tests cover conversion, cache lifecycle and ownership/guard ordering, and
real-server tests use active silent stereo and six-channel outputs. TC-OUT-010's
controller and listening checks remain part of the owner's rc1 run.

## Rounding clarification — 2026-10-28

After local review, the owner chose to retain the usable ratios when every read
channel is within one protocol unit of those ratios scaled to the read's maximum.
This covers an earlier rounded write read after a newer accepted write, small
server rounding differences, and outside changes that only scale the volume.
The comparison uses the observed level even above the control's write cap.
Actual writes still obey the control's cap. Zero retains the existing balance;
without usable ratios, the first nonzero server read supplies them.

The cost is explicit: a deliberate outside balance adjustment inside this
one-unit tolerance does not replace the retained ratios. Near silence, one unit
can represent a large proportion of the volume. Larger changes are accepted as
before. Tracking a history of exact writes was rejected: it adds storage and
expiry rules and does not cover server rounding.

The `output balance retained` record adds reason `consistent`, with source
`remembered`. It does not claim who changed the volume. Exact last-write matches
keep reason `own_write`; zero keeps reason `zero`. This one-unit balance tolerance
is separate from the guard's unchanged 0.5 percentage-point level tolerance.
