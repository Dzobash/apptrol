# 0029. The controller follows the user in front of the computer

- **Status:** Accepted
- **Date:** 2026-10-07

## Context

A raw MIDI device can be read by one program at a time. Apptrol runs as a user service,
so on a computer shared by two users there can be two Apptrols, each wanting the
controller (#78).

Today the first one keeps it. After *Switch user*, every press still acts on the first
user's apps and LEDs, although logind has given the speakers to the user now in front;
launchers are blocked and every press is logged as a warning (ADR 0024). The second
user's Apptrol logs `controller_busy` every second. ADR 0024 says "everything other than
launchers works whatever the screen state"; for another user in front, that is wrong.

Questions and options:

- **Who counts as "in front"?** **A.** Only the state of the user's own graphical
  session (`Active`), as ADR 0024 reads it. An Apptrol in a login without a screen (SSH
  only) has no graphical session: it would either never get the controller, or take it
  whenever it is free, while someone else sits at the desk. A setup without a display
  manager (started with `startx`) also has no graphical session in logind and would lose
  the controller. **B.** The seat (chosen): logind's `seat0` names the session in front
  (`ActiveSession`), with its user and class. Apptrol holds the controller unless a
  session of another user is in front. Alice at the desk: hers. Bob logged in over SSH
  while Alice is at the desk: Alice's. A `startx` session in front: its user's.
- **The login screen** (a session of class `greeter`, shown on *Switch user* or after the
  other user logs out). Keeping the controller there is right with one user: sliders keep
  working, and launchers are blocked anyway. With several users, someone at the login
  screen should not change the last user's audio. Both make sense, so it is a setting,
  `at_login_screen`, default `keep` (chosen); a fixed rule either way was the
  alternative.
- **The hand-over moment.** Switching back, the returning user's Apptrol may try to open
  the controller before the other one has closed it, and would log an error ("in use by
  another program"). As with plugging in (HW-08), "busy" is expected for a short time
  after the user comes back to the front (chosen); the alternative was one false error
  on every switch.

## Decision

1. **Apptrol holds the controller unless a session of another user is in front** at
   seat `seat0`: logind's `Seat.ActiveSession`, and that session's `User` and `Class`.
   Any session of the same user in front (graphical or not), and no session in front,
   count as "not another user". All emit changes, so nothing is polled; the connection
   is the one of ADR 0023 and ADR 0024, in `internal/session`.
2. **Releasing:** when another user's session comes to the front, the session watcher
   tells the mixer (`mixer.SeatChanged`); the mixer answers with every LED off and
   `ReleaseController`. The controller adapter closes the device and stops looking for
   it. Held states end as on unplugging (INPUT-07).
3. **Taking it back:** when no other user's session is in front any more, the mixer
   answers `TakeController`; the adapter looks for the controller at once (not after the
   usual second) and connects as after plugging it in, sending the LEDs again (LED-07).
4. **The login screen:** `[controller] at_login_screen` is `"keep"` (default) or
   `"release"`. With `keep`, a `greeter` session in front does not count as another
   user; with `release`, it does. A reload applies it at once. Any other value is
   rejected. The lock screen of one's own session is not the login screen: it keeps
   ADR 0024's behaviour.
5. **Hand-over:** for 5 seconds after taking the controller back, "busy" when opening it
   is logged at debug and retried every 100 ms; afterwards it is the error of HW-06.
6. **Without logind** (no system bus), or while the seat cannot be read, Apptrol holds the
   controller whenever it is free, as before. Only `seat0` is followed; computers with
   several seats are out of scope.
7. ADR 0024 stays for launchers and the lock screen. Its sentence "everything other than
   launchers works whatever the screen state" now applies while this user's session, or
   the login screen with `keep`, is in front; while another user's session is in front,
   this Apptrol does not hold the controller at all.
8. With this, each user runs their own Apptrol with their own configuration and saved
   state (`systemctl --user enable --now apptrol` once in their own login); the
   controller works for whoever is in front.

### Logging

9. Following ADR 0016; the decision is logged by the mixer with the reason, the device's
   side by the controller:

   | Level | Message | Attributes |
   |---|---|---|
   | info | `another user is in front; releasing the controller` | `apptrol.seat.front` = `other_user` or `login_screen`, `apptrol.session.id` (the session in front) (component `mixer`) |
   | info | `controller released` | `apptrol.controller.device` (component `controller`); `controller disconnected` is not logged for it |
   | info | `this user is in front again; taking the controller` | `apptrol.seat.front` = `this_user`, `nobody` or `login_screen` (component `mixer`) |
   | info | `login screen in front; keeping the controller` | `apptrol.seat.front=login_screen`, `apptrol.controller.at_login_screen=keep` (component `mixer`, once per change) |
   | debug | `controller still held by the other session; retrying` | `apptrol.controller.device`, `error.type=controller_busy` (component `controller`) |
   | warn | `cannot read who is in front; holding the controller as before` | `error.type=seat_unreadable` (component `session`) |
   | debug | `session in front changed` | `apptrol.seat.front`, `apptrol.session.id` (component `session`) |

   New attributes: `apptrol.seat.front`, `apptrol.controller.at_login_screen`. New error
   type: `seat_unreadable`. Switching to Bob and back reads:

   ```text
   INFO another user is in front; releasing the controller apptrol.component=mixer apptrol.seat.front=other_user apptrol.session.id=<id>
   INFO controller released apptrol.component=controller apptrol.controller.device=/dev/snd/midiC<n>D0
   INFO this user is in front again; taking the controller apptrol.component=mixer apptrol.seat.front=this_user
   DEBUG controller still held by the other session; retrying apptrol.component=controller error.type=controller_busy
   INFO controller connected apptrol.component=controller apptrol.controller.device=/dev/snd/midiC<n>D0
   ```

## Consequences

- Each user has their own Apptrol, configuration and state; the controller follows the
  person at the desk. The second user's Apptrol no longer logs `controller_busy` every
  second.
- With another user in front, presses never reach this Apptrol, so LAUNCH-15's warnings
  for `inactive` only appear with `at_login_screen = "keep"` at the login screen.
  LAUNCH-13 and LAUNCH-15 change their wording; SVC-03, LED-07 and INPUT-07 name the
  release.
- New requirements SVC-08 to SVC-12 and CFG-24; checklist row H-47 changes, rows H-49
  to H-51 are added.
- The mixer gets two actions and one event; the session watcher follows the seat as
  well as the user's session; the controller adapter can be told to release and take.
- An Apptrol started over SSH while nobody is logged in at the desk (the login screen in
  front, `keep`) takes the controller, and gives it up when someone logs in at the desk.
- Shipped in 0.2.1 although it adds a setting: it is part of the fix for #78, and the
  default changes nothing for one user.

## Notes

- 2026-10-07: Point 4 corrected after the v0.2.1-rc2 hardware checklist. "With `keep`,
  a `greeter` session in front does not count as another user" made an Apptrol that had
  let go for another user take the controller back as soon as that user switched to the
  login screen, before its own user was in front; opening it then failed with "permission
  denied" (the login screen's session had the rights) and was logged as an error. Now,
  with `keep`, the login screen changes nothing: the controller stays with whoever had it
  (`login screen in front; the controller stays with the other user`, info). Point 5 also
  covers "permission denied" right after taking it back: the system gives the user in
  front the rights a moment after logind reports them. Unit tests had covered only the
  way from this user to the login screen, not back from another user.
