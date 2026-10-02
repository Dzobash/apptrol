# 0021. No desktop GUI: a terminal setup and a tray icon in the service

- **Status:** Accepted
- **Date:** 2026-10-02

## Context

Phase 3 planned a separate desktop program: a window that looks like the controller, a
tray icon, and a toolkit chosen from a Fyne prototype. It would talk to the service over
D-Bus (Q-4). Looking at what such a program is for:

- **Feedback** while using the controller is the on-screen display (Phase 1.6). It
  belongs in the service, which makes every change and knows it first.
- **Status** (is Apptrol running, is something wrong) needs no window: a tray icon and a
  notification when there is a problem show it.
- **Setup** is what is left: assigning apps and buttons without a text editor.
- Desktop toolkits such as Fyne need **cgo** (OpenGL). That breaks ADR 0013: a pure Go,
  static binary.
- A program that stores its own settings **on top of** the configuration file creates two
  truths: a saved change in `config.toml` that does nothing because the GUI value wins,
  and an `apptrol check` that no longer shows what is active.

## Decision

1. **There is no separate desktop GUI program.**
2. **`apptrol setup`** is a terminal interface in the same binary:
   - It shows the layout as the controller's columns, and the buttons' settings.
   - Apps are picked from what is playing now (the data of `apptrol list`), so no match
     fragments have to be guessed.
   - Every change is checked with the rules of `apptrol check` before it is saved.
   - It edits `config.toml`, and the running service reloads it (CFG-06). **The file stays
     the only truth** (Q-6): it can still be edited by hand, and the setup shows what is
     in it.
   - Saving keeps the user's comments and layout. How is decided when it is built; if a
     file cannot be written without losing them, the setup says so before writing.
   - Libraries (all MIT, pure Go, from Charm, whose `log` Apptrol already uses):
     **bubbletea** (the framework), **bubbles** (list with filtering, table, key help),
     **huh** (forms, e.g. a button's mode, and a first-run wizard), **lipgloss** (layout
     and colours) and **x/exp/teatest** for tests without a terminal (QA-06).
   - For the documentation: **vhs** records the setup as a GIF for the README, kept up to
     date in CI with `vhs-action`; **freeze** makes pictures of terminal output.
3. **A tray icon in the service** shows status only:
   - normal: running, controller connected, configuration valid
   - attention: the tooltip names the problem, e.g. an invalid configuration
   - no icon: Apptrol is not running
   - A small menu: open the configuration file, show the log, and *Set up…*, which opens
     `apptrol setup` in a terminal.
   - It is a StatusNotifierItem on the shared session bus connection (ADR 0017), pure Go.
     It appears when the desktop's panel does (followed on D-Bus, as the media players
     are). KDE shows it; GNOME with the AppIndicator extension; elsewhere there is no icon,
     and nothing depends on it.
4. **Notifications for problems** (e.g. an invalid configuration, the controller
   unplugged) come from the service with the on-screen display in Phase 1.6.
5. **Man page and shell completions** for the packages come later, written without a
   command-line framework (no cobra, no fang): the command line stays on Go's `flag`
   package.

Each of these gets its own record with a logging table (ADR 0018, point 9) when it is
built; this record sets the direction.

## Consequences

- The binary stays pure Go and static (ADR 0013).
- Q-4 (how the GUI talks to the service) no longer applies: there is no GUI program. The
  service uses D-Bus for the media players, the on-screen display, notifications and the
  tray icon.
- Q-6 (where settings live when they can be changed outside the file) is answered: in
  `config.toml` only.
- Phase 3 becomes *Setup and tray* (`0.5.0`). The Phase 3 requirements outline, the
  roadmap and ADR 0011 change with it; ADR 0017 gets a note.
- The tray icon uses the 16 px mark from the brand guide (ADR 0014).
- Users who never open a terminal get no window. If that turns out to be needed, it is a
  new decision, with the cgo question attached.
