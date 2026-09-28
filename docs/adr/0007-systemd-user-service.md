# 0007. Run as a systemd user service

- **Status:** Accepted
- **Date:** 2026-09-28

## Context

PipeWire runs per logged-in user. A system-wide service running as root cannot reach the
user's audio session without workarounds.

## Decision

Apptrol runs as a systemd **user** service (`systemctl --user`), shipped as
`apptrol.service` in the packages. It never needs root.

## Consequences

- Files follow the XDG base directories: config in `~/.config/apptrol/`, state and default
  log in `~/.local/state/apptrol/`.
- Writing logs to `/var/log` needs a one-time directory setup by the user (see
  `docs/config.md`).
- The service runs only while the user is logged in, which matches when audio is used.
