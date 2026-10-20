# Configuration test cases

### TC-CFG-001 — App settings under a layout name the right table

Covers: CFG-08, CFG-11 · Phase 1
Config: two-apps
Type: Automated

Test: `TestCFG08_AppSettingUnderLayoutSaysWhereItBelongs`,
`TestCFG08_UnknownLayoutKeyKeepsDiagnostic`,
`TestCFG08_TargetSettingInsideAButtonStaysUnknown` in `internal/config/config_test.go`,
and `TestCFG11_AppSettingUnderLayoutSaysWhereItBelongs` in `cmd/apptrol/main_test.go`.

Misplaced settings of apps, inputs and outputs name the table they belong in:
`talk_over_volume` only `[inputs.<id>]`, `type` only `[apps.<id>]`, and `device` only
`[outputs.<id>]`. `apptrol check` exits with status 1; other unknown keys keep their
diagnostics:

```text
layouts.default.max_volume: max_volume is an app, input or output setting, not a control; put it under [apps.<id>], [inputs.<id>] or [outputs.<id>]
```

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-CFG-002 — An input in the old form still loads, with a warning naming its table

Covers: CFG-19, CFG-11 · Phase 1.6
Config: old-input-form
Type: Manual

**Setup**
- [ ] Your desktop's volume settings are open and show the input `apptrol-test-mic`

**Steps**
- [ ] Run `apptrol check` → it ends with `warning: apps.mic: type = "input" is the old form; move it to [inputs.mic]`; slider 8 is listed as `Test microphone`, `input: apptrol-test-mic`, and there is no error
- [ ] Run `journalctl --user -u apptrol -b | grep 'old form'` → one line `config uses an old form; move the entry to its own table` with `apptrol.app.id=mic` and `apptrol.config.table=inputs`
- [ ] Move slider 8 to the middle → the volume of `apptrol-test-mic` in the desktop's volume settings follows the slider
- [ ] Save the configuration again without changes (`touch ~/.config/apptrol/config.toml`) → `configuration reloaded`, followed by the same old-form warning again

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-CFG-003 — An input in its own table loads without a warning

Covers: CFG-18, CFG-02 · Phase 1.6
Config: full-layout
Type: Manual

**Setup**
- [ ] Your desktop's volume settings are open and show the input `apptrol-test-mic`

**Steps**
- [ ] Run `apptrol check` → slider 8 is listed as `Test microphone`, `input: apptrol-test-mic`; no warning mentions `old form`
- [ ] Move slider 8 to the middle → the volume of `apptrol-test-mic` in the desktop's volume settings follows the slider

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-CFG-004 — An id in two tables, or type in [inputs], is rejected

Covers: CFG-18, CFG-19 · Phase 1.6
Config: full-layout
Type: Automated

Test: `TestCFG18_SameIDInTwoTablesIsRejected`, `TestCFG18_SameIDInTwoTablesCarriesItsID`,
`TestCFG18_TalkOverVolumeOnlyInInputs`,
`TestCFG18_MediaPlayerMustBeAnApp`, `TestCFG19_OtherTypesAreRejected` in
`internal/config/targets_test.go`, and `TestCFG19_OldFormWarningIsLoggedWithItsAttributes`
and `TestCFG18_SameIDInTwoTablesIsLoggedWithItsID` in `internal/service/service_test.go` for
the log records of TC-CFG-002 and of the id in two tables (with `apptrol.app.id`).

```text
inputs.mic: the same id is defined in two tables ([apps] and [inputs])
inputs.mic.type: not used in [inputs]; remove the line
```

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-CFG-005 — Outputs accept exactly one selector and respect target rules

Covers: CFG-18, CFG-19, CFG-20, CFG-11 · Phase 1.6
Config: outputs-table
Type: Automated

Test: `TestCFG20_OutputsLoadWithEitherSelector`, `TestCFG20_RejectsInvalidSelectors`,
`TestCFG20_DeviceRejectedOutsideOutputs`, `TestCFG20_OutputRulesAndDiagnostics`,
`TestCFG20_OutputOverlapDoesNotClaimFirstControlWins` in `internal/config/outputs_test.go`,
`TestCFG20_CheckShowsOutputSelectors` in `cmd/apptrol/main_test.go`, and
`TestCFG20_InvalidOutputReloadKeepsConfiguration` and
`TestCFG20_DefaultOutputWarningOnStartAndReload` in `internal/service/service_test.go`.
`TestCFG20_SetupPassesFixedOutputsButNotDefault` in `internal/config/setup_test.go`
checks the narrower guard: a fixed output reaches the mixer as an output, one with
`device = "default"` stays out until #85 builds it. `TestOUT01_OutputNeverControlsAPlaybackStream`
checks that a fixed output matching `"speaker"` never controls a playback stream named
`"speaker"`, including after reload.

```text
match only / device = "default" only  -> valid, selector shown by apptrol check
both / neither / invalid device      -> config_invalid, running config kept
```

This checks configuration only; output volume is in [TC-OUT-003](out.md), mute and solo
follow with #85.

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-CFG-006 — Monitor fragments warn at load, reload and check, but stay valid

Covers: CFG-21 · Phase 1.6
Config: device-monitors
Type: Automated

Test: `TestCFG21_MonitorWarningsOnEveryLoad` in `internal/config/outputs_test.go`,
`TestCFG21_CheckWarnsAboutInputAndOutputMonitors` in `cmd/apptrol/main_test.go`, and
`TestCFG21_MonitorWarningsAreLoggedOnStartAndReload` in `internal/service/service_test.go`.

Uppercase suffixes and the old input form are included; an app's match list and a
fragment with `.monitor` in the middle get no monitor warning. The log's fixed message
is `match names a monitor, which records an output and is never matched; use the output's name without .monitor`,
with `apptrol.component=config`, `apptrol.app.id` and the fragment in `apptrol.app.match`.

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-CFG-007 — Device discovery instructions ship with the reference and example

Covers: CFG-22 · Phase 1.6
Config: outputs-table
Type: Automated

Test: `TestCFG22_DeviceDiscoveryInstructions` in `internal/config/outputs_test.go`.

Both files include `apptrol list`, `pactl list short sources`, `pactl list short sinks`,
case-insensitive matching on name or description, full names for ambiguous matches,
first-by-name selection with a warning, and the exclusion of monitors. `apptrol list`
names outputs too (OUT-12).

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-CFG-008 — Output selectors appear in the installed command

Covers: CFG-20, CFG-11 · Phase 1.6
Config: outputs-table
Type: Manual

**Setup**
- [ ] A candidate with the outputs table is installed

**Steps**
- [ ] Run `apptrol check` → no error; slider 7 shows `output: device = "default"`, knob 7 shows `output: apptrol-test-mic-sink`, both with `max 80 %`; one warning, for `outputs.main` only, says `following the default output is not implemented yet; this target is inactive` (the fixed output on knob 7 works)
- [ ] Save the configuration again (`touch ~/.config/apptrol/config.toml`) → the journal reports `configuration reloaded` without an invalid-configuration error and repeats the inactive-output warning for `apptrol.app.id=main`

This case checks parsing and reload only, without moving output controls. Output
volume is covered by [TC-OUT-003](out.md); following the default output by #85's later
cases.

**Result:** Pass / Fail / Blocked
**Notes:**
