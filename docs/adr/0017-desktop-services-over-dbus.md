# 0017. Talk to desktop services over D-Bus with godbus

- **Status:** Accepted
- **Date:** 2026-10-02

## Context

From Phase 1.5 on, Apptrol talks to programs on the desktop, not only to the audio server
and the controller: media players (MPRIS) for the media buttons and R, systemd for
starting apps in their own scope (launchers), and later the on-screen display (Phase 1.6)
and the GUI (Phase 3, Q-4). All of them are reached over the D-Bus **session bus**, the
per-user bus the desktop session starts.

Options for talking to D-Bus from Go:

- **`godbus/dbus/v5`**: pure Go, BSD-2-Clause, v5.2.2 (December 2025). The standard Go
  D-Bus library, used by Docker, containerd and `coreos/go-systemd`.
- **`libdbus` or `sd-bus` through cgo**: conflicts with `CGO_ENABLED=0` (ADR 0013).
- **Running `busctl` or `dbus-send`**: a process per call, text parsing, and no good way
  to receive signals (e.g. "a player started playing").

No other maintained pure-Go D-Bus library exists. Wrapper libraries for MPRIS exist, but
are small and rarely maintained, and MPRIS needs only a handful of calls.

## Decision

- Apptrol uses `godbus/dbus/v5`.
- One connection to the session bus, shared by every feature that needs D-Bus, in a new
  adapter package (`internal/desktop`). The calls Apptrol needs (MPRIS, later
  notifications) are written there directly on godbus, without wrapper libraries.
- Calls to systemd (starting apps in their own scope, finding running ones) use
  `coreos/go-systemd/v22` (Apache-2.0), which is built on godbus and keeps its own
  connection to systemd. Its `journal`, `daemon` and `login1` packages may serve later
  needs (structured journal fields, Q-5; a watchdog; resume from suspend). Its
  `sdjournal` package needs cgo and must not be imported (ADR 0013).
- As in ADR 0015, the service uses the adapter through a small interface: signals become
  events for the mixer, and the mixer's actions (play, pause, start an app) are carried
  out by the adapter. The mixer stays pure.
- Without a session bus, or when the connection is lost, sliders, knobs, M and S keep
  working; only features that need D-Bus do nothing. Apptrol logs an error and
  reconnects, as it does for the audio server (SVC-04).
- Log records from the adapter carry `apptrol.component=desktop` (ADR 0016).
- Tests: unit tests use a fake behind the interface (QA-06); integration tests run
  against a private bus started with `dbus-run-session`, only when an environment
  variable is set, as the audio integration tests do (QA-09).

Which media player a button controls, and how apps are started, are decided in their own
records.

## Consequences

- Two more external libraries besides `jfreymuth/pulse`, confined to one package. They
  are checked by `govulncheck` and kept current by Dependabot. If one were abandoned, the
  adapter is the only code that would need to change.
- The binary stays pure Go and static (ADR 0013).
- LOG-10 gains the component `desktop`; `docs/logging.md` documents it.
- CI gets a D-Bus integration test job next to the PipeWire one.
- The GUI (Phase 3) can later talk to the service over the same session bus.

## Notes

- 2026-10-02: There will be no GUI program ([ADR 0021](0021-no-desktop-gui.md)). The
  session bus connection serves the media players, the on-screen display and
  notifications (Phase 1.6) and the tray icon (Phase 3), all inside the service.
- 2026-10-04: Resume from sleep is followed through logind's `PrepareForSleep` signal on
  the **system bus**, with a connection of its own in `internal/power`
  ([ADR 0023](0023-leds-after-resume-from-sleep.md)). The session bus connection stays
  shared by everything on the session bus.
