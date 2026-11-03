# Input test cases

Following the server's default microphone (INPUT-09,
[ADR 0038](../../adr/0038-default-output-follows-the-server.md)). Use silent virtual
microphones so changing a default never makes a real microphone live unexpectedly.
Microphone buttons are also covered by the [hardware checklist](../../testing.md#manual-hardware-checklist).

### TC-INPUT-001 — Default microphone, saved mute, cough and ownership

Covers: INPUT-09, INPUT-02, INPUT-07, CFG-20, MUTE-07 · Phase 1.6 follow-up (#221)
Config: input-default
Type: Manual + Automated

```text
default B: slider8 → B, knob8 → A
default A: slider8 → A, knob8 inactive
default B: slider8 → B, knob8 → A at its saved position
```

**Setup**
- [ ] Before applying the fixture, run `pactl get-default-source` → note
  `<original default>` privately for restoration
- [ ] Run `pactl load-module module-null-sink sink_name=apptrol-test-input-home-sink`
  → note `<home sink module>`
- [ ] Run `pactl load-module module-remap-source master=apptrol-test-input-home-sink.monitor source_name=apptrol-test-input-home`
  → note `<home source module>`; this is microphone B
- [ ] Run `pactl set-default-source apptrol-test-input-home` → B is the default
- [ ] Apply `input-default` → A (`apptrol-test-mic`) is created; the journal has
  `input matched` for slider8 with `apptrol.device.selected_by=default` on B

**Steps**
- [ ] Run `apptrol check` → slider8 shows `input: device = "default"`
- [ ] Move slider8 to the middle → B's volume is about 40 % in the desktop's input settings
- [ ] Move knob8 to the top → A's volume is 50 %
- [ ] Press M8 → B muted, M8 dark
- [ ] Run `pactl set-default-source apptrol-test-mic` → A muted at 50 %, B unmuted
  at about 40 %; journal has `default input changed`, `input matched` with
  `selected_by=default`, and the overlap line naming slider8 before knob8
- [ ] Move knob8 to the middle → A stays at 50 %: the later control is inactive
- [ ] Press M8 → A live, M8 lit
- [ ] Hold S8 → A muted, M8 dark
- [ ] While holding S8, run `pactl set-default-source apptrol-test-input-home`
  → the cough ends (`held state ended`, `apptrol.held.reason=input_changed`), B live,
  A live at knob8's saved position (about 25 %)
- [ ] Release S8 → B stays live
- [ ] Hold S8 again → B muted; release → B live
- [ ] Restore the fixture with `docs/testing/apply-fixture.sh --restore` → own config back
- [ ] Run `pactl set-default-source <original default>` → original default restored
- [ ] Unload `<home source module>`, then `<home sink module>` with `pactl unload-module`
  → no `apptrol-test-input-home` remains

Automated: `TestCFG20_InputsAcceptEitherSelector`, `TestCFG20_InputsRejectInvalidSelectors`,
`TestCFG20_CheckShowsDefaultInput`, `TestINPUT09_DefaultFollowsSwitchAndSnapshot`,
`TestINPUT09_NoDefaultAndDeviceArrivesLater`, `TestINPUT09_FirstControlOwnsOverlap`,
`TestINPUT09_GuardDefendsRestoresAndReleases`,
`TestINPUT09_GuardWaitsForNewestIdentityAndSettlesMuteDebt`,
`TestINPUT09_RestoreReadBeforeDefaultTakeoverDoesNotChangeSavedMute`,
`TestINPUT09_DefaultInputFollowedThroughTheService`, and
`TestIntegration_INPUT09_DefaultInputFollowsASwitch`.

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-INPUT-002 — A held M cannot open the new default microphone

Covers: INPUT-09, INPUT-03, INPUT-04, INPUT-07, LED-04 · Phase 1.6 follow-up (#221)
Config: input-default-hold
Type: Manual + Automated

```text
M held: B live, music down ─ default A ─► A muted, B released, music restored
M release + fresh press ─────────────────► A live, music down
```

**Setup**
- [ ] Repeat TC-INPUT-001's setup with `input-default-hold` instead → B default,
  A available, both silent; M8 dark
- [ ] Move slider1 to the middle → tone A plays at that position

**Steps**
- [ ] Hold M8 → B live, M8 lit, tone A reduced
- [ ] While holding M8, run `pactl set-default-source apptrol-test-mic` → A muted
  immediately, B unmuted, M8 dark, tone A restored; journal says `held state ended`
  with `apptrol.held.reason=input_changed`
- [ ] Release M8 → A stays muted
- [ ] Hold M8 again → A live, M8 lit, tone A reduced
- [ ] Release M8 → A muted, M8 dark, tone A restored
- [ ] In the fixture config set `s8 = { mode = "talk_over" }` → valid reload
- [ ] Hold S8 → tone A reduced, A stays muted
- [ ] While holding S8, switch the default to B → B muted, tone A restored
- [ ] Release and hold S8 again → tone A reduced, B still muted
- [ ] Release S8 → tone A restored
- [ ] Follow TC-INPUT-001's cleanup → own configuration and original default restored,
  extra modules removed

Automated: `TestINPUT09_SwitchEndsHeldStatesAndAllowsFreshPress` and
`TestIntegration_INPUT09_DefaultInputFollowsASwitch`. Real late restores are covered
by the deterministic adapter tests listed in TC-INPUT-001; a reported restore is
imitated by `TestIntegration_INPUT09_NewDefaultDefendsReportedRestores`.

**Result:** Pass / Fail / Blocked
**Notes:**
