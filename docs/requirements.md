# Apptrol — Requirements

| | |
|---|---|
| **Status** | Draft — Phase 1 requirements agreed, later phases outlined |
| **Last updated** | 2026-09-28 |
| **Related** | [Roadmap](roadmap.md) · [Configuration reference](config.md) · [Decision records](adr/) |

## 1. Purpose

Apptrol turns a hardware MIDI controller into a per-application volume mixer on Linux.
Each slider or knob controls the volume of one application (or an input such as a
microphone), so the operating system's software mixer is no longer needed for
day-to-day volume changes.

The first supported controller is the **Korg nanoKONTROL2**. Apptrol is designed to run
next to a main audio interface (for example a GoXLR Mini), but it does not depend on one
and works just as well with a plain internal sound card.

### 1.1 Conventions

- The key words **MUST**, **SHOULD** and **MAY** are used as described in
  [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).
- Every requirement has a stable ID (`AREA-NN`). IDs are never reused; a dropped
  requirement is marked *withdrawn* rather than deleted. Commits, issues and tests
  reference these IDs.
- **Phase** says when a requirement is delivered. Only Phase 1 is specified in full;
  later phases are outlined in section 5 and in the [roadmap](roadmap.md).

### 1.2 Terms

| Term | Meaning |
|---|---|
| **Control** | A physical slider or knob on the controller. |
| **Column** | One of the eight vertical strips on the nanoKONTROL2: a slider, a knob above it, and the S / M / R buttons next to it. |
| **Target** | What a control acts on: an *app* (one or more playback streams) or an *input* (a capture device such as a microphone). |
| **App** | A target defined in the config, matched against playback streams by name. |
| **Layout** | A set of control → target assignments. Phase 1 has exactly one layout, `default`. |
| **User mute** | Mute set by pressing a column's M button. |
| **Solo** | A temporary state in which only one column's app stays audible. |
| **Position** | The last known physical value of a control (0–127). *Unknown* until the control is first moved, unless restored from saved state. |

## 2. Scope

### 2.1 In scope (Phase 1)

- Sliders and knobs set the volume of configured apps and inputs.
- M (mute) and S (solo) buttons for slider columns, with LED feedback.
- The controller always has priority over other volume changes.
- State is saved and restored across restarts.
- A TOML configuration file that already uses the layout structure, reloaded automatically.
- Structured logging to journald and/or a log file.
- Runs as a systemd user service on any Linux desktop with PipeWire.

### 2.2 Out of scope for Phase 1

Media buttons, on-screen popups, multiple layouts, pick-up, a GUI, recording control,
moving apps between outputs (R), mic bleep. See section 5 and the [roadmap](roadmap.md).

### 2.3 Never in scope

- Audio routing or effects (EQ, compression, mixing streams together).
- Windows or macOS support.
- Replacing the audio interface's own software (e.g. the GoXLR utility).

## 3. Hardware assumptions

| ID | Requirement | Level | Phase |
|---|---|---|---|
| HW-01 | Apptrol MUST support the Korg nanoKONTROL2 in its factory **CC mode** (not a DAW mode). | MUST | 1 |
| HW-02 | LED feedback requires the controller's **LED mode set to "External"** (one-time setting in Korg's editor). Apptrol MUST work without LED feedback if this is not set. | MUST | 1 |
| HW-03 | Apptrol SHOULD log a hint about the LED mode setting when it starts and LEDs are expected to be used. | SHOULD | 1 |
| HW-04 | The controller mapping (CC numbers) SHOULD be defined in one place in the code so other controllers can be added later. | SHOULD | 1 |
| HW-05 | The controller MUST be found by its ALSA card id (from `[controller] port`), not by a fixed card or device number. | MUST | 1 |
| HW-06 | If the controller's MIDI device is busy (held by another program), Apptrol MUST log a clear error naming the device and retry. | MUST | 1 |

Factory CC numbers of the nanoKONTROL2 (MIDI channel 1). Buttons send 127 on press and 0 on release.

