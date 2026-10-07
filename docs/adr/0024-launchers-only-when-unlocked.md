# 0024. Launchers start apps only while the screen is unlocked, unless opted in

- **Status:** Accepted
- **Date:** 2026-10-04

## Context

Launcher buttons (ADR 0019) start apps and run commands with the user's rights. The lock
screen does not stop the controller: it guards the screen and the keyboard, not what a
program in the background does with a USB device. So while the screen is locked, anyone
at the desk can press R1 five times, and the app or command starts five times, behind
the lock screen, where nobody sees it. The denylist (ADR 0022) catches only catastrophic
commands; a user's own script can still do harm when run at the wrong moment.

Sliders, knobs, mute, solo, the media keys and the microphone buttons change audio that
can be heard and changed back at once. People want them while the screen is locked
(turn the music down, mute the microphone when walking away from a call); desktops show
media controls on their lock screens for the same reason.

Launchers ship for the first time in 0.2.0. A safety default added later would break
the habits of those who already rely on the unsafe behaviour, so it comes with them.

Questions and options:

- **Which actions are blocked?** Only launchers (chosen); or everything (also volume and
  the microphone, which people want while locked).
- **Can a launcher be allowed while locked?** **A.** Never, no setting. **B.** Opt-in
  per launcher, `when_locked = true` (chosen). Few launchers make sense while locked: an
  app opens unseen behind the lock screen. Some do: a script that acts outside the
  screen, such as turning the lights off. Those are the riskiest kind, so the opt-in is
  per button, and every use of it is a warning.
- **What if Apptrol cannot tell whether the screen is locked?** No system bus, no
  graphical session found (e.g. Apptrol started over SSH), a lost connection, or before
  the first answer. Fail open (launchers work, a warning) or **fail closed** (chosen):
  launchers do nothing, and no setting changes that, not even `when_locked`. Any doubt
  means no.
- **What is logged when the controller is used while locked?** Every button press, as a
  warning, so that the owner sees after unlocking what was touched (chosen). Sliders and
  knobs are left out: one movement sends dozens of messages, and they change only the
  volume. Nothing is reported while the state is unknown: then it is not known that
  anything happened at a lock screen, and on a setup without logind every press would
  be reported all day.
- **Where does the lock state come from?** **logind** (chosen): every graphical login
  session has `LockedHint`, which the lock screens of KDE Plasma and GNOME set, and
  `Active`, false while another session (another user) is in front. It is desktop-neutral
  and on the system bus Apptrol already listens to (ADR 0023). The alternative,
  `org.freedesktop.ScreenSaver` on the session bus, differs between desktops (GNOME uses
  its own name) and has no notion of another user's session.

## Decision

- Apptrol follows the user's graphical session in logind: the user's `Display` property
  names it; its `LockedHint` and `Active` properties say whether it is locked or in
  front. All three emit changes, so nothing is polled. The connection is the one of
  ADR 0023, in `internal/session`.
- When logind (re)starts on the bus (`NameOwnerChanged`), everything is read again: its
  objects may be new. A state that cannot be read is read again every 5 seconds;
  launchers stay blocked meanwhile.
- The screen state is one of: `unlocked`; `locked` (`LockedHint` true); `inactive`
  (`Active` false: another session is in front, e.g. after *Switch user*); `unknown`
  (no system bus, no graphical session, the connection lost, a property that cannot be
  read, or no answer yet). It is sent to the mixer as `mixer.ScreenChanged`. Apptrol
  starts in state `unknown`.
- A launcher starts its app while the screen is `unlocked` (LAUNCH-13). While it is
  `locked` or `inactive`, only a launcher with `when_locked = true` starts its app
  (LAUNCH-14). While it is `unknown`, no launcher starts, whatever the configuration. A
  launcher that does not start does not flash the Record LED.
- `when_locked` is a setting of a launcher in `[layouts.<name>.buttons]` (`true` or
  `false`, default `false`), and only of a launcher. Every configuration load and
  `apptrol check` warn about each launcher that has it.
- While the screen is `locked` or `inactive`, every press of a button on the controller
  is logged as a warning (LAUNCH-15): launchers with what they did, other buttons
  (M, S, R, transport) with the button. Releases, sliders and knobs are not reported.
  Everything other than launchers works whatever the screen state.

Logs:

| Level | Message | Attributes |
|---|---|---|
| info | `launchers allowed: the screen is unlocked` | `apptrol.session.id` |
| info | `launchers blocked: the screen is not known to be unlocked` | `apptrol.screen.state` (`locked`, `inactive`, `unknown`); with `unknown`, `apptrol.screen.reason` (`no_system_bus`, `system_bus_lost`, `no_graphical_session`, `unreadable`) |
| warn | `cannot read the screen lock state; launchers are blocked` | `error.type=screen_state_unreadable` |
| warn | `cannot connect to the system bus; launchers are blocked and LEDs are not sent again after sleep, retrying` | `error.type=system_bus_unreachable` |
| debug | `graphical session found` | `apptrol.session.id` |
| warn | `launcher pressed while the screen is locked; nothing started` | `apptrol.button`, `apptrol.screen.state`, `apptrol.launcher.desktop_id` or `apptrol.launcher.command` (component `mixer`) |
| warn | `launcher pressed while the screen is locked; started (when_locked)` | the same (component `mixer`); the launcher then logs the start as usual |
| warn | `button pressed while the screen is locked` | `apptrol.button`, `apptrol.screen.state` (component `mixer`) |
| info | `launcher not started: the screen is not known to be unlocked` | `apptrol.button`, `apptrol.screen.state=unknown` (component `mixer`) |
| warn | `configuration warning` | `apptrol.warning="layouts.default.buttons.record: when_locked = true: …"` (component `config`, on every load) |

The two `launchers …` records are written only when the state changes. A locked screen,
someone pressing M2 and R1, and the screen unlocked again read:

```text
INFO launchers blocked: the screen is not known to be unlocked apptrol.component=session apptrol.screen.state=locked
WARN button pressed while the screen is locked apptrol.component=mixer apptrol.button=M2 apptrol.screen.state=locked
WARN launcher pressed while the screen is locked; nothing started apptrol.component=mixer apptrol.button=R1 apptrol.screen.state=locked apptrol.launcher.desktop_id=discord
INFO launchers allowed: the screen is unlocked apptrol.component=session apptrol.session.id=<id>
```

## Consequences

- A press at the lock screen starts nothing unless the owner opted in for that button,
  and every press is in the log at the default level, `warn`.
- Lock screens that do not set `LockedHint` (some lock programs on minimal window
  manager setups) look unlocked to Apptrol, so launchers still work there and nothing is
  reported. It cannot be told apart from "unlocked"; README, *Known issues*.
- Where logind has no graphical session for the user (e.g. some setups started without
  a display manager), launchers never work, `when_locked` or not; the log says so with
  `apptrol.screen.reason=no_graphical_session`. README, *Known issues*.
- New requirements LAUNCH-13 to LAUNCH-15; checklist rows H-45 to H-47. The service
  tests' fake for `internal/session` reports `unlocked`, as tests of launchers need it.
- ADR 0023's warning about a missing system bus now also names the blocked launchers.

## Notes

- 2026-10-07: While another user's session is in front, Apptrol now releases the
  controller instead of keeping it with launchers blocked; "everything other than
  launchers works whatever the screen state" holds only while this user's session, or
  the login screen with `at_login_screen = "keep"`, is in front. See
  [ADR 0029](0029-controller-follows-the-user-in-front.md) (#78).
