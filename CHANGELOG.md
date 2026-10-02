# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **R** on an app's column plays or pauses that app through its media player: it pauses
  every playing player of the app, or resumes the paused one that played last; a stopped
  player is never started, as with a keyboard's play / pause key (MEDIA-04, MEDIA-07,
  MEDIA-08). The R LED is lit while one of the app's players is playing, and goes off
  as soon as it pauses, also when it is paused in the app itself (MEDIA-09, LED-03).
  `r1 = { mode = "off" }` turns it off. README: *Known issues* explains why R pauses only
  one browser tab.
- Code of conduct (Contributor Covenant 2.0, GitHub's template), linked from the README
  and the contributing guide.
- Apptrol connects to the desktop's D-Bus session bus and finds the media players
  (MPRIS), following them as they start, stop and change between playing and paused
  (DESK-01 to DESK-03, MEDIA-01). Nothing uses them yet; the media buttons follow.
  Without a session bus everything else keeps working, and Apptrol reconnects.
- Each media player is matched to the app on a slider or knob through the app's `match`
  list: its bus name, its name or its desktop ID (MEDIA-03). Players on other devices
  (KDE Connect), `playerctld` and `plasma-browser-integration` are ignored (MEDIA-02).
  The log says which control a player belongs to and why (`media player matched`).
- On a microphone's slider, holding **S** mutes the microphone while you cough
  (INPUT-02). The M LED goes dark while it is muted; S and R stay lit to mark the
  column as an input (LED-04).
- Buttons are configured per layout in `[layouts.<name>.buttons]` (CFG-13): on a
  microphone's column, M can be `hold_to_talk` (live only while held) and turn the other
  apps down while you talk (`talk_over = true`); S can be `talk_over` or `off`. The
  input's `talk_over_volume` sets how far apps go down (default 25 %). Launchers,
  `play_pause` on R and `[media] player` are checked already and work later in 0.2.0.
- `apptrol check` rejects wrong button settings, one line per problem, and lists the
  buttons that are set (CFG-14). Every configured button is logged on load
  (`button configured`).
- Held buttons end when the controller is unplugged, the configuration changes their
  mode, or Apptrol stops; the log says why (`apptrol.held.reason`).
- `apptrol --log-level debug` (or `info`, `warn`, `error`) sets the log level for one run,
  without changing the configuration file; it also holds when the configuration is
  reloaded. The first log line says where the level comes from (LOG-16).
- README: building from source, and running a self-built Apptrol as a user service.
- README and roadmap: a picture of the controller showing which controls work now and
  which are planned (`docs/assets/photos/nanokontrol2-controls.png`, CC BY-NC-SA 3.0).
- README: mutes made with M stay set while Apptrol is stopped; ADR 0006 records why.

### Changed
- The default log level is `warn`: the journal stays quiet in daily use. Set
  `level = "info"`, or run `apptrol --log-level info`, to see every mute, solo and app
  found (LOG-02). Configuration files that set `level` keep their value.
- ADR 0020 replaces the input column part of ADR 0019: M is the microphone button
  (`mute` or `hold_to_talk`), S coughs or talks over, R on an input column is `off` or a
  launcher. The input's talk-over setting is named `talk_over_volume`.
- Roadmap: no desktop GUI (ADR 0021). Phase 3 becomes *Setup and tray*: `apptrol setup`
  in the terminal, editing `config.toml`, and a status icon in the tray. Notifications
  for problems join the on-screen display in Phase 1.6; a man page and shell completions
  are planned for later.
- Roadmap: the on-screen display gets its own release (Phase 1.6, `0.3.0`) after the
  media buttons (`0.2.0`); layouts move to `0.4.0`, the GUI to `0.5.0`.
- Roadmap: `0.2.0` also plans app launchers on Record and the Marker buttons, play /
  pause per slider on R, and a cough button and talk-over for the microphone. The
  microphone "bleep" is dropped: it would be an audio effect, which is out of scope.

## [0.1.0] - 2026-09-30

First release: Phase 1, the core mixer.

### Added
- Project documentation: requirements, roadmap, configuration reference, decision records.
- Example configuration and systemd user unit.
- Go project skeleton: `apptrol` command with `--version`, `--config` and placeholder
  `run`, `list` and `check` commands; Makefile for building and testing.
- Testing strategy (ADR 0012), QA requirements and manual hardware checklist (`docs/testing.md`).
- CI workflow: lint, tests with race detector and coverage gate on two Go versions,
  vulnerability scan, build and smoke test.
- Release pipeline: GoReleaser builds binaries, .deb/.rpm packages and checksums for
  amd64 and arm64 on every version tag (ADR 0013, `docs/releasing.md`).
- Dependabot for Go modules and GitHub Actions; issue and pull request templates;
  security policy.
- Branch and pull request workflow in the contributing guide.
- Logo and brand guide (`docs/brand.md`, ADR 0014): SVG mark, wordmark and lockups,
  PNG sizes from 16 to 512 px, GitHub social preview.
- Architecture for Phase 1 (`docs/architecture.md`, ADR 0015); checklist for making the
  repository public in the release guide.
- Configuration loading and validation (`internal/config`): every problem in a file is
  reported at once with its setting name, unknown settings are rejected, and friendly
  messages explain wrong value types.
- `apptrol check` validates the configuration and shows which app is on which control.
- CI runs every fuzz test for 20 seconds on each push.
- README: prerequisites, controller settings (with SysEx Controls for Linux), first
  steps and a disclaimer.
- Logging (`internal/logging`): journald output with priorities (coloured text with
  timestamps when started from a terminal), log file with size-based rotation, separate
  formats per output (text, JSON, logfmt), level and outputs changeable at runtime; falls
  back to the journal if the log file cannot be opened.
- Mixer core (`internal/mixer`): matching streams and inputs to controls, volume, mute,
  exclusive solo, LED states, new streams getting the control's position, saved state,
  configuration changes. Tested per requirement against a simulated audio server and
  controller, plus a fuzz test over random event sequences.
- New requirement SVC-07: on shutdown, end solo so no app stays muted by it.
- Saved state (`internal/state`): positions and user mutes in
  `~/.local/state/apptrol/state.json`, written atomically and at most once per second;
  a missing or damaged file means starting fresh, single bad entries are skipped.
- Audio server connection (`internal/audio/pulse`): tracks playing apps and input devices,
  sets volume and mute, reconnects when PipeWire restarts. Volumes match the percentages
  desktop mixers show. When PipeWire restores an app's old volume just after Apptrol set
  it, Apptrol sets it again.
- `apptrol list` shows playing apps and input devices with the names used for matching,
  their volume, and which control each one is on.
- CI runs integration tests against a headless PipeWire; `make test-audio` runs them
  against your own.
- Controller (`internal/controller`): reads the nanoKONTROL2 through raw MIDI, finds it by
  its sound card id, waits for it and picks it up again after unplugging, drives the
  button LEDs, and names the device if another program holds it. The MIDI decoder is
  fuzz-tested.
- `apptrol test` shows what the controller sends and toggles each button's LED, to check
  the controller and its LED mode. It says which buttons have no LED (Track and Marker).
- `apptrol run` (the default command) runs the service: sliders and knobs set volumes,
  M mutes, S solos, LEDs show the state, positions, mutes and the solo survive restarts. It creates
  the example configuration on first start and reloads the configuration when the file
  is saved; an invalid file is reported and the previous settings stay active. On stop it
  saves the state, ends solo (so no app stays silent while Apptrol is stopped) and turns the
  LEDs off.
- New requirement LED-08: LEDs are turned off when Apptrol stops.
- `max_volume` per app (1–150 %, default 100): the control spans 0 to that value, for a
  boost above 100 % or a cap below it. Changing it in a running Apptrol applies at once.
- CI requires at least 75 % test coverage (was 70 %).
- README: known issues (Spotify resets its own volume on track changes).
- README: why Apptrol exists, and a photo of the controller (CC BY-NC-SA 3.0, from
  iFixit, credited in `docs/assets/photos/`).
- Brand guide: lockup proportions (mark and wordmark) with a drawing, and how to edit and
  remake the logo files; a README in the asset folder points to the guide.
- Warning when two apps' match lists overlap, naming the app that gets the streams
  (CFG-12); the matching rule is documented in the configuration reference.
- Log records follow the OpenTelemetry semantic conventions (ADR 0016, LOG-10 to LOG-13):
  every line names its component (`apptrol.component`), errors carry `error.type` and
  `exception.message`, attribute names are standard, and every record is one line. Lines
  about a control name the layout, the control and the app on it; every button press is
  logged (LOG-14). Tests check every log call against these rules.
- User guide for the logs (`docs/logging.md`): how to read a line, levels, components,
  error types, examples, how to find lines, how to collect them into Loki, Elasticsearch
  or the OpenTelemetry Collector, and every attribute.
- Log timestamps have milliseconds, so events within one second keep their order.
- README: Apptrol has been tested on one system only (Kubuntu, KDE Plasma, PipeWire,
  amd64); reports from other systems are welcome.

### Fixed
- Mute changes made outside Apptrol are followed: unmuting an app in the desktop's volume
  applet (or muting the mic with a mute key) now updates the M LED and the saved state
  (MUTE-07). Solo still keeps other apps silent. Found in testing v0.1.0-rc2.
- The solo survives a restart of Apptrol: it is saved with the state and restored on
  start (STATE-04 changed; solo was not saved before). Found in testing v0.1.0-rc2.
- LEDs show the right state again after PipeWire restarts: they are sent again when the
  audio server reconnects, and once more 2 seconds later (LED-07). Found in testing
  v0.1.0-rc2.
- An invalid configuration is logged as one line per problem instead of one record
  spanning several lines. Found in testing v0.1.0-rc2.
- The message after a package upgrade includes `systemctl --user daemon-reload`, and the
  README explains upgrading. Found in testing v0.1.0-rc2.
- LEDs show the right state again after the controller is unplugged and plugged back in:
  the controller ignores LED messages while it starts up, so the state is now sent again
  after 0.5 and 2 seconds, and LED messages are spaced out. Found in testing v0.1.0-rc1.
- The warning for several matching input devices says how to fix it; the configuration
  reference explains devices that share a description (such as the GoXLR Mini's inputs).
- Hardware checklist: clearer expectations for H-02 (LEDs after replugging) and H-03 (no
  on-screen popup before Phase 1.5).
- Documentation matches the Phase 1 code: architecture, contributing guide, testing,
  requirements (CTRL-02, NFR-03), configuration reference; release candidates described
  in the release guide; implementation notes added to ADR 0015.
- Example configuration and configuration reference: `[controller] port` is the ALSA
  card id from `/proc/asound/cards`, not a name from `aseqdump -l`.

[Unreleased]: https://github.com/Dzobash/apptrol/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Dzobash/apptrol/releases/tag/v0.1.0
