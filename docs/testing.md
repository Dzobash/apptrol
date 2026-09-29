# Apptrol — Testing

This page explains how Apptrol is tested and how to run the tests. The reasoning behind the
approach is in [ADR 0012](adr/0012-testing-strategy.md); the requirements are the QA section
of [requirements.md](requirements.md#51-quality-assurance).

## Running the checks

| Command | What it does |
|---|---|
| `make check` | Format check, `go vet`, `golangci-lint`, tests — the same checks CI runs |
| `make test` | Tests with the race detector |
| `make test-audio` | Integration tests against your running PipeWire (they add a silent test output and inputs and remove them afterwards) |
| `make cover` | Tests with coverage; fails below the minimum; writes `coverage.html` to open in a browser |
| `make vulncheck` | Scans dependencies for known vulnerabilities |
| `make lint` | `golangci-lint` only ([install it](https://golangci-lint.run/welcome/install/) first) |

## What runs in CI

Every push to `main` and every pull request runs [`.github/workflows/ci.yml`](../.github/workflows/ci.yml):

| Job | Checks |
|---|---|
| **Lint** | `golangci-lint` with the rules in [`.golangci.yml`](../.golangci.yml) (includes `gofmt` and `goimports`) |
| **Test** | Starts a headless PipeWire, then `go vet` and all tests including the audio integration tests, with the race detector, on the minimum Go version and the latest stable; coverage report in the job summary; fails below the minimum |
| **Vulnerability check** | `govulncheck` |
| **Build** | Builds the binary and runs `apptrol --version` |

## Writing tests

- **No hardware, no network, no desktop.** Tests use fakes for the controller and the audio
  server. Integration tests are the only exception: they talk to a real audio server and
  run only when `APPTROL_PULSE_TEST=1` is set (see `internal/audio/pulse/integration_test.go`).
- **Table-driven.** One test function, a table of cases — see `cmd/apptrol/main_test.go`.
- **Name tests after requirements** where one applies, e.g. `TestSOLO05_PressingSoloAgainTurnsItOff`.
- **Bug fixes** come with a test that fails without the fix.
- **Test behaviour, not lines.** Coverage is a safety net, not the goal.

## Manual hardware checklist

Complete this on a real nanoKONTROL2 (CC mode, Momentary buttons, LED mode *External*)
before each release and note the result in the release description. Start from an empty
state file unless a step says otherwise.

### Phase 1

| # | Step | Expected | Req. |
|---|---|---|---|
| H-00 | `apptrol test`: move every slider and knob, press every button twice | Every control is shown with the right name and 0–127; each button's LED turns on, then off (Track and Marker buttons have no LED) | HW-01, HW-02, HW-07 |
| H-01 | Start the service with the controller unplugged, then plug it in | Log says it is waiting, then connected; LEDs set | SVC-03, LED-07 |
| H-02 | Unplug and replug while running | Disconnect and reconnect logged; LEDs restored | SVC-03 |
| H-03 | Play music in an app assigned to slider 1; move slider 1 from bottom to top | Volume follows smoothly from 0 % to 100 % | CTRL-02 |
| H-04 | Same with an app on a knob | Volume follows | CTRL-02 |
| H-05 | Move an unassigned slider | Nothing changes | CTRL-01 |
| H-06 | Change the app's volume in the desktop mixer, then touch the slider | Volume jumps back to the slider's position | PRIO-01 |
| H-07 | Set slider 1 to about 30 %, then start the app | App starts at about 30 % | PRIO-03 |
| H-08 | Press M on slider 1, then again | App mutes (M lit), then unmutes (M off) | MUTE-01, LED-02 |
| H-09 | Press S on slider 1 | Only that app is audible; S lit on column 1 only; other M LEDs unchanged | SOLO-01, SOLO-02, LED-01, LED-02 |
| H-10 | With solo on column 1, press S on column 2 | Solo moves to column 2 | SOLO-04 |
| H-11 | Press S on column 2 again | Solo off; everything audible except user-muted apps | SOLO-05 |
| H-12 | Mute column 3, solo column 1, end solo | Column 3 is still muted | MUTE-04, SOLO-05 |
| H-13 | Mic on a slider: check LEDs, press M, speak | S, M, R lit; M turns off and mic is muted | LED-04, CTRL-05 |
| H-14 | Solo an app column while the mic is on a slider | Mic is not affected | SOLO-03 |
| H-15 | Edit and save the config (move an app to another slider) | Reload logged; new assignment works without restart | CFG-06 |
| H-16 | Save an invalid config | Error logged; old config keeps working | CFG-07 |
| H-17 | Mute an app, solo another, restart the service | Mute restored, solo off | STATE-03, STATE-04 |
| H-18 | `systemctl --user restart pipewire pipewire-pulse` while running | Reconnect logged; volumes and mutes re-applied | SVC-04 |
| H-19 | Check `journalctl --user -u apptrol` and the log file | Entries at the expected levels and formats | LOG-* |
