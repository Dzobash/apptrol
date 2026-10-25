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
  | debug | `status line sent` | `apptrol.component=systemd`, `apptrol.status` |
  | debug | `status line not sent; Apptrol was not started by systemd` (once) | `apptrol.component=systemd`, `apptrol.status` |
  | warn | `could not send the status line` | `apptrol.component=systemd`, `apptrol.status`, `error.type=status_not_sent`, `exception.message` |

  Review (2026-10-21): writing to systemd's socket has no time limit and blocks when
  systemd stops reading. The event loop must never wait for that (ADR 0015), so the
  adapter sends from its own goroutine, the newest text wins, and shutdown waits at most
  200 ms for the last one. The owner chose this over a write deadline in the loop.

## Status-line solo note (2026-10-25)

SVC-13, #168: the owner chose to always name an active app solo, replacing the
assignment count to keep the line short. It remains visible when the controller is
missing or released, because solo still applies then. Waiting for valid configuration
and stopping retain their short texts.

```
off       <version> · controller connected · 8 controls assigned
playing   <version> · controller connected · solo on slider 5 (VLC)
paused    <version> · controller connected · solo on slider 5 (VLC not playing)
```

Playing follows the definition already decided in #101: any matched uncorked stream
or any matched MPRIS player reporting `Playing`. Stream-only would miss a player that
reports playback without a stream; MPRIS-only would miss games and other apps without
MPRIS. Neither signal means `not playing`, without claiming whether the app is closed
or paused. The LED pause delay does not alter this fact. A user mute can still silence
an app that plays (SOLO-06); outputs remain outside app solo (ADR 0036).

The mixer exposes only the solo column, configured app name and playing boolean
through a read-only accessor. The service formats the text after events; the existing
changed-text comparison avoids duplicate sends (ADR 0015). Showing solo only while
silent would hide why other apps cannot be heard during playback; appending it after
the assignment count would make the line longer.

Logging uses the existing records and attribute, with no new log decision or key:

| Level | Message | Attributes |
|---|---|---|
| debug | `status line sent` | `apptrol.component=systemd`, `apptrol.status` (includes the solo and whether it is not playing) |
| debug | `status line not sent; Apptrol was not started by systemd` (once) | `apptrol.component=systemd`, `apptrol.status` |
| warn | `could not send the status line` | `apptrol.component=systemd`, `apptrol.status`, `error.type=status_not_sent`, `exception.message` |

#101's blinking S and additional solo logs remain separate work.
