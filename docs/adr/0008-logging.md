# 0008. Logging with charmbracelet/log, journald and file outputs

- **Status:** Accepted
- **Date:** 2026-09-28

## Context

The service runs in the background, so logs are the main way to see what it does. Users
differ: some read `journalctl`, some want a separate file or JSON for log tooling.

## Decision

- Use `github.com/charmbracelet/log` (text, JSON and logfmt formatters; readable coloured
  output in a terminal; compatible with Go's `slog`).
- Two outputs, selectable separately or together: **journald** (stdout with severity
  prefixes under systemd) and **file** (with built-in size-based rotation).
- Each output has its own format. Level and outputs are applied on config reload.
- The default file location is `~/.local/state/apptrol/apptrol.log`; `/var/log` is possible
  with a one-time directory setup.

## Consequences

- Rotation is handled by Apptrol, not `logrotate`.
- High-volume events (every volume step, raw MIDI) are kept at `debug` so normal logs stay small.

## Notes

- 2026-09-30: What each log record contains (message, attribute names, component, errors)
  is decided in [ADR 0016](0016-log-records-follow-opentelemetry.md).
