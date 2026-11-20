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

### TC-LAUNCH-002 — A desktop file that cannot be read counts as not installed

Covers: LAUNCH-02 · Phase 1.5
Config: minimal
Type: Manual

**Setup**
- [ ] The `minimal` fixture is applied and Apptrol runs (test tone A plays on slider 1)
- [ ] The configuration has `record = { app = "ghost" }` under `[layouts.default.buttons]`
  (added to the fixture's config.toml and saved, so Apptrol reloads it)
- [ ] Create a dangling symlink in the user applications folder:
  `mkdir -p "${XDG_DATA_HOME:-$HOME/.local/share}/applications" && ln -s "${XDG_DATA_HOME:-$HOME/.local/share}/applications/gone.desktop" "${XDG_DATA_HOME:-$HOME/.local/share}/applications/ghost.desktop"`

**Steps**
- [ ] Run `apptrol check` → a warning that `layouts.default.buttons.record: desktop ID
  "ghost" is not installed …`, and the rest of the configuration is valid
- [ ] Run `apptrol list apps ghost` → no app is listed
- [ ] Run `systemctl --user restart apptrol` → Apptrol is active again
- [ ] Read the journal since that restart (`journalctl --user -u apptrol --since <time of
  the restart>`) → one record with the message `desktop file could not be read; the app
  counts as not installed`, a `file.path` ending in `ghost.desktop` and
  `error.type=desktop_file_unreadable`
- [ ] Create a valid `ghost.desktop` in a second data folder:
  `mkdir -p /tmp/apptrol-test-share/applications && printf '[Desktop Entry]\nType=Application\nName=Ghost\nExec=/bin/true\n' > /tmp/apptrol-test-share/applications/ghost.desktop`
  → `/tmp/apptrol-test-share/applications/ghost.desktop` exists
- [ ] Run `XDG_DATA_DIRS="/tmp/apptrol-test-share:${XDG_DATA_DIRS:-/usr/local/share:/usr/share}" apptrol list apps ghost` →
  still no app listed: the first folder's rejected file wins (LAUNCH-02)
- [ ] Replace the symlink with that valid file:
  `rm "${XDG_DATA_HOME:-$HOME/.local/share}/applications/ghost.desktop" && cp /tmp/apptrol-test-share/applications/ghost.desktop "${XDG_DATA_HOME:-$HOME/.local/share}/applications/"`
  → `ghost.desktop` in the user applications folder is now a regular file
- [ ] Run `systemctl --user restart apptrol` → Apptrol is active again
- [ ] Read the journal since that restart → no `desktop file could not be read` record
- [ ] Run `apptrol list apps ghost` → `ghost` is listed
- [ ] Clean up what the case created:
  `rm -rf "${XDG_DATA_HOME:-$HOME/.local/share}/applications/ghost.desktop" /tmp/apptrol-test-share`
  → both paths are removed
- [ ] Run `apptrol list apps ghost` → no app is listed

**Result:** Pass / Fail / Blocked
**Notes:**