| Element | Columns 1–8 | | Element | CC |
|---|---|---|---|---|
| Slider | 0–7 | | Track ◀ / ▶ | 58 / 59 |
| Knob | 16–23 | | Cycle | 46 |
| S button | 32–39 | | Marker Set / ◀ / ▶ | 60 / 61 / 62 |
| M button | 48–55 | | ◀◀ / ▶▶ | 43 / 44 |
| R button | 64–71 | | ■ / ▶ / ● | 42 / 41 / 45 |

## 4. Phase 1 requirements

### 4.1 Controls and volume

| ID | Requirement | Level |
|---|---|---|
| CTRL-01 | Each slider and each knob MAY be assigned one target in the active layout. Unassigned controls MUST do nothing. | MUST |
| CTRL-02 | Moving a control MUST set the volume of its target to the control's position, mapped linearly from 0–127 to 0–100 %. | MUST |
| CTRL-03 | Volume MUST stay within 0–100 %. Boosting above 100 % is not supported. | MUST |
| CTRL-04 | For an **app** target, the volume MUST be applied to every playback stream that matches the app, including several streams of the same program (e.g. several browser tabs). | MUST |
| CTRL-05 | For an **input** target, the volume MUST be applied to the matching capture device. | MUST |
| CTRL-06 | Apps and inputs that are not assigned to any control MUST NOT be touched. | MUST |
| CTRL-07 | A burst of control events (moving a slider quickly) SHOULD be coalesced so that the latest value is applied without flooding the audio server. The final position MUST always be applied. | SHOULD |
| CTRL-08 | A volume change SHOULD reach the audio server within 50 ms of the control being moved. | SHOULD |

### 4.2 Controller priority

