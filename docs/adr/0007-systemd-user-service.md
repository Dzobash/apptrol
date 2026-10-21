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

## Notes

- 2026-10-21: The Status line (SVC-13, #99). `systemctl --user status apptrol` shows a
  line Apptrol sets with `STATUS=` over sd_notify, from go-systemd's `daemon` package
  (pure Go; `sdjournal` stays out, ADR 0017). The owner chose to keep `Type=simple` and
  add `NotifyAccess=main`, over `Type=notify`:

  ```
  Type=simple + NotifyAccess=main   start: systemd calls it started when the process forks (as before)
  Type=notify                       start: systemd waits for READY=1; without it, it kills Apptrol
                                    after 90 s and Restart=on-failure starts it again
  ```

  Why: for Apptrol "ready" says almost nothing. A missing controller or a configuration
  that is not valid yet are normal states, so `READY=1` could only mean that the main loop
  runs, milliseconds after the start. `Type=notify` would add a way to fail at start (a
  new unit file with an older binary) and make `enable --now` wait. If something later
  needs to be ordered after Apptrol, `Type=notify` can follow with its own ADR.
  The unit's `Description=` is now `Apptrol: control each app's volume with a hardware
  MIDI controller`, the AppStream summary; `Documentation=` stays the repository.

  The text is the version, then the state (`v0.3.0 · controller connected · 6 controls
  assigned`, `controller not found`, `controller released (another user in front)`,
  `waiting for a valid configuration`, `stopping`). It counts the controls the
  configuration assigns, not the apps that are running. Only a changed text is sent.

  | Level | Message | Attributes |
  |---|---|---|
  | debug | `status line sent` | `apptrol.status` |
  | debug | `status line not sent; Apptrol was not started by systemd` (once) | `apptrol.status` |
  | warn | `could not send the status line` | `apptrol.status`, `error.type=status_not_sent`, `exception.message` |
