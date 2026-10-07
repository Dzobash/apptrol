# Apptrol — Roadmap

Each phase ends with a tagged release. Detailed requirements live in
[requirements.md](requirements.md); decisions and their reasons in [adr/](adr/).

| Phase | Name | Topic | Version | Status |
|---|---|---|---|---|
| 0 | It's Dangerous to Go Alone! Take This. | Project setup | 0.0.1 | ✅ Done |
| 1 | Turn Down for What | Core mixer: sliders, knobs, M, S | 0.1.0 | ✅ Released 2026-09-30 |
| 1.5 | You Get a Button! And You Get a Button! | Media, launcher and column buttons | 0.2.0 | ✅ Released 2026-10-04 |
| 1.6 | Show Me What You Got | On-screen display and output volume | 0.3.0 | ⚪ Planned |
| 2 | Multiverse of Madness | Layouts | 0.4.0 | ⚪ Planned |
| 3 | I'm In | Setup and tray | 0.5.0 | ⚪ Planned |
| – | Soon™ | Backlog | – | ⚪ Unscheduled |

`1.0.0` is released once Phases 1–3 are stable and the config format is frozen.

<p align="center">
  <img src="assets/photos/nanokontrol2-controls.png" alt="The controller with its controls marked by version: sliders, knobs, S and M since 0.1.0; R buttons, the transport buttons as media keys, and Marker and Record as launchers since 0.2.0; Track and Cycle for layouts planned for 0.4.0" width="800">
</p>

---

## Phase 0 — It's Dangerous to Go Alone! Take This.

*Project setup · `0.0.1` · ✅ Done*

**Goal:** everything the project needs before the first feature: repository, CI,
releases and the first decisions.

- [x] Requirements, roadmap, configuration reference, decision records
- [x] License (MIT), README, changelog, contributing guide
- [x] Git repository on GitHub (public since the first release, v0.1.0)
- [x] Go module and project skeleton (`cmd/apptrol`, `internal/…`)
- [x] Testing strategy and QA requirements ([ADR 0012](adr/0012-testing-strategy.md), [testing.md](testing.md))
- [x] CI with GitHub Actions: lint, tests (race, coverage gate, two Go versions), `govulncheck`, build
- [x] Release pipeline with GoReleaser on version tags: binaries (x86_64, arm64), .deb, .rpm, checksums ([ADR 0013](adr/0013-release-packaging.md), [releasing.md](releasing.md)); first test release `v0.0.1`
- [x] Dependabot for Go modules and GitHub Actions
- [x] Issue templates (bug report, feature request), pull request template, security policy
- [x] Logo and brand guide: SVG mark and wordmark, PNG sizes, social preview ([brand.md](brand.md), [ADR 0014](adr/0014-logo-and-visual-identity.md))

**Done when:** CI checks every push, a tagged test release (`v0.0.1`) builds binaries and
packages, and the requirements, roadmap and first ADRs are in the repository.

## Phase 1 — Turn Down for What

*Core mixer · `0.1.0` · ✅ Released 2026-09-30*

**Goal:** every app and microphone on its own slider, with mute, solo and LEDs, kept
across restarts.

Requirement areas: HW, CTRL, PRIO, MUTE, SOLO, LED, BTN, STATE, CFG, LOG, SVC, NFR, QA.
Design: [architecture.md](architecture.md), [ADR 0015](adr/0015-service-architecture.md).

- [x] Architecture and controller access decided
- [x] Read MIDI from the controller; handle plug / unplug
- [x] Connect to PipeWire (pulse protocol); track playback streams and capture devices; reconnect
- [x] Match streams and inputs against configured apps
- [x] Sliders and knobs set volume; new streams get the control's position
- [x] M (mute) and S (solo) with the agreed semantics
- [x] LED feedback, including the input-column style
- [x] State saving and restore
- [x] TOML config with layout structure, validation, automatic reload
- [x] `apptrol list`, `apptrol check`, `apptrol test`, `apptrol --version`
- [x] `max_volume` per app, up to 150 % (CTRL-03); warning for overlapping match lists (CFG-12)
- [x] Logging to journald and/or a rotating file
- [x] systemd user unit; packaged in .deb / .rpm
- [x] Interfaces and fakes for the controller and audio server (QA-06)
- [x] Tests for matching, mute/solo logic, config validation and state handling, named after requirement IDs (QA-07)
- [x] Fuzz tests for config parsing and MIDI decoding, run briefly in CI (QA-08)
- [x] Integration tests against headless PipeWire in CI (QA-09)
- [x] Raise the coverage minimum as code grows (QA-04): 75 %
- [x] Manual hardware checklist completed ([testing.md](testing.md), QA-12): release candidates rc1 to rc3

