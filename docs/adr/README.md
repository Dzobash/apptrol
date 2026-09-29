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
| [0004](0004-toml-config-with-layouts.md) | TOML configuration with the layout structure from the start | Accepted |
| [0005](0005-controller-has-priority.md) | The controller always has priority; no pick-up in Phase 1 | Accepted |
| [0006](0006-mute-and-solo.md) | Mute and solo semantics | Accepted |
| [0007](0007-systemd-user-service.md) | Run as a systemd user service | Accepted |
| [0008](0008-logging.md) | Logging with charmbracelet/log, journald and file outputs | Accepted |
| [0009](0009-mit-license.md) | MIT license | Accepted |
| [0010](0010-project-name.md) | Project name "Apptrol" | Accepted |
| [0011](0011-phased-delivery.md) | Deliver in phases; layouts and GUI later | Accepted |
| [0012](0012-testing-strategy.md) | Testing strategy | Accepted |
| [0013](0013-release-packaging.md) | Releases and packaging with GoReleaser | Accepted |
| [0014](0014-logo-and-visual-identity.md) | Logo and visual identity | Accepted |
