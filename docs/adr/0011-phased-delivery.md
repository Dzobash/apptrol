# 0011. Deliver in phases; layouts and GUI later

- **Status:** Accepted
- **Date:** 2026-09-28

## Context

The full feature set (layouts, pick-up, GUI, media, recording, output switching, bleep) is
large. Building it at once would delay a usable tool.

## Decision

Deliver in phases (see `docs/roadmap.md`): Phase 1 is the core mixer with a single layout,
M and S. Media buttons and on-screen display follow in 1.5, layouts in 2, the GUI in 3.
Recording, output switching (R), bleep and Marker buttons stay in the backlog.

## Consequences

- A usable tool early; later phases are refined with real usage experience.
- The config format and code must leave room for later phases (see ADR 0004).

## Notes

- 2026-10-02: Phase 1.5 is split in two. Media buttons stay in Phase 1.5 (`0.2.0`); the
  on-screen display becomes Phase 1.6 (`0.3.0`). MPRIS is one standard on every desktop,
  while the on-screen display differs per desktop and should not hold back the media
  buttons. Layouts move to `0.4.0` and the GUI to `0.5.0`; the phase numbers stay.
- 2026-10-02: Phase 1.5 also gives every other button except the layout buttons a
  function: launchers on Record and the Marker buttons, R plays / pauses its column's app,
  and R and S on an input column become cough and talk-over. None of it is uncertain like
  the on-screen display, so it ships with the media buttons in `0.2.0`.
- 2026-10-02: Phase 3 is no longer a GUI. It becomes *Setup and tray* (`0.5.0`): a
  terminal setup, `apptrol setup`, and a tray icon in the service; problem notifications
  join the on-screen display in Phase 1.6. See [ADR 0021](0021-no-desktop-gui.md).
