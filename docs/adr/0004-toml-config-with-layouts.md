# 0004. TOML configuration with the layout structure from the start

- **Status:** Accepted
- **Date:** 2026-09-28

## Context

Users edit the configuration by hand until the GUI exists, and the GUI will later edit the
same file. Layouts arrive in Phase 2, but the configuration should not change shape when
they do.

## Decision

- The configuration is a TOML file (simpler to read and write by hand than YAML).
- Apps and inputs are defined once under `[apps.<id>]`; layouts refer to them by id.
- Assignments already live under `[layouts.<name>]`. Phase 1 uses only `default`.
- The file is reloaded automatically when saved; an invalid file never replaces a valid one.

## Consequences

- Phase 2 adds more `[layouts.*]` tables without breaking existing files.
- An app's settings (match rules, later its allowed outputs) are shared by all layouts.
- Slightly more structure than a single-layout tool strictly needs.

## Notes

- 2026-10-07: Inputs and outputs get their own tables, `[inputs.<id>]` and
  `[outputs.<id>]`, next to `[apps.<id>]`; the old form is read with a warning until
  1.0. See [ADR 0026](0026-separate-tables-for-apps-inputs-outputs.md). Layouts are
  unchanged.
