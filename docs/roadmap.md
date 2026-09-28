# Apptrol — Roadmap

Each phase ends with a tagged release. Detailed requirements live in
[requirements.md](requirements.md); decisions and their reasons in [adr/](adr/).

| Phase | Goal | Version | Status |
|---|---|---|---|
| 0 | Project setup | – | 🟡 In progress |
| 1 | Core mixer: sliders, knobs, M, S | 0.1.0 | ⚪ Planned |
| 1.5 | Media buttons and on-screen display | 0.2.0 | ⚪ Planned |
| 2 | Layouts | 0.3.0 | ⚪ Planned |
| 3 | GUI and tray | 0.4.0 | ⚪ Planned |
| – | Backlog | – | ⚪ Unscheduled |

`1.0.0` is released once Phases 1–3 are stable and the config format is frozen.

---

## Phase 0 — Project setup

- [x] Requirements, roadmap, configuration reference, decision records
- [x] License (MIT), README, changelog, contributing guide
- [x] Git repository on GitHub (private until the first release)
- [x] Go module and project skeleton (`cmd/apptrol`, `internal/…`)
- [x] Testing strategy and QA requirements ([ADR 0012](adr/0012-testing-strategy.md), [testing.md](testing.md))
- [x] CI with GitHub Actions: lint, tests (race, coverage gate, two Go versions), `govulncheck`, build
- [ ] Release pipeline with GoReleaser on version tags: binaries (x86_64, arm64), .deb, .rpm, checksums
- [ ] Dependabot for Go modules and GitHub Actions
- [ ] Issue templates (bug report, feature request)

## Phase 1 — Core mixer (`0.1.0`)

Requirement areas: HW, CTRL, PRIO, MUTE, SOLO, LED, BTN, STATE, CFG, LOG, SVC, NFR, QA.

- [ ] Read MIDI from the controller; handle plug / unplug
- [ ] Connect to PipeWire (pulse protocol); track playback streams and capture devices; reconnect
- [ ] Match streams and inputs against configured apps
- [ ] Sliders and knobs set volume; new streams get the control's position
- [ ] M (mute) and S (solo) with the agreed semantics
- [ ] LED feedback, including the input-column style
- [ ] State saving and restore
- [ ] TOML config with layout structure, validation, automatic reload
- [ ] `apptrol list`, `apptrol check`, `apptrol --version`
- [ ] Logging to journald and/or a rotating file
- [ ] systemd user unit; packaged in .deb / .rpm
- [ ] Interfaces and fakes for the controller and audio server (QA-06)
- [ ] Tests for matching, mute/solo logic, config validation and state handling, named after requirement IDs (QA-07)
- [ ] Fuzz tests for config parsing and MIDI decoding, run briefly in CI (QA-08)
- [ ] Integration tests against headless PipeWire in CI (QA-09)
- [ ] Raise the coverage minimum as code grows (QA-04)
- [ ] Manual hardware checklist completed ([testing.md](testing.md), QA-12)

**Done when:** all Phase 1 MUST requirements are met, and Apptrol has run as the only
volume control on the author's desktop for a week without problems.

## Phase 1.5 — Media buttons and on-screen display (`0.2.0`)

- [ ] ◀◀ ▶▶ ■ ▶ via MPRIS; most recent player by default, optionally pinned
- [ ] On-screen feedback: KDE volume OSD, notification fallback for other desktops
- [ ] Config switch to turn on-screen feedback off

## Phase 2 — Layouts (`0.3.0`)

- [ ] Multiple layouts with their own assignments and saved state
- [ ] Switch behaviour per layout: fixed value (with per-control overrides), last values, carry over
- [ ] Pick-up after a switch; blinking M LED while a control is waiting
- [ ] Track ◀ / ▶ to step through layouts; Cycle + S / M / R to jump to one (up to 24)
- [ ] Optional automatic switching by running application
- [ ] Resolve open question Q-1

## Phase 3 — GUI and tray (`0.4.0`)

- [ ] Decide how the GUI talks to the service (ADR; open question Q-4)
- [ ] Choose the GUI toolkit (ADR)
- [ ] Main window: layout drop-down, control assignments with current percentages
- [ ] Tray icon (StatusNotifierItem; works on KDE and on GNOME with AppIndicator support)

## Backlog

- Record button (●): OBS WebSocket or custom start/stop commands, LED shows recording state
- R: move an app between a per-app list of outputs (needs discussion; Q-2, Q-3)
- Bleep on an input column's S button
- Marker buttons
- Support for other MIDI controllers
- Native journald protocol for structured log fields (Q-5)
