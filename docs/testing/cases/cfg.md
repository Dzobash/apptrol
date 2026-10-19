# Configuration test cases

### TC-CFG-001 — App settings under a layout name the right table

Covers: CFG-08, CFG-11 · Phase 1
Config: two-apps
Type: Automated
Was: H-20 (misplaced setting found during the volume test)

Test: `TestCFG08_AppSettingUnderLayoutSaysWhereItBelongs`,
`TestCFG08_UnknownLayoutKeyKeepsDiagnostic` in `internal/config/config_test.go`,
and `TestCFG11_AppSettingUnderLayoutSaysWhereItBelongs` in `cmd/apptrol/main_test.go`.

The tests use temporary configurations. All five app settings (`name`, `type`,
`match`, `max_volume`, `talk_over_volume`) under a layout must report the right table,
regardless of the value's type. Other unknown keys retain their existing diagnostics.
`apptrol check` must exit with status 1 and the same message:

```text
layouts.default.max_volume: max_volume is an app setting, not a control; put it under [apps.<id>]
```

**Result:** Pass / Fail / Blocked
**Notes:**