**Done when:** all Phase 1 MUST requirements are met, and the hardware checklist passes on
an installed release candidate. (The original goal of a week as the only volume control
was dropped: three release candidates were tested on hardware and daily use showed no
problems. Anything found later ships as a patch release, `0.1.x`.)

## Phase 1.5 — You Get a Button! And You Get a Button!

*Media, launcher and column buttons · `0.2.0` · ✅ Released 2026-10-04*

**Goal:** every button except the layout buttons gets a function.

Requirement areas: DESK, MEDIA, LAUNCH, INPUT; CFG-13 to CFG-17, LOG-15, LOG-16, LED-09.
Design: [ADR 0017](adr/0017-desktop-services-over-dbus.md), [ADR 0018](adr/0018-media-players-through-mpris.md), [ADR 0019](adr/0019-launcher-and-column-buttons.md), [ADR 0020](adr/0020-microphone-column-buttons.md), [ADR 0022](adr/0022-launcher-command-safety.md), [ADR 0023](adr/0023-leds-after-resume-from-sleep.md), [ADR 0024](adr/0024-launchers-only-when-unlocked.md).

- [x] Decision records (ADR 0017–0019), requirements and hardware checklist (H-23 to H-42)
- [x] D-Bus connection in the service, following the media players (DESK-01 to DESK-03, MEDIA-01; also used by Phases 1.6 and 3)
- [x] ◀◀ ▶▶ ■ ▶ via MPRIS; most recent player by default, optionally pinned; ▶ lit while it plays
- [x] Record (●) and the Marker buttons start apps (desktop ID or command), each in its own systemd unit; already-running behaviour configurable
- [x] `apptrol list apps [search]` shows desktop IDs; `apptrol check` warns about unknown ones
- [x] R on an app column: play / pause that app (MPRIS), LED lit while it plays
- [x] R can be overridden per column as a launcher; launchers and overrides set per layout
- [x] Button configuration per layout, checked on load (CFG-13 to CFG-17)
- [x] M on an input column: mute (toggle) or hold-to-talk, optionally turning apps down while you talk
- [x] S on an input column: hold to mute (cough), or hold to turn apps down (talk-over)
- [x] `--log-level` flag for one run (LOG-16)
- [x] LEDs are sent again after the computer wakes from sleep, told by logind (LED-09)
- [x] Launchers start nothing while the screen is locked, unless set to `when_locked`; presses at the lock screen are logged as warnings (LAUNCH-13 to LAUNCH-15)

**Done when:** all Phase 1.5 MUST requirements are met, and the full hardware checklist
passes on an installed release candidate.

## Phase 1.6 — Show Me What You Got

*On-screen display and output volume · `0.3.0` · ⚪ Planned*

**Goal:** see every change on screen, and control whole outputs (speakers, headphones),
not only apps.

