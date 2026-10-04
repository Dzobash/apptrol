# 0023. Send every LED again when the computer wakes up, told by logind

- **Status:** Accepted
- **Date:** 2026-10-04

## Context

After the computer wakes from sleep (suspend) or hibernation, the controller's LEDs were
dark, apart from the ones Apptrol happened to change afterwards. Seen on the owner's
system: after a night of sleep, only M8 came back on the microphone column when the
microphone's device reappeared; S8 and R8 stayed dark.

The cause, from the logs and a repeated test:

- While the computer sleeps, its USB ports lose power. The nanoKONTROL2 starts again on
  wake-up, and after a start all its LEDs are off.
- The kernel keeps the USB device and the open raw MIDI file across sleep. Apptrol's read
  goes on as if nothing happened: there is no `controller disconnected`, so the re-send
  of LED-07 after a reconnect does not run.
- The mixer sends only LEDs that changed (`syncLEDs`). When the microphone's device came
  back, M8 was sent; S8 and R8 had not changed as far as Apptrol knew.

Apptrol needs to learn that the computer woke up. Options:

- **A. logind's `PrepareForSleep` signal.** systemd-logind sends
  `org.freedesktop.login1.Manager.PrepareForSleep(true)` before sleep and `(false)`
  after waking, for suspend, hibernation and hybrid sleep, on the D-Bus **system bus**.
  Listening needs no rights. It is what other programs use (NetworkManager, ModemManager)
  and what ADR 0017 foresaw ("resume from suspend").
- **B. The same signal, but close and reopen the controller.** Reuses the connect path
  and its logs, but logs a disconnect that did not happen and ends held buttons.
- **C. Send every LED every 30 seconds.** No D-Bus, but LEDs can stay wrong for up to
  30 seconds, and Apptrol keeps writing MIDI that is almost never needed.
- **D. Watch USB with udev.** Does not see this case: the kernel does not remove and add
  the controller on wake-up.
- **E. Notice a jump in the clock.** Works without D-Bus, but guesses: a stopped process
  or a time change looks the same.

The signal arrives whether or not the screen is locked: a wake-up continues the same
login session, and logind sends it for the whole machine. A user service such as
Apptrol runs only while its user is logged in, so there is always a session to receive
it.

## Decision

Option A.

- A new adapter package, `internal/power`, connects to the system bus
  (`DBUS_SYSTEM_BUS_ADDRESS`, or `/run/dbus/system_bus_socket`) and subscribes to
  `PrepareForSleep` from `org.freedesktop.login1`. It is written on godbus directly, like
  `internal/desktop`; go-systemd's `login1` package would add nothing but a second way to
  connect.
- On `PrepareForSleep(false)` it sends the event `mixer.SystemResumed`, and again 0.5, 2
  and 5 seconds later: the controller starts again some seconds after the wake-up and
  ignores LED messages while it starts (as after plugging in, LED-07).
- The mixer handles `SystemResumed` by sending every LED (`syncLEDs` with force). The
  mixer stays pure (ADR 0015).
- Its own connection, not the session bus connection of `internal/desktop`: the buses
  are different, and each keeps working when the other is lost.
- Without a system bus, or when the connection is lost, Apptrol logs a **warning** and
  reconnects (waits from 0.5 up to 30 seconds, like `internal/desktop`). Everything works
  except the re-send after sleep; a warning, not an error, because nothing the user does
  is affected until the computer sleeps.

Logs (`apptrol.component=power`):

| Level | Message | Attributes |
|---|---|---|
| info | `connected to the system bus` | `apptrol.power.bus_address` |
| warn | `cannot connect to the system bus; LEDs are not sent again after sleep, retrying` | `error.type=power_bus_unreachable` |
| debug | `system bus still unreachable` | `error.type=power_bus_unreachable`, `apptrol.retry.delay_s` |
| warn | `lost the connection to the system bus; reconnecting` | `error.type=power_bus_lost` |
| debug | `system going to sleep` | |
| info | `system resumed; sending LEDs again` | |

So a wake-up reads:

```text
DEBU system going to sleep apptrol.component=power
INFO system resumed; sending LEDs again apptrol.component=power
```

## Consequences

- LEDs are right again within seconds of waking up, before the lock screen is unlocked.
- Apptrol now uses the system bus, read-only: it only receives a signal. No new library.
- LOG-10 gains the component `power`; `docs/logging.md` documents it and the two error
  types.
- New requirement LED-09; hardware checklist row H-44 (suspend and wake up).
- On a system without systemd-logind (e.g. elogind setups are compatible, other init
  systems may not be) nothing sends the signal; Apptrol works as before, and LEDs come
  back as soon as they change or the controller is replugged.

## Notes

- 2026-10-04: Renamed before the first release that contains it: the package
  `internal/power` is now `internal/session`, the log component `power` is now `session`,
  `apptrol.power.bus_address` is now `apptrol.session.bus_address`, and the error types
  `power_bus_unreachable` and `power_bus_lost` are now `system_bus_unreachable` and
  `system_bus_lost`. The same connection to logind now also follows the screen lock
  ([ADR 0024](0024-launchers-only-when-unlocked.md)), so "power" no longer described it.
