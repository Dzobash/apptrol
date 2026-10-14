# 0030. Three screen states: locked, switched away, logged out

- **Status:** Accepted
- **Date:** 2026-10-14

## Context

ADR 0024 treats a locked screen and a session that is not in front alike ("locked or
inactive"): launchers are blocked, everything else works. ADR 0029 then let the
controller go while another user's session is in front, so `inactive` is only seen at
the login screen with `at_login_screen = "keep"`.

The v0.2.1-rc3 hardware checklist showed that the two cases differ in what the user can
still hear (#60, H-49). On *Switch user* the user's session loses the speakers, and the
system pauses their players (the log shows `Paused` from the player, with no command
from Apptrol). Pressing ▶ or R at the login screen then starts playback that nobody hears:
the player reports `Playing`, the speakers stay silent. At the lock screen the user's
session keeps the speakers, so ▶ and R do what is expected.

Logind tells the three cases apart, with the properties ADR 0024 already follows:

```text
                     your session        speakers   logind
1. screen locked     in front, locked    yours      LockedHint true, Active true
2. Switch user       in the background   not yours  Active false
3. logged out        gone                not yours  no graphical session
```

*Switch user* does not log the user out: the session, its apps and Apptrol keep running
in the background. What reaches Apptrol depends on who is in front:

```text
Switch user ──► login screen in front   keep:    state 2 (this ADR)
                                        release: Apptrol lets the controller go
            ──► another user in front   Apptrol lets the controller go (ADR 0029)
switch back ──► this user in front      everything works again
```

Options for state 2:

- **A.** Keep ADR 0024: everything but launchers works. The media buttons play into
  nothing, as seen in H-49.
- **B.** Block the media buttons as well (chosen): what changes audio that can be heard
  and changed back at once (volume, mute, solo, the microphone buttons) keeps working;
  what starts something unheard (playback, launchers) does not.
- **C.** Block everything: `at_login_screen = "release"` already does that.

## Decision

1. **Screen locked** (`locked`): unchanged from ADR 0024. Everything works except
   launchers; a launcher with `when_locked = true` starts.
2. **Switched away** (`inactive`, the login screen in front with
   `at_login_screen = "keep"`): sliders, knobs, M and S work, as do the microphone
   buttons (ADR 0020). The media buttons (▶, ■, ◀◀, ▶▶) and R in mode `play_pause` send
   nothing to players. Volume and mute changes are applied, but are heard only after
   switching back. Launchers are blocked as in ADR 0024, and `when_locked = true` still
   lets one start: the session runs in the background, so the app or command starts
   there, which suits what `when_locked` is for (a script that acts outside the screen).
   With another user's session in front, Apptrol holds no controller (ADR 0029), so
   none of this applies.
3. **Logged out**: the user's service manager stops Apptrol with the last session, as
   on every stop (LEDs off, solo ended, state saved). With lingering on
   (`loginctl enable-linger`), the service manager and Apptrol keep running; the login
   screen then acts as `at_login_screen` says, and with no graphical session the state
   is `unknown` (launchers blocked, everything else works). Nothing changes for this
   case, so ADR 0029's Apptrol started over SSH keeps working.
4. A blocked press is logged as a warning, as LAUNCH-15 does for launchers, so the user
   sees after switching back what was pressed. It replaces the general
   `button pressed while the screen is locked` record for that press, so each press
   gives one line.
5. While the state is `unknown`, the media buttons and R work: playing into nothing is
   harmless, unlike a launcher, so ADR 0024's "fail closed" stays for launchers only.
6. Apptrol does not save whether music plays; that is the player's state. After
   switching back, a player paused by the system stays paused until ▶ or R is pressed.
7. Chosen per kind of button, not configurable. Whether controls can be allowed or
   blocked per app or per button while locked (#60's first question) is not decided
   here.

### Logging

8. Following ADR 0016; logged by the mixer, which decides:

   | Level | Message | Attributes |
   |---|---|---|
   | info | `media buttons blocked: this session is not in front` | `apptrol.screen.state=inactive` (component `mixer`, once per change) |
   | info | `media buttons allowed: this session is in front` | `apptrol.screen.state` (component `mixer`, once per change) |
   | warn | `media button pressed while this session is not in front; nothing sent` | `apptrol.button` (`▶`, `■`, `◀◀`, `▶▶`, `R1`–`R8`), `apptrol.screen.state=inactive` (component `mixer`) |

   No new attributes or error types. Switching to the login screen, pressing ▶ and M2,
   and switching back reads:

   ```text
   INFO launchers blocked: the screen is not known to be unlocked apptrol.component=session apptrol.screen.state=inactive
   INFO media buttons blocked: this session is not in front apptrol.component=mixer apptrol.screen.state=inactive
   WARN media button pressed while this session is not in front; nothing sent apptrol.component=mixer apptrol.button=▶ apptrol.screen.state=inactive
   WARN button pressed while the screen is locked apptrol.component=mixer apptrol.button=M2 apptrol.screen.state=inactive
   INFO launchers allowed: the screen is unlocked apptrol.component=session apptrol.session.id=<id>
   INFO media buttons allowed: this session is in front apptrol.component=mixer apptrol.screen.state=unlocked
   ```

## Consequences

- At the login screen, ▶ and R no longer start playback nobody hears; volume and the
  microphone still work there.
- Replaces ADR 0024's "everything other than launchers works whatever the screen state"
  for `inactive`; ADR 0024 gets a dated note.
- New requirement for the media buttons and R at `inactive`; LAUNCH-15 changes its
  wording. Checklist row H-49 changes. The mixer already receives the screen state
  (`mixer.ScreenChanged`), so no adapter changes.
- #60 stays open for the per-app or per-button choice.
- Planned for 0.3.0.
