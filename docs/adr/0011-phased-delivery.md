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
- 2026-10-07: Each phase has a pop-culture name next to its topic, a Goal and a Done
  when line; the roadmap also lists what is out of roadmap and the scope. The phases
  themselves are unchanged. See [ADR 0028](0028-docs-as-code-for-the-website.md).
- 2026-10-15: Phase 1.6 is split in two. Output volume and the separate configuration
  tables stay in Phase 1.6, now *These Go to Eleven* (`0.3.0`); the on-screen display,
  problem notifications and the LED language become Phase 1.7, *Show Me What You Got*
  (`0.4.0`). Output volume is fully designed and tested on the reference system, while
  the on-screen display needs a trial on GNOME first. Layouts move to `0.5.0`, setup and
  tray to `0.6.0`; mentions of "Phase 1.6" for the on-screen display in earlier ADRs now
  mean Phase 1.7.
- 2026-10-31: a new Phase 1.8, *We'll Do It Live!* (`0.5.0`), controls OBS over its
  built-in WebSocket server: recording, streaming and OBS's audio sources (#216). It
  comes after the LED language, whose patterns it uses, and before layouts, because OBS
  targets do not need layouts. It finishes the recording control that was in the
  backlog (#62). Layouts move to `0.6.0`, setup and tray to `0.7.0`; the phase numbers
  stay. Layouts also change: Cycle tapped alone pins a layout, and Cycle + S / M / R to
  jump to a layout is dropped.
