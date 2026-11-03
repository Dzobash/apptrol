# Launcher test cases

Launcher buttons: starting apps from the controller, and the `apptrol list` words that
help write the configuration for them ([ADR 0019](../../adr/0019-launcher-and-column-buttons.md)).

### TC-LAUNCH-001 — The sections of `apptrol list` can be asked for alone

Covers: LAUNCH-16 · Phase 1.5
Config: minimal
Type: Manual

**Setup**
- [ ] The `minimal` fixture is applied and Apptrol runs (test tone A plays on slider 1)

**Steps**
- [ ] Run `apptrol list outputs` → only the output devices section: its heading is
  `Output devices (* = the system's default)…`, the NAME column lists
  `apptrol-test-mic-sink`, and exactly one line starts with `*`; no `Audio server`
  line and no other section
- [ ] Run `apptrol list inputs` → only the input devices section: its NAME column
  lists `apptrol-test-mic`; no other section
- [ ] Run `apptrol list playing` → only the playing apps section: test tone A
  (`apptrol-test-a`) with slider 1's control, and a heading that also points to
  `apptrol list apps`; no other section
- [ ] Run `apptrol list` → all three sections with their headings and the
  `Audio server:` line; the playing heading is the shorter one without the pointer
- [ ] Run `apptrol list output` (a typo) → an error that names the valid words:
  `(apps, outputs, inputs or playing)`

**Result:** Pass / Fail / Blocked
**Notes:**
