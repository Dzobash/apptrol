# 0025. The saved state is kept while the configuration is invalid

- **Status:** Accepted
- **Date:** 2026-10-04

## Context

Restarting Apptrol while `config.toml` was invalid erased the saved slider positions,
mutes and solo ([#77](https://github.com/Dzobash/apptrol/issues/77), found in the
v0.2.0-rc1 hardware checklist). Two rules, each right on its own, combined:

- CFG-07: with an invalid file, Apptrol starts anyway, without assignments, and waits for
  a valid one.
- STATE-07: saved positions and mutes of controls without an app are dropped (meant for
  apps removed from the configuration).

Without a valid configuration, no control has an app, so everything was dropped, and the
empty state was saved over `state.json`. Afterwards talk-over also muted every app, as no
control's position was known.

Options:

- **A. "Last known good"**, as nginx, Caddy, systemd and Kubernetes handle a broken
  configuration: an invalid input never causes a write. Reloads already followed this
  rule ("keeping the current settings"); the start did not.
- **B. A backup of `state.json`** before every write. It protects against unknown future
  bugs too, but adds files and a manual restore, and does not fix the cause. Backups suit
  data that is valuable and hard to recreate (open tabs, documents); slider positions and
  mutes are rebuilt by touching the controls once. `state.json` is already written
  atomically, so a crash cannot leave half a file.

## Decision

Option A.

- Without a valid configuration at start (invalid, unreadable, or missing and the example
  could not be written), the mixer is created with `mixer.NewWithoutConfig(saved)`. It
  controls nothing and keeps the saved state as it is.
- Meanwhile `Snapshot` returns the saved state unchanged, so the saver, which skips data
  equal to what it last wrote, does not write. One exception: a control moved meanwhile
  has a newer position, which is kept and saved. That write comes from the user's
  movement, not from the invalid file.
- The first valid configuration applies the kept state as a normal start does
  (STATE-03, STATE-04), before streams and inputs get their volumes; a control moved
  meanwhile keeps its new position. Only then does STATE-07 drop what has no app.
- The rule is general: **an invalid input must never cause a write.** Later features that
  write files (e.g. `apptrol setup`, ADR 0021) follow it too.

Logs (`apptrol.component=state`):

| Level | Message | Attributes |
|---|---|---|
| info | `saved state kept until a valid configuration is loaded` | `file.path`, `apptrol.state.positions`, `apptrol.state.mutes` |
| info | `saved state applied` | `file.path`, `apptrol.state.positions`, `apptrol.state.mutes` |

They follow the existing `ERRO invalid configuration; waiting for a valid one` and
`INFO configuration reloaded`:

```text
ERRO invalid configuration; waiting for a valid one (run `apptrol check`) apptrol.component=config …
INFO saved state kept until a valid configuration is loaded apptrol.component=state file.path=… apptrol.state.positions=8 apptrol.state.mutes=3
INFO configuration reloaded apptrol.component=config … apptrol.config.controls=9
INFO saved state applied apptrol.component=state file.path=… apptrol.state.positions=8 apptrol.state.mutes=3
```

## Consequences

- A typo in the configuration can no longer cost the saved positions and mutes, however
  often Apptrol is restarted meanwhile.
- New requirement STATE-08; checklist row H-48.
- The mixer has one more state (waiting for a configuration), visible through
  `Mixer.Waiting`; it ends with the first valid configuration and never returns, as a
  later invalid file is rejected on reload and keeps the running settings.