| ID | Requirement | Level |
|---|---|---|
| PRIO-01 | The controller always has priority: whenever a control is moved, its target MUST be set to the control's position, regardless of any volume change made elsewhere (e.g. in the desktop's mixer). | MUST |
| PRIO-02 | Volume changes made outside Apptrol MUST NOT be reverted actively; they are overridden the next time the control is moved. *(Rationale: some apps lower other apps' volume on purpose, e.g. voice-chat ducking.)* | MUST |
| PRIO-03 | When a new playback stream appears that matches an assigned app, its volume MUST be set to the control's known position immediately. | MUST |
| PRIO-04 | When a new stream appears and the control's position is *unknown*, the stream's volume MUST be left unchanged. | MUST |
| PRIO-05 | A new stream of an app that is user-muted or silenced by solo MUST be muted when it appears. | MUST |

### 4.3 Mute (M)

| ID | Requirement | Level |
|---|---|---|
| MUTE-01 | Pressing M on a slider column MUST toggle the user mute of that column's target. | MUST |
| MUTE-02 | Knobs have no mute; a knob's target MUST only be silenced by solo (SOLO-03). | MUST |
| MUTE-03 | Pressing M on a column without a slider target MUST do nothing. | MUST |
| MUTE-04 | User mute and solo are separate states. An app's stream is muted when it is user-muted **or** silenced by solo. | MUST |
| MUTE-05 | Pressing M on any column while solo is active MUST toggle that column's user mute; the effect becomes audible once solo ends. | MUST |

### 4.4 Solo (S)

| ID | Requirement | Level |
|---|---|---|
| SOLO-01 | Pressing S on a slider column whose target is an app MUST turn solo on for that column. | MUST |
| SOLO-02 | While solo is active, every other **app** target in the layout (on sliders and on knobs) MUST be silenced. | MUST |
| SOLO-03 | Solo MUST NOT affect input targets. | MUST |
| SOLO-04 | Only one column can be soloed at a time. Pressing S on another column MUST move the solo to that column. | MUST |
| SOLO-05 | Pressing S on the soloed column MUST turn solo off. Every app then returns to its own state: user-muted apps stay muted, all others become audible. Solo MUST NOT return to a previously soloed column. | MUST |
| SOLO-06 | A soloed column's own user mute still applies (soloing a user-muted app results in silence). | MUST |
| SOLO-07 | Pressing S on an input column MUST do nothing in Phase 1. The button is reserved for a future bleep function. | MUST |

### 4.5 LED feedback

| ID | Requirement | Level |
|---|---|---|
| LED-01 | **App column** — the S LED MUST be lit only on the soloed column. | MUST |
| LED-02 | **App column** — the M LED MUST be lit when the column's app is user-muted. It MUST NOT light up for apps that are only silenced by solo. | MUST |
| LED-03 | **App column** — the R LED MUST be off in Phase 1. | MUST |
| LED-04 | **Input column** — S, M and R LEDs MUST be lit by default so the column is recognisable as an input. When the input is muted, only the M LED MUST turn off. | MUST |
| LED-05 | Columns without a slider target MUST have all LEDs off. | MUST |
| LED-06 | Transport button LEDs MUST be off in Phase 1. | MUST |
| LED-07 | LEDs MUST be re-sent whenever the controller (re)connects and whenever the configuration or state changes. | MUST |

### 4.6 Other buttons

| ID | Requirement | Level |
|---|---|---|
| BTN-01 | R buttons, transport buttons, Track, Cycle and Marker buttons MUST do nothing in Phase 1. Their functions are reserved for later phases. | MUST |

### 4.7 State

| ID | Requirement | Level |
|---|---|---|
| STATE-01 | Apptrol MUST save, per layout and per control, the last known position and, per slider column, the user mute. | MUST |
| STATE-02 | State MUST be saved after every change, at most once per second, and written atomically (write to a temporary file, then rename). | MUST |
| STATE-03 | On start, saved positions and user mutes MUST be restored and applied to matching streams and inputs. | MUST |
| STATE-04 | Solo MUST NOT be saved. Apptrol always starts with solo off. | MUST |
| STATE-05 | If the state file is missing or unreadable, Apptrol MUST start with all positions *unknown* and no user mutes, leave current volumes unchanged, and log a warning (unless it is the first start). | MUST |
| STATE-06 | The state file MUST be stored at `$XDG_STATE_HOME/apptrol/state.json` (default `~/.local/state/apptrol/state.json`). | MUST |
| STATE-07 | State of controls whose assignment was removed from the config SHOULD be discarded. | SHOULD |

### 4.8 Configuration

| ID | Requirement | Level |
|---|---|---|
| CFG-01 | Configuration MUST be a TOML file at `$XDG_CONFIG_HOME/apptrol/config.toml` (default `~/.config/apptrol/config.toml`). A different path MAY be given with `--config`. | MUST |
| CFG-02 | Apps and inputs MUST be defined once in an `[apps.<id>]` table; layouts MUST refer to them by id. | MUST |
| CFG-03 | Assignments MUST live in a `[layouts.<name>]` table. Phase 1 MUST use the layout named `default`; other layouts MUST be accepted and ignored with an info log. | MUST |
| CFG-04 | An app MUST be matched by a list of case-insensitive substrings compared with the stream's `application.name` and `application.process.binary`. | MUST |
| CFG-05 | An input MUST be matched by a case-insensitive substring of the capture device's name or description. | MUST |
| CFG-06 | Apptrol MUST reload the configuration automatically when the file is saved, without restarting. | MUST |
| CFG-07 | An invalid configuration MUST be rejected with an error log naming the problem; the previous valid configuration MUST stay active. If there is no valid configuration at start, Apptrol MUST start, log the error and wait for a valid file. | MUST |
| CFG-08 | Validation MUST reject: unknown control names, references to undefined apps, the same app assigned to more than one control in a layout, and empty match lists. | MUST |
| CFG-09 | If no configuration file exists, Apptrol SHOULD create a commented example file and log where it is. | SHOULD |
| CFG-10 | A command `apptrol list` SHOULD print the currently playing streams and capture devices with the names used for matching, to help write the config. | SHOULD |
| CFG-11 | A command `apptrol check` SHOULD validate the configuration file and exit. | SHOULD |

### 4.9 Logging

| ID | Requirement | Level |
|---|---|---|
| LOG-01 | Apptrol MUST use structured logging (charmbracelet/log). | MUST |
| LOG-02 | The log level MUST be configurable: `debug`, `info`, `warn`, `error`. | MUST |
| LOG-03 | Two outputs MUST be available, usable separately or together: **journald** and **file**. | MUST |
| LOG-04 | Each output MUST have its own format: journald `text` or `logfmt`; file `text`, `json` or `logfmt`. | MUST |
| LOG-05 | When running under systemd, the journald output MUST mark each line with its severity so that `journalctl -p` filtering works. When started from a terminal, it MUST print coloured text to the terminal instead. | MUST |
| LOG-06 | The file output MUST default to `$XDG_STATE_HOME/apptrol/apptrol.log`; the path MUST be configurable (e.g. to `/var/log/apptrol/` if the user has prepared that directory). | MUST |
| LOG-07 | The file output MUST rotate by size, keeping a configurable number of old files. | MUST |
| LOG-08 | Log level and outputs MUST be updated on config reload without restarting. | MUST |
| LOG-09 | Events MUST be logged at these levels: **info** — start/stop, config loaded/reloaded, controller connected/disconnected, stream matched to a control, mute/solo changes; **warn** — config entry that matches nothing, controller not found, state file unreadable; **error** — invalid config, lost connection to the audio server; **debug** — every volume change and raw MIDI message. | MUST |

### 4.10 Service and runtime

| ID | Requirement | Level |
|---|---|---|
| SVC-01 | Apptrol MUST run as a **systemd user service** (per logged-in user, not as root). | MUST |
| SVC-02 | Apptrol MUST NOT require root privileges for normal operation. | MUST |
| SVC-03 | If the controller is not connected, Apptrol MUST keep running, wait for it, and pick it up when it is plugged in. Unplugging MUST be handled the same way. | MUST |
| SVC-04 | If the connection to the audio server is lost (e.g. PipeWire restart), Apptrol MUST reconnect and re-apply the current state. | MUST |
| SVC-05 | Apptrol MUST shut down cleanly on SIGTERM/SIGINT, saving state first. | MUST |
| SVC-06 | `apptrol --version` MUST print the version, commit and build date. | MUST |

## 5. Non-functional requirements

| ID | Requirement | Level | Phase |
|---|---|---|---|
| NFR-01 | Apptrol MUST run on Linux with PipeWire (through `pipewire-pulse`). It SHOULD also work on plain PulseAudio. | MUST | 1 |
| NFR-02 | The service MUST NOT depend on a specific desktop environment. It MUST work on KDE Plasma and GNOME, on Wayland and X11. | MUST | 1 |
| NFR-03 | Apptrol MUST be written in Go and ship as a single binary without runtime dependencies beyond the ALSA library. | MUST | 1 |
| NFR-04 | CPU usage while idle SHOULD be close to 0 %; memory use SHOULD stay below 30 MB. | SHOULD | 1 |
| NFR-05 | Releases MUST be built by CI and published with binaries for x86_64 and arm64, .deb and .rpm packages (including the systemd user unit) and checksums. | MUST | 0 |
| NFR-06 | The project MUST be published under the MIT license. | MUST | 0 |
| NFR-07 | Documentation MUST NOT use Korg trademarks in the project name or logo, and MUST state that the project is not affiliated with Korg. | MUST | 0 |

### 5.1 Quality assurance

How these are met is described in [ADR 0012](adr/0012-testing-strategy.md) and
[testing.md](testing.md).

| ID | Requirement | Level | Phase |
|---|---|---|---|
| QA-01 | Every push and pull request MUST run CI: format check, `go vet`, `golangci-lint`, tests with the race detector, vulnerability scan, and a build with a smoke test. | MUST | 0 |
| QA-02 | Changes SHOULD only reach `main` when CI passes. Once the repository is public, this MUST be enforced with branch protection. | SHOULD | 0 |
| QA-03 | Tests MUST run against the minimum Go version from `go.mod` and against the latest stable Go release. | MUST | 0 |
| QA-04 | CI MUST fail when total test coverage drops below the configured minimum (currently 70 %). Logic packages (matching, mixer state, config, saved state) SHOULD reach at least 85 %. | MUST | 0 |
| QA-05 | Dependencies MUST be checked with `govulncheck`; a known vulnerability in code Apptrol actually calls MUST fail CI. | MUST | 0 |
| QA-06 | Access to the controller and to the audio server MUST go through interfaces, so that all behaviour can be tested with fakes, without hardware or PipeWire. | MUST | 1 |
| QA-07 | Every Phase 1 MUST requirement that can be tested without hardware MUST have at least one automated test. Test names SHOULD include the requirement ID. | MUST | 1 |
| QA-08 | Configuration parsing and MIDI message decoding MUST have fuzz tests; CI SHOULD run each for a short time on every push. | MUST | 1 |
| QA-09 | Integration tests against a real, headless PipeWire SHOULD run in CI (build tag `integration`). | SHOULD | 1 |
| QA-10 | Automated tests (except integration tests) MUST NOT need network access, hardware or a desktop session. | MUST | 0 |
| QA-11 | A bug fix SHOULD include a test that fails without the fix. | SHOULD | 1 |
| QA-12 | Before each release, the manual hardware checklist in [testing.md](testing.md) MUST be completed on a real controller. | MUST | 1 |

## 6. Later phases (outline)

These are agreed directions, not yet full requirements. They will be refined and given IDs
before the phase starts. See the [roadmap](roadmap.md).

### Phase 1.5 — Media buttons and on-screen display
- ◀◀ ▶▶ ■ ▶ control media playback through MPRIS. Default: the most recently active player; optionally pinned to one player in the config.
- On-screen feedback when a volume or mute changes: KDE's native volume OSD when available, a desktop notification elsewhere (e.g. GNOME).

### Phase 2 — Layouts
- Several layouts (e.g. *Work*, *Gaming*), each with its own assignments.
- Every layout keeps its own positions, user mutes and solo state, and restores them when it becomes active. Switching layouts does not change the state of apps that are not part of the new layout.
- Per layout, what happens on switch: set controls to a **fixed value** (a layout-wide default with optional per-control overrides), restore **this layout's last values**, or **carry over** the physical positions from the previous layout.
- **Pick-up** (as on the GoXLR): after a switch, a control does nothing until its physical position crosses the target's current level. The M LED of a waiting column blinks.
- Track ◀ / ▶ step through layouts; Cycle + S / M / R on a column jumps directly to a layout (up to 24).
- Optional automatic switching (e.g. when a game starts).

### Phase 3 — GUI
- A main window with a layout drop-down and a list of every control's target and current percentage.
- A tray icon. The GUI talks to the running service; it is not required for the service to work.

### Backlog (unscheduled)
- **Record (●)**: start/stop recording in a configured app (OBS via its WebSocket API, or custom start/stop commands); LED shows recording state.
- **R — move app to another output**: cycle an app through a per-app list of allowed output devices; R LED shows when the app is not on its home output. Needs further discussion.
- **Bleep** on an input column's S button: replace the microphone signal with a tone while held.
- Functions for the Marker buttons.
- Support for other MIDI controllers.

## 7. Open questions

| # | Question | Phase |
|---|---|---|
| Q-1 | When switching from a layout where an app is user-muted to a layout that does not contain that app, should the app stay muted or become audible until you switch back? | 2 |
| Q-2 | Should R also cycle an input column between input devices? | Backlog |
| Q-3 | Should an output move made with R persist after the app restarts, and is it per layout or global? | Backlog |
| Q-4 | How does the GUI talk to the service (D-Bus or a local socket)? To be decided in an ADR. | 3 |
| Q-5 | Should the journald output use the native journal protocol (structured fields) instead of stdout with severity prefixes? | 1 |
