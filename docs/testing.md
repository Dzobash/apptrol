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
| `make test-desktop` | D-Bus integration tests in a private session bus (`dbus-run-session`): media players, and a fake logind standing in for the system bus; your desktop's buses and media players are not touched |
| `make test-launcher` | Starts harmless test units (`true`) in your systemd user manager and checks they are removed; CI has no user systemd, so run it before a release |
| `make cover` | Tests with coverage; fails below the minimum; writes `coverage.html` to open in a browser |
| `make vulncheck` | Scans dependencies for known vulnerabilities |
| `make lint` | `golangci-lint` only ([install it](https://golangci-lint.run/welcome/install/) first) |

## What runs in CI

Every push to `main` and every pull request runs [`.github/workflows/ci.yml`](../.github/workflows/ci.yml):

| Job | Checks |
|---|---|
| **Lint** | `golangci-lint` with the rules in [`.golangci.yml`](../.golangci.yml) (includes `gofmt` and `goimports`) |
| **Test** | Starts a headless PipeWire, then `go vet` and all tests including the audio integration tests, with the race detector, on the minimum Go version and the latest stable; coverage report in the job summary; fails below the minimum |
| **Fuzz** | Runs every fuzz test (config, saved state, MIDI decoding, mixer) for 20 seconds |
| **Vulnerability check** | `govulncheck` |
| **Build** | Builds the binary and runs `apptrol --version` |

## Writing tests

- **No hardware, no network, no desktop.** Tests use fakes for the controller and the audio
  server. Integration tests are the only exception: they talk to a real audio server and
  run only when `APPTROL_PULSE_TEST=1` is set (see `internal/audio/pulse/integration_test.go`),
  and against a private D-Bus with fake media players or a fake logind when
  `APPTROL_DBUS_TEST=1` is set (see `internal/desktop/integration_test.go` and
  `internal/session/session_test.go`). CI runs both.
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
| H-02 | Mute two apps, then unplug and replug the controller while running | Disconnect and reconnect logged; within 2 seconds the LEDs show the same state as before (M lit for the muted apps, input column lit) | SVC-03, LED-07 |
| H-03 | Play music in an app assigned to slider 1; move slider 1 from bottom to top | Volume follows smoothly from 0 % to 100 %; the desktop's volume settings show the same percentage (there is no on-screen popup yet; that comes in Phase 1.6) | CTRL-02 |
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
| H-17 | Mute an app, solo another (e.g. slider 1), restart the service (`systemctl --user restart apptrol`) | Mute restored; solo restored: S lit on the same column and only that app audible | STATE-03, STATE-04 |
| H-18 | With an app muted and another soloed: `systemctl --user restart pipewire pipewire-pulse` while running | Reconnect logged; volumes, mutes and solo re-applied; within 2 seconds the LEDs show the same state as before | SVC-04, LED-07 |
| H-19 | Check `journalctl --user -u apptrol` and the log file, including the lines from H-16 and the button presses from H-08 to H-12 | Entries at the expected levels and formats, as in [`logging.md`](logging.md); every line has `apptrol.component`; each error has `error.type`; each configuration problem is its own single line; mute and solo lines name layout, control, app and button | LOG-* |
| H-20 | Set `max_volume = 150` for the app on slider 1, save, move slider 1 to the top | The desktop mixer shows 150 %; with the slider in the middle, 75 % | CTRL-03, CFG-06 |
| H-21 | Assign two apps whose match lists overlap (`"fire"` and `"firefox"`); run `apptrol check` | Warning names both apps and which one gets the streams | CFG-12 |
| H-22 | With music on slider 1: press M1, then unmute the app in the Plasma volume applet; then mute it there again. Also mute the mic with the desktop (or `pactl set-source-mute`) | M1 turns off, then on again, following the applet; the mic's M LED turns off; each change is logged as "muted/unmuted outside Apptrol" and survives a restart | MUTE-07, LED-02 |

### Phase 1.5

Set the log level to `debug` (`[log] level`, or `apptrol --log-level debug` in a terminal)
so the reasons in the log can be checked too. Unless a step says otherwise: Spotify on
slider 1, a browser on slider 2, the microphone on slider 8.

| # | Step | Expected | Req. |
|---|---|---|---|
| H-23 | Play music in Spotify, then a video in the browser. Press ▶, then ▶ again; then ▶▶ | The browser video pauses and ▶'s LED goes off, then it plays and ▶ lights (it started playing most recently); ▶▶ goes to the next item in the browser | MEDIA-05, MEDIA-06, LED-06 |
| H-24 | Set `[media] player = "spotify"`, save; with the browser video playing last, press ▶; then close Spotify and press ▶ | Spotify pauses, the browser video keeps playing; with Spotify closed, ▶ does nothing | MEDIA-06, CFG-15 |
| H-25 | Press ■ while VLC plays, then ▶ | VLC stops, then starts the track again from the beginning (Spotify only pauses on ■, see *Known issues*) | MEDIA-05 |
| H-26 | With Spotify playing, press R1; then R1 again; then pause Spotify in its own window | Spotify pauses and the R1 LED goes off at once; then it plays and R1 lights; pausing in Spotify turns R1 off too | MEDIA-07, MEDIA-09 |
| H-27 | Play videos in two browser tabs; press R2; then M2 | R2 pauses only the most recently used tab; the other keeps playing (known issue), R2 goes off. M2 silences both tabs | MEDIA-07, MEDIA-09 |
| H-28 | Put an app without MPRIS on a slider (e.g. mpv without its plugin, or Discord) and play sound; press its R | Nothing happens; its R LED stays off; the log says there is no media player for the app | MEDIA-08, MEDIA-09 |
| H-29 | Restart the browser and play a video again; press R2 | R2 pauses it (the player's new instance number is found) | MEDIA-01, MEDIA-03 |
| H-30 | If KDE Connect is used: play Spotify on the phone; press R1 and ▶ | Neither affects the phone; the log shows the phone's player as ignored (`other_device`) | MEDIA-02 |
| H-31 | Set `record = { app = "<desktop ID>" }` (find it with `apptrol list apps <name>`), save, press ● | The app starts; the Record LED flashes briefly; the log names the unit | LAUNCH-01, LAUNCH-04, LAUNCH-08, LAUNCH-09 |
| H-32 | With that app open: `systemctl --user restart apptrol` | The app keeps running | LAUNCH-04 |
| H-33 | Set `if_running = "skip"`; with the app open from the menu, press ●; then close it, start it from a terminal and press ● again; press ● a few more times in a row with the app closed | Not started a second time either way; the log says how it was found (`unit`, then `process`). Presses in a row all work | LAUNCH-04, LAUNCH-06, LAUNCH-07 |
| H-43 | Put a Steam game's desktop ID on a launcher with `if_running = "skip"`; press it twice while the game runs; quit the game, leave Steam open, press it again | Steam does not start the game twice; after quitting, it starts again although Steam still runs | LAUNCH-07 |
| H-34 | Set `marker_prev = { command = ["konsole", "-e", "htop"] }` (or another terminal) and press Marker ◀ | The command runs in a new window | LAUNCH-05 |
| H-35 | Set a desktop ID that is not installed; run `apptrol check` | A warning names it; the rest of the configuration is valid | LAUNCH-10 |
| H-36 | Hold S8 while speaking (watch the desktop's microphone level or a recording), release | The mic is muted only while S8 is held; M8 is off while held, lit after; S8 and R8 stay lit | INPUT-02, LED-04 |
| H-37 | Set `m8 = { mode = "hold_to_talk" }`, save; speak, then hold M8 and speak; then unmute the mic in the desktop's volume applet | Muted (M8 off) until M8 is held; live (M8 lit) while held; the applet's unmute is undone and logged | INPUT-03, LED-04 |
| H-38 | In mode `mute`: press M8 (mic muted), then hold and release S8 (cough) | The mic stays muted throughout and after | INPUT-05 |
| H-39 | Set `s8 = { mode = "talk_over" }`. Music at about 80 %: hold S8; while holding, move slider 1 to the middle; release. Repeat with an app whose slider has not been moved since it was assigned | While held, apps drop to 25 % (desktop mixer); slider 1's move has no effect until release, then Spotify is at its new position. The unmoved app is muted while held, and the log says why | INPUT-04, INPUT-06, CFG-16 |
| H-40 | Set `m8 = { mode = "hold_to_talk", talk_over = true }`: hold M8 and unplug the controller; then plug it in, and stop Apptrol | While held, the mic is live and the music down; on unplug, the mic is muted, the music back, and the log says the held state ended (`controller_disconnected`). After stopping, the mic is still muted | INPUT-03, INPUT-04, INPUT-07 |
| H-41 | Run `apptrol --log-level debug` in a terminal (service stopped), then Ctrl+C and start the service | Debug lines in the terminal; the start record says the level came from the flag; the config file is unchanged; the service logs at the configured level | LOG-16 |
| H-42 | With `debug` set, check the log after H-23 to H-40 | Every player is listed as matched (with `matched_by`), not matched or ignored (with the reason); D-Bus records carry `apptrol.component=desktop` | LOG-10, LOG-15 |
| H-44 | Mute the app on slider 1 (M1 lit; S8, M8, R8 lit). `systemctl suspend`, wait a minute, wake the computer; look at the controller before touching it (lock screen still up). Repeat with `systemctl hibernate` if hibernation is set up | Within about 5 seconds of waking up, M1 and S8, M8, R8 are lit again without pressing anything. No `controller disconnected` in the log; it shows `system going to sleep` (debug), then `system resumed; sending LEDs again` with `apptrol.component=session` | LED-09 |
| H-45 | Set `record = { app = "<desktop ID>" }`. Lock the screen (Meta+L), press ●, M1 and S1, move slider 1; unlock and read the log | Nothing starts, the Record LED does not flash; M1 and S1 work (M1 lit). The log has `launchers blocked` (`apptrol.screen.state=locked`), one warning `launcher pressed while the screen is locked; nothing started` for ●, one `button pressed while the screen is locked` each for M1 and S1, none for the slider, then `launchers allowed: the screen is unlocked`. After unlocking, ● starts the app | LAUNCH-13, LAUNCH-15 |
| H-46 | Set `record = { command = ["notify-send", "Apptrol", "started while locked"], when_locked = true }`, save; lock the screen, press ●, unlock | The configuration load warns about `when_locked`; after unlocking, the notification is there; the log has `launcher pressed while the screen is locked; started (when_locked)` | LAUNCH-14 |
| H-47 | With a second user account: *Switch user*, log in as the other user, press ● (without `when_locked`) and M1; switch back | Nothing starts; the warnings say `apptrol.screen.state=inactive` | LAUNCH-13, LAUNCH-15 |
