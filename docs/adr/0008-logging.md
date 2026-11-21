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
- 2026-11-21: A failed rotation no longer ends file logging (LOG-07, LOG-08, #250).
  Only an explicit close stops the file; after a failure Apptrol keeps writing:
  ```text
  rotation fails, file still in place ─► keep writing to it; retry once it has grown
                                         by another max_size, or after a reload
  file moved, new one cannot be opened ─► that record is lost; each next record
                                         tries to open the file again
  ```
  The retry waits for growth, not time, so a lasting failure is not retried on every
  record and the behaviour needs no clock. The rotation renames before it gives up the
  open file, and fills only the first free archive number: within the `max_files`
  limit, a failed try costs at most the oldest archive (which that rotation would have
  dropped anyway), and later tries cost none. Archives beyond a lowered `max_files` are
  removed on purpose, failed try or not. A reload with the same path keeps a file that cannot be opened attached
  (with the error and the journal as a fallback until the next reload), so the file
  comes back on its own. A file a reload replaces is closed after the records still
  being written to it, and a reload never waits for a write in progress. Rarer cases
  (paths that share files, a full disk, a file replaced mid-rotation) are left for a
  follow-up issue.
