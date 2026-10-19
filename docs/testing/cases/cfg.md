# Configuration test cases

### TC-CFG-001 — App settings under a layout name the right table

Covers: CFG-08, CFG-11 · Phase 1
Config: two-apps
Type: Automated

Test: `TestCFG08_AppSettingUnderLayoutSaysWhereItBelongs`,
`TestCFG08_UnknownLayoutKeyKeepsDiagnostic`,
`TestCFG08_TargetSettingInsideAButtonStaysUnknown` in `internal/config/config_test.go`,
and `TestCFG11_AppSettingUnderLayoutSaysWhereItBelongs` in `cmd/apptrol/main_test.go`.

Misplaced settings of apps and inputs name the table they belong in (`talk_over_volume`
only `[inputs.<id>]`, `type` only `[apps.<id>]`) and make `apptrol check` exit with
status 1, while other unknown keys keep their diagnostics:

```text
layouts.default.max_volume: max_volume is an app or input setting, not a control; put it under [apps.<id>] or [inputs.<id>]
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

Test: `TestCFG18_SameIDInTwoTablesIsRejected`, `TestCFG18_TalkOverVolumeOnlyInInputs`,
`TestCFG18_MediaPlayerMustBeAnApp`, `TestCFG19_OtherTypesAreRejected` in
`internal/config/targets_test.go`, and `TestCFG19_OldFormWarningIsLoggedWithItsAttributes`
in `internal/service/service_test.go` for the log record of TC-CFG-002.

```text
inputs.mic: "mic" is also defined in [apps]; an id may be used in one table only
inputs.mic.type: not used in [inputs]; remove the line
```

**Result:** Pass / Fail / Blocked
**Notes:**
