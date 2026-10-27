# Architecture Decision Records

Each significant decision is recorded here in a short file: what the situation was,
what was decided, and what follows from it. Records are never rewritten; a changed
decision gets a new record that supersedes the old one.

Format: [Michael Nygard's ADR template](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions).
Copy [`template.md`](template.md) and use the next free number.

| # | Decision | Status |
|---|---|---|
| [0001](0001-record-architecture-decisions.md) | Record architecture decisions | Accepted |
| [0002](0002-use-go.md) | Write the service in Go | Accepted |
| [0003](0003-pipewire-via-pulse-protocol.md) | Talk to PipeWire through the PulseAudio protocol | Accepted |
| [0004](0004-toml-config-with-layouts.md) | TOML configuration with the layout structure from the start | Accepted; the `[apps]` table for every target replaced by 0026 |
| [0005](0005-controller-has-priority.md) | The controller always has priority; no pick-up in Phase 1 | Accepted |
| [0006](0006-mute-and-solo.md) | Mute and solo semantics | Accepted |
| [0007](0007-systemd-user-service.md) | Run as a systemd user service | Accepted |
| [0008](0008-logging.md) | Logging with charmbracelet/log, journald and file outputs | Accepted |
| [0009](0009-mit-license.md) | MIT license | Accepted |
| [0010](0010-project-name.md) | Project name "Apptrol" | Accepted |
| [0011](0011-phased-delivery.md) | Deliver in phases; layouts and GUI later | Accepted |
| [0012](0012-testing-strategy.md) | Testing strategy | Accepted |
| [0013](0013-release-packaging.md) | Releases and packaging with GoReleaser | Accepted; attestations and pre-release file names: see notes |
| [0014](0014-logo-and-visual-identity.md) | Logo and visual identity | Accepted |
| [0015](0015-service-architecture.md) | Service architecture and controller access | Accepted |
| [0016](0016-log-records-follow-opentelemetry.md) | Log records follow the OpenTelemetry semantic conventions | Accepted |
| [0017](0017-desktop-services-over-dbus.md) | Talk to desktop services over D-Bus with godbus | Accepted |
| [0018](0018-media-players-through-mpris.md) | Media players through MPRIS: finding, matching and controlling them | Accepted |
| [0019](0019-launcher-and-column-buttons.md) | Launcher buttons, input column buttons and their configuration | Accepted; input column buttons replaced by 0020 |
| [0020](0020-microphone-column-buttons.md) | Microphone column buttons: M talks, S coughs or talks over | Accepted |
| [0021](0021-no-desktop-gui.md) | No desktop GUI: a terminal setup and a tray icon in the service | Accepted |
| [0022](0022-launcher-command-safety.md) | Launcher commands: the user's responsibility, and a denylist of catastrophic commands | Accepted |
| [0023](0023-leds-after-resume-from-sleep.md) | Send every LED again when the computer wakes up, told by logind | Accepted |
| [0024](0024-launchers-only-when-unlocked.md) | Launchers start apps only while the screen is unlocked, unless opted in | Accepted; another user in front: see 0029; switched away: see 0030 |
| [0025](0025-state-kept-while-config-invalid.md) | The saved state is kept while the configuration is invalid | Accepted |
| [0026](0026-separate-tables-for-apps-inputs-outputs.md) | Separate configuration tables for apps, inputs and outputs | Accepted |
| [0027](0027-output-volume.md) | Output volume: a control for a whole output device | Accepted; point 4 superseded for fixed outputs by [0035](0035-first-control-owns-matched-output.md) and for the default output by [0038](0038-default-output-follows-the-server.md), point 7 by [0036](0036-no-solo-on-outputs.md) |
| [0028](0028-docs-as-code-for-the-website.md) | Docs as code for the website; the roadmap as its single source | Accepted |
| [0029](0029-controller-follows-the-user-in-front.md) | The controller follows the user in front of the computer | Accepted |
| [0030](0030-three-screen-states.md) | Three screen states: locked, switched away, logged out | Accepted |
| [0031](0031-manual-test-cases-as-docs.md) | Manual test cases as docs, with fixtures and a record of every run | Accepted; how many run for a patch release: see 0032 |
| [0032](0032-patch-releases-test-what-changed.md) | Patch releases test what changed, plus a smoke set | Accepted |
| [0033](0033-r-and-play-ignore-short-pauses.md) | R and ▶ ignore pauses shorter than half a second | Accepted |
| [0034](0034-containers-through-one-script.md) | Containers: rootless Podman through one script, with an audit record of every run | Accepted; point 4's build-limit mechanism superseded by [0037](0037-builds-in-a-limited-user-scope.md) |
| [0035](0035-first-control-owns-matched-output.md) | The first control owns a matched output device | Accepted; supersedes 0027 point 4 for fixed outputs |
| [0036](0036-no-solo-on-outputs.md) | No solo on outputs | Accepted; supersedes 0027 point 7 |
| [0037](0037-builds-in-a-limited-user-scope.md) | Build images inside a limited, delegated user scope | Accepted; supersedes 0034 point 4's build-limit mechanism |
| [0038](0038-default-output-follows-the-server.md) | The default output follows the server; the first control owns an overlap | Accepted; supersedes 0027 point 4 for the default output |
| [0039](0039-output-volume-keeps-channel-balance.md) | Output volume keeps each channel's balance | Accepted; supersedes the flat-channel decision in 0027's note of 2026-10-20 and 0038's flat-channel consequence; implementation pending |
| [0040](0040-where-agent-rules-live.md) | Where agent rules live: one home per rule; AGENTS.md in the order of a task | Accepted |
