# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
  M mutes, S solos, LEDs show the state, positions and mutes survive restarts. It creates
  the example configuration on first start and reloads the configuration when the file
  is saved; an invalid file is reported and the previous settings stay active. On stop it
  ends solo, saves the state and turns the LEDs off.
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

### Fixed
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