Split from Phase 1.5 on 2026-10-02: the on-screen display differs per desktop and should
not hold back the media buttons. Output volume added on 2026-10-07 (#85, #86).

- [x] Decision records for separate configuration tables and output volume ([ADR 0026](adr/0026-separate-tables-for-apps-inputs-outputs.md), [ADR 0027](adr/0027-output-volume.md))
- [ ] Separate tables `[apps]`, `[inputs]`, `[outputs]`; the old `type = "input"` still read with a warning until 1.0 (CFG-18 to CFG-22, #86)
- [ ] Output volume on a slider or knob: a fixed device, or the system's default output; M, S and R on output columns; outputs in `apptrol list` (OUT-01 to OUT-13, #85)
- [ ] On-screen feedback: KDE volume OSD, notification fallback for other desktops
- [ ] Config switch to turn on-screen feedback off
- [ ] Desktop notifications when something needs attention: invalid configuration, controller unplugged ([ADR 0021](adr/0021-no-desktop-gui.md))
- [ ] Show microphone button states and hints, e.g. "cough has no use with hold-to-talk: release M to mute" (ADR 0020)

**Done when:** all Phase 1.6 MUST requirements are met, volume and mute changes show on
KDE and as a notification on GNOME, configurations in the old form still load with a
warning, and the hardware checklist, including the new output rows, passes on an
installed release candidate.

## Phase 2 — Multiverse of Madness

*Layouts · `0.4.0` · ⚪ Planned*

**Goal:** several sets of assignments (e.g. *Work*, *Gaming*), switched from the
controller.

- [ ] Multiple layouts with their own assignments and saved state
- [ ] Switch behaviour per layout: fixed value (with per-control overrides), last values, carry over
- [ ] Pick-up after a switch; blinking M LED while a control is waiting
- [ ] Track ◀ / ▶ to step through layouts; Cycle + S / M / R to jump to one (up to 24)
- [ ] Optional automatic switching by running application
- [ ] Resolve open question Q-1

**Done when:** layouts switch with Track and Cycle, each brings back its own positions,
mutes and solo, pick-up works with a blinking M LED, Q-1 is decided, and the hardware
checklist passes on an installed release candidate.

## Phase 3 — I'm In

*Setup and tray · `0.5.0` · ⚪ Planned*

**Goal:** set up Apptrol without editing a file, and see its status in the tray.

No desktop GUI ([ADR 0021](adr/0021-no-desktop-gui.md)): the binary stays pure Go, and
`config.toml` stays the only place settings live.

- [ ] `apptrol setup` in the terminal (bubbletea, bubbles, huh, lipgloss): the layout as columns, apps picked from what is playing, button settings
- [ ] Save to `config.toml` keeping its comments and layout (Q-6)
- [ ] Tests without a terminal (teatest); a GIF of the setup in the README (vhs)
- [ ] Tray icon in the service (StatusNotifierItem): status, tooltip with the problem, menu with *Open configuration*, *Show log*, *Set up…*

**Done when:** a layout can be set up completely in `apptrol setup`, saving keeps the
file's comments, the tray icon shows status on KDE, and the hardware checklist passes on
an installed release candidate.

## Soon™

*Backlog · unscheduled*

**Goal:** ideas worth keeping that have no release yet.

- Recording control beyond starting an app (e.g. OBS WebSocket, Record LED shows recording state) ([#62](https://github.com/Dzobash/apptrol/issues/62))
- Read the controller's LED mode over SysEx and warn only when it is "Internal" (see HW-03) ([#63](https://github.com/Dzobash/apptrol/issues/63))
- Support for other MIDI controllers ([#64](https://github.com/Dzobash/apptrol/issues/64))
- Native journald protocol for structured log fields (Q-5) ([#65](https://github.com/Dzobash/apptrol/issues/65))
- Man page and shell completions (bash, zsh, fish) in the packages, written without a command-line framework (ADR 0021) ([#66](https://github.com/Dzobash/apptrol/issues/66))

---

## Out of roadmap

Ideas that were dropped or will not be done, and why.

| Idea | Why not | Decided in |
|---|---|---|
| A desktop GUI program | Needs cgo, and would keep settings in a second place; `apptrol setup` and the tray icon replace it | [ADR 0021](adr/0021-no-desktop-gui.md) |
| Moving an app to another output with R | R plays or pauses its app, or starts an app; no press is left for it | [#61](https://github.com/Dzobash/apptrol/issues/61) |
| An LED mark or blinking on output columns | More complicated than it is worth; output columns light like app columns | [ADR 0027](adr/0027-output-volume.md) |
| A microphone "bleep" and other audio effects | Audio effects and routing are out of scope | [requirements §2.3](requirements.md#23-never-in-scope) |
| Windows and macOS | Apptrol is built on PipeWire, systemd and Linux's MIDI devices | [requirements §2.3](requirements.md#23-never-in-scope) |
| Replacing an audio interface's own software | Apptrol works next to it, e.g. next to the GoXLR utility | [requirements §2.3](requirements.md#23-never-in-scope) |

## How this roadmap changes

- Phases are delivered **in order**; each ends with a tagged release. Before `1.0.0`, a
  phase raises the minor version ([releasing.md](releasing.md)).
- The roadmap changes **through pull requests**, like the code; the decisions behind a
  change are recorded in an [ADR](adr/).
- New ideas start as an **issue**. Without a planned release they get the label
  `backlog` and are listed under *Soon™*.
- Dropped ideas are **closed as *not planned*** with the reason, removed from the phases
  and listed under *Out of roadmap*
  ([CONTRIBUTING › Issues](https://github.com/Dzobash/apptrol/blob/main/CONTRIBUTING.md#issues)).
- A phase is done when its **Done when** line is met. After its release, its list stays
  as it was.

## Scope

**Vision:** a hardware mixer for Linux: every app, microphone and output on a physical
fader under your fingers.

**In scope**

- Volume, mute and solo of apps, inputs and outputs from a MIDI controller, the Korg
  nanoKONTROL2 first
- Buttons for media keys and for starting apps
- Layouts: several sets of assignments, switched from the controller
- A setup in the terminal and a tray icon
- Any Linux desktop with PipeWire (or PulseAudio) and systemd
- One pure Go binary, without C libraries

**Out of scope**

- Audio routing and effects (EQ, compression, mixing streams, a "bleep")
- Windows and macOS
- Replacing an audio interface's own software
- A desktop GUI program
- Code that needs cgo
