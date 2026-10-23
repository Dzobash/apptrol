# Test cases

The manual test cases, one file per area, and the format every case follows. Why some
things are tested by hand is in [Testing strategy](../strategy.md); how to run them for a
release is in [Running the release tests](../how-to-run-release-tests.md).

## The format

Every case has the same parts, in this order:

```markdown
### TC-SOLO-001 — Stopping Apptrol during solo leaves no app muted

Covers: SVC-07, STATE-04 · Phase 1
Config: two-apps
Type: Manual
Was: H-17

**Setup**
- [ ] Both test tones play; slider 1 and slider 2 in the middle

**Steps**
- [ ] Press S1 → S1 lights up; only tone A is heard
- [ ] Run `systemctl --user stop apptrol` → all LEDs go out; both tones are heard
- [ ] Run `systemctl --user start apptrol` → S1 lights up again; only tone A is heard

**Result:** Pass / Fail / Blocked
**Notes:**
```

| Part | Rule |
|---|---|
| Id and title | `TC-<AREA>-<NNN>`: the area as below, a three-digit number that is never reused. The title says what must hold, not what is done. |
| `Covers:` | The requirement IDs the case checks, then the phase: `SVC-07, STATE-04 · Phase 1`. A search for a requirement ID then finds the requirement, its Go test and its case. |
| `Config:` | The [fixture](../fixtures/) the case runs with, applied with `apply-fixture.sh <name>`. Every runtime case names one, so it starts from the same setup on every run. Package-only cases use `none (package check)` because they do not run the service. |
| `Type:` | `Manual`, or `Automated` for a risk covered by a Go test: then the case has no steps, only `Test:` with the test's name. |
| `Was:` | The step of the old hardware checklist it replaces, if any (H-00 to H-51). Optional. |
| Setup | What must be true before the first step, as checkboxes. The fixture is not repeated here. |
| Steps | Checkboxes. **One action per step**, then `→`, then what is seen, heard or in the log. A step with two actions becomes two steps. |
| Result | `Pass`, `Fail` or `Blocked` (could not be run, e.g. no second user account), and a `Notes:` line. In this file the result stays empty: results go into the [run record](../runs/template.md). |

What makes a good expected result:

- **Observable:** an LED, a sound, a number in the desktop's volume settings, a log line.
  "Works" is not a result.
- **Log lines** are quoted with their fixed message and the attribute that matters:
  `` `controller connected` ``, `` `apptrol.seat.front=other_user` ``.
- **No values from your own system:** no device names, IDs or process numbers. Use the
  fixture's names (`apptrol-test-a`, `apptrol-test-mic`) or placeholders (`<device>`).

## Areas

One file per area, named after the requirement group; within a file, the cases that could
leave audio muted or too loud come first.

| Area | File | Checks |
|---|---|---|
| HW | `hw.md` | The controller: found, plugged in and out, permissions |
| CTRL | `ctrl.md` | Sliders and knobs set the volume; `max_volume` |
| PRIO | `prio.md` | The controller has priority; new streams take the control's position |
| MUTE | `mute.md` | M, and mutes made outside Apptrol |
| SOLO | `solo.md` | S, and solo across stops and restarts |
| LED | `led.md` | What the LEDs show |
| STATE | `state.md` | What is kept across restarts |
| CFG | `cfg.md` | The configuration: reload, invalid files, `apptrol check` |
| LOG | `log.md` | Log outputs, levels and records |
| SVC | [svc.md](svc.md) | The service: start, stop, crash, several users; the Status line *(Phase 1.6)* |
| MEDIA | [media.md](media.md) | R on app columns and the media keys *(Phase 1.5)* |
| LAUNCH | `launch.md` | Launcher buttons and the lock screen *(Phase 1.5)* |
| INPUT | `input.md` | Microphone buttons and talk-over *(Phase 1.5)* |
| NFR | `nfr.md` | Package descriptions, copyright and license texts |
| QA | [qa.md](qa.md) | Container restrictions, audit records and real systemd checks |
| OUT | [out.md](out.md) | Output devices: listing, volume, M, S and R on output columns *(Phase 1.6)* |

The area files are added in the pull requests that turn the old
[hardware checklist](../../testing.md#manual-hardware-checklist) into cases: Phase 1
first, then Phase 1.5. Until then, the checklist is what a release is tested with.
