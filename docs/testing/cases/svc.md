# Service test cases

The service: how it starts, what `systemctl --user status apptrol` shows, how it runs
beside another user's Apptrol. Each case runs with Apptrol installed from the candidate
package and started by systemd, unless a step says otherwise.

### TC-SVC-001 — The Status line starts with the version and says what Apptrol is doing

Covers: SVC-13, SVC-09 · Phase 1.6
Config: two-apps
Type: Manual

```
systemctl --user status apptrol
     Active: active (running) since …
     Status: "<version> · controller connected · 2 controls assigned"
```

**Setup**
- [ ] The controller is plugged in; Apptrol was installed from the candidate package
- [ ] `systemctl --user daemon-reload`, then `systemctl --user restart apptrol`, so systemd runs the installed unit and binary
- [ ] A second user account exists for the last step; otherwise mark that step blocked

**Steps**
- [ ] Run `apptrol --version` → note the version, e.g. `v0.3.0`
- [ ] Run `systemctl --user status apptrol` → the `Status:` line is `"<that version> · controller connected · 2 controls assigned"`
- [ ] Unplug the controller and run `systemctl --user status apptrol` again → `Status:` is `"<version> · controller not found"`
- [ ] Plug the controller in and run `systemctl --user status apptrol` once more → `Status:` is `"<version> · controller connected · 2 controls assigned"` again
- [ ] In `~/.config/apptrol/config.toml` add `knob3 = "nothing"` under `[layouts.default]`, save, and run `systemctl --user restart apptrol`, then `systemctl --user status apptrol` → `Status:` is `"<version> · waiting for a valid configuration"`
- [ ] Remove the line again, save, wait two seconds and run `systemctl --user status apptrol` → `Status:` is `"<version> · controller connected · 2 controls assigned"`
- [ ] Run `systemctl --user stop apptrol`, then `apptrol --log-level debug` in a terminal → the log has `status line not sent; Apptrol was not started by systemd` once and no ERROR; Ctrl+C stops it
- [ ] Run `systemctl --user start apptrol`, note the time, then *Switch user* and log in as the second user, then *Switch user* and log in as yourself again
- [ ] Run `journalctl --user -u apptrol --since <noted time> | grep "status line sent"` → in this order, `apptrol.status` ends with `controller released (another user in front)`, `controller not found`, `controller connected · 2 controls assigned`

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-SVC-003 — The Status line names an active solo

Covers: SVC-13, SOLO-01, SOLO-04, SOLO-05, STATE-04 · Phase 1.6
Config: two-apps
Type: Manual

```
S1, tone A closed   controller connected · solo on slider 1 (Test tone A not playing)
start tone A       controller connected · solo on slider 1 (Test tone A)
S2                 controller connected · solo on slider 2 (Test tone B not playing)
S2 again           controller connected · 2 controls assigned
```

**Setup**
- [ ] Install the candidate, apply `two-apps`, and start Apptrol with the controller plugged in
- [ ] Close both test tones; use the quiet test tones described in [how to run release tests](../how-to-run-release-tests.md) when a step asks to start one
- [ ] Read `systemctl --user status apptrol` after each step below; every text starts with the installed version and ` · `

**Steps**
- [ ] Press S1 → `controller connected · solo on slider 1 (Test tone A not playing)`
- [ ] Start tone A → the suffix becomes `(Test tone A)`; the assignment count is absent
- [ ] Close tone A → the suffix becomes `(Test tone A not playing)`
- [ ] Restart Apptrol → the saved solo returns with the same Status text
- [ ] Unplug the controller → `controller not found · solo on slider 1 (Test tone A not playing)`
- [ ] Plug it back in → the solo stays visible after `controller connected`
- [ ] Press S2 → `controller connected · solo on slider 2 (Test tone B not playing)`
- [ ] Press S2 again → `controller connected · 2 controls assigned`
- [ ] Inspect the debug journal's `status line sent` records → `apptrol.status` shows each changed solo text

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-SVC-002 — The unit has Apptrol's summary as its title and still starts at once

Covers: SVC-13 · Phase 1.6
Config: none (package check)
Type: Manual

**Setup**
- [ ] The candidate package is installed, and `systemctl --user daemon-reload` has been run

**Steps**
- [ ] Run `systemctl --user status apptrol` → the first line reads `apptrol.service - Apptrol: control each app's volume with a hardware MIDI controller`
- [ ] Run `systemctl --user cat apptrol` → `Description=` is that text, `Documentation=` is the repository's address, `Type=simple` and `NotifyAccess=main` are present, and `Type=notify` is not
- [ ] Run `systemctl --user restart apptrol` → the command returns at once, without a wait of about 90 seconds
- [ ] Open the package's AppStream data (`/usr/share/metainfo/io.github.dzobash.apptrol.metainfo.xml`) → its `<summary>` is the same text as `Description=` after `Apptrol: `

**Result:** Pass / Fail / Blocked
**Notes:**
