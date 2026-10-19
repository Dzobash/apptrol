# Configuration test cases

### TC-CFG-001 — App settings under a layout name the right table

Covers: CFG-08, CFG-11 · Phase 1
Config: two-apps
Type: Automated

Test: `TestCFG08_AppSettingUnderLayoutSaysWhereItBelongs`,
`TestCFG08_UnknownLayoutKeyKeepsDiagnostic`,
`TestCFG08_TargetSettingInsideAButtonStaysUnknown` in `internal/config/config_test.go`,
and `TestCFG11_AppSettingUnderLayoutSaysWhereItBelongs` in `cmd/apptrol/main_test.go`.

Misplaced app settings name the right table and make `apptrol check` exit with status 1,
while other unknown keys keep their diagnostics:

```text
layouts.default.max_volume: max_volume is an app setting, not a control; put it under [apps.<id>]
```

**Result:** Pass / Fail / Blocked
**Notes:**
