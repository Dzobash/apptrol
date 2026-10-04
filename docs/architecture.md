# Apptrol — Architecture

How the service is put together. Decisions and their reasons are in
[ADR 0015](adr/0015-service-architecture.md); requirements in [requirements.md](requirements.md).

## Overview

```
 nanoKONTROL2 ──► controller ──┐                          ┌──► audio ──────► PipeWire
  (raw MIDI)      (decode MIDI,│                          │    (volume, mute)
                   drive LEDs) │       events   ┌───────┐ ├──► controller ─► LEDs
 config.toml ───► config ──────┼───────────────►│ mixer │─┤
                  (load,       │                │ (pure │ ├──► desktop ────► media players
                   validate,   │◄───────────────│ logic)│ │    (play, pause, next …)
                   watch)      │     actions    └───────┘ ├──► launcher ───► systemd
 PipeWire ──────► audio ───────┤                          │    (start an app in its own unit)
  (streams appear / go)        │                          └──► state ──────► state.json
 session bus ───► desktop ─────┤
  (media players appear / go,  │
   playing or paused)          │
 system bus ────► session ─────┘
  (the computer woke up,
   the screen locked / unlocked)
```

- **`mixer`** holds all behaviour: assignments, mute, solo, the held microphone buttons,
  which media player belongs to which control, what R, the media keys and the launcher
  buttons do, and every LED. It receives events and returns actions. It does no I/O and
  has no clock, so it is tested completely with plain unit tests.
- **Adapters** (`controller`, `audio`, `desktop`, `session`, `launcher`, `config`, `state`,
  `logging`) talk to the outside world. The service uses them through small interfaces
  (`service.Controller`, `service.Audio`, `service.Desktop`, `service.Power`,
  `service.Launcher`); tests use
  in-memory fakes (QA-06).
- **`service`** runs one event loop that owns all state: it takes events from the adapters,
  passes them to the mixer, and carries out the returned actions. One goroutine owns the
  state, so there are no data races by design. Actions that can take long (a media
  player's answer, systemd starting an app) are handed to their adapter without waiting,
  so the controller never stalls.

## Packages

| Package | Responsibility |
|---|---|
| `cmd/apptrol` | Command line: `run`, `list`, `check`, `test`, `version`; wires everything together |
| `internal/service` | Event loop; the `Controller`, `Audio`, `Desktop`, `Power` and `Launcher` interfaces; turns mixer actions into adapter calls; the Record LED's flash (the mixer has no clock); config reload and its warnings; shutdown |
| `internal/mixer` | Core logic (pure): matching streams, inputs and media players to controls, positions, `max_volume`, user mutes, solo, held microphone buttons and talk-over, R, media keys, launcher presses, LED computation |
| `internal/controller` | MIDI decoding; the nanoKONTROL2 CC/LED map, including which buttons have LEDs |
| `internal/controller/rawmidi` | Linux raw MIDI backend: discovery by sound card id, plug/unplug, read/write |
| `internal/audio/pulse` | PulseAudio-protocol backend for PipeWire (`pipewire-pulse`); reconnects; `apptrol list` data |
| `internal/desktop` | D-Bus session bus (`godbus`, ADR 0017): finds MPRIS media players and follows them, sends them commands; reconnects; never starts a service |
| `internal/session` | logind on the D-Bus system bus (`godbus`, ADR 0023, ADR 0024): reports each wake-up, repeated while the controller starts, and whether the user's graphical session is unlocked, locked, behind another session or unknown; reconnects |
| `internal/launcher` | Installed apps from their desktop files (XDG folders, `Exec` parsing) for `apptrol list apps`; starts launcher apps through systemd (`go-systemd`, ADR 0019) |
| `internal/config` | TOML loading, validation (including button rules, blocked commands and overlap warnings), file watching |
| `internal/state` | Saved state: JSON, atomic, batched writes |
| `internal/logging` | Log outputs (journald, rotating file) and formats; keeps every record on one line |
| `internal/logattr` | Names of log attributes and error types, following the OpenTelemetry conventions (ADR 0016); explained for users in [`logging.md`](logging.md) |
| `internal/version` | Build information |
| `examples` | The example configuration, built into the binary for the first start (CFG-09) |

Dependencies point inwards: adapters and `service` import `mixer`'s types; `mixer` imports
nothing from the adapters.

## Event flow

1. An adapter produces an event: *slider 3 moved to 90*, *M pressed on column 2*,
   *S released on column 8*, *stream 57 (Spotify) appeared*, *media player Spotify now
   playing*, *config reloaded*, *controller connected*, *controller disconnected*,
   *the computer woke up*, *the screen is locked*.
   Releases and disconnects only matter for buttons that act while held (INPUT-*,
   ADR 0020).
2. The service passes it to `mixer.Handle(event)`.
3. The mixer updates its model and returns actions: *set stream 57 to 71 %*, *mute
   input "GoXLR"*, *LED M2 on*, *pause the Spotify player*, *start OBS*, *state changed*.
4. The service executes them. Failures are logged; they never stop the loop. Player
   commands and app starts are only handed over: their adapters log the outcome later.

Bursts of slider events are coalesced before step 2 (CTRL-07): the loop reads every event
already waiting, and of several positions of one control only the last is handled.

The configuration file is checked once a second; after a change the loop reloads it
(CFG-06). On SIGTERM or Ctrl+C the loop saves the state (including the solo, which the next
start restores), ends solo and turns the LEDs off before the adapters are stopped (SVC-05,
STATE-04, SVC-07, LED-08).

**An invalid input never causes a write** ([ADR 0025](adr/0025-state-kept-while-config-invalid.md)).
An invalid file on reload is rejected and the running settings stay. Without a valid
configuration at start, the mixer is created with `mixer.NewWithoutConfig`: it controls
nothing and keeps the saved state unchanged, so its snapshot equals what is on disk and
the saver writes nothing (apart from positions of controls moved meanwhile). The first
valid configuration applies the kept state like a normal start (STATE-08).

## Controller access

Raw MIDI (`/dev/snd/midiC<card>D<device>`), in pure Go:

- **Discovery:** the card whose `/proc/asound/card<N>/id` matches `[controller] port`
  (default `nanoKONTROL2`) — never a fixed card number, which can change.
- **Hot-plug:** while the controller is away, look for it once a second (a few file reads;
  no measurable CPU). Unplugging ends the pending read with an error.
- **Input:** read and decode MIDI Control Change messages (running status included).
- **Output:** Control Change messages to set LEDs (LED mode *External*), on the MIDI
  channel the controller sends on. Track ◀ ▶ and the three Marker buttons have no LED.
  LED messages are sent at least 2 ms apart: the controller drops some of a burst.
- **After a connect** the controller is still starting up for a moment and ignores LED
  messages. The full LED state is therefore sent at once, and again after 0.5 s and 2 s
  (LED-07).
- **Exclusive:** raw MIDI allows one reader. While Apptrol runs, other programs cannot use
  the controller, and if another program holds it, Apptrol logs a clear error and retries.

Checked on the reference system (Kubuntu, PipeWire 1.6): the controller appears as card 1,
`hw:1,0,0`, and is not held by PipeWire.

## Audio access

The PulseAudio protocol, served by `pipewire-pulse` (ADR 0003), through a pure-Go client:

- Playback streams ("sink inputs") and capture devices ("sources") are listed on connect
  and tracked through subscription events.
- An app matches a stream when a configured fragment is found in `application.name` or
  `application.process.binary`; some apps (Spotify) only report the name.
- Mute changes made by others (e.g. the desktop's volume applet) are reported to the mixer,
  which takes them over (MUTE-07). A change within a second of Apptrol's own, or while the
  new-stream guard is active, counts as Apptrol's.
- Volume is set per stream and per input; mute likewise. The percentage is the one desktop
  mixers (KDE, GNOME, pavucontrol) show, so a slider at 50 % shows 50 % there. The top of
  a control is the app's `max_volume` (default 100 %, up to 150 %). All channels get the
  same volume.
- Monitors of outputs ("Monitor of GoXLR…") are not capture devices and are never matched.
- WirePlumber restores an app's remembered volume shortly after the app starts. When that
  lands after Apptrol has set the slider's position, Apptrol sets it again: for 3 seconds
  after a stream or input appears, changes to what Apptrol set are undone (PRIO-03).
- On connection loss the backend reconnects and the service re-applies the current state.
- PipeWire (WirePlumber) remembers each app's volume and mute and restores them when the
  app starts again — also when Apptrol is not running. Apptrol therefore sets the mute of
  every stream it controls explicitly, and ends solo on shutdown (SVC-07) so no app stays
  silenced by it.

## Desktop access

The D-Bus session bus, through `godbus` (ADR 0017, ADR 0018):

- **Connecting:** the address from `DBUS_SESSION_BUS_ADDRESS`, or the standard socket
  `$XDG_RUNTIME_DIR/bus`. Apptrol never starts a bus, and calls players with
  `FlagNoAutoStart`, so it never starts a service by accident (DESK-03). Without a bus,
  everything except media players keeps working; Apptrol reconnects (DESK-02).
- **Media players:** every running `org.mpris.MediaPlayer2.*` name, followed through
  `NameOwnerChanged` and `PropertiesChanged` (`PlaybackStatus`). The adapter only reports
  them; the mixer ignores players on other devices, proxies and duplicates, and matches
  the rest to controls through the apps' `match` lists (MEDIA-02, MEDIA-03).
- **Commands** (`Play`, `Pause`, `Stop`, `Next`, `Previous`) are sent without waiting;
  a player that refuses is logged when its answer arrives.
- **LEDs from players:** R and ▶ follow the players' `PlaybackStatus`, which arrives
  milliseconds after a command, not the audio stream, which apps pause only seconds
  later (ADR 0018, notes).

## Sleep and wake-up

While the computer sleeps (suspend or hibernation), its USB ports lose power. The
controller starts again on wake-up with every LED off, but the kernel keeps the device and
Apptrol's open raw MIDI file: no disconnect is seen, so the re-send after a connect
(LED-07) does not run, and the mixer, which sends only changed LEDs, would leave them dark.

- **Hearing the wake-up:** `internal/session` connects to the system bus
  (`DBUS_SYSTEM_BUS_ADDRESS`, or `/run/dbus/system_bus_socket`) and subscribes to
  `org.freedesktop.login1.Manager.PrepareForSleep` from logind: `true` before sleep,
  `false` after waking. It arrives whether or not the screen is locked; a wake-up continues
  the same login session (ADR 0023).
- **Sending the LEDs:** after a wake-up the adapter sends `mixer.SystemResumed` at once and
  after 0.5, 2 and 5 seconds, while the controller starts; going to sleep again stops the
  repeats. The mixer sends every LED for each one (LED-09).
- **Its own connection:** separate from the session bus connection, so either can be lost
  without the other. Without a system bus Apptrol logs a warning and reconnects;
  everything else works.

## The lock screen

The lock screen guards the screen and the keyboard, not the controller: Apptrol keeps
receiving it. Launchers therefore start apps only while the screen is unlocked
(LAUNCH-13 to LAUNCH-15, [ADR 0024](adr/0024-launchers-only-when-unlocked.md)):

- **Lock state:** `internal/session` asks logind for the user (`GetUser`), follows the
  user's `Display` (the graphical session) and that session's `LockedHint` and `Active`
  through `PropertiesChanged`, and reads everything again when logind (re)starts
  (`NameOwnerChanged`) or, while a read fails, every 5 seconds. It reports `unlocked`,
  `locked`, `inactive` (another session in front) or `unknown` as `mixer.ScreenChanged`,
  only on a change.
- **Fail closed:** the mixer starts in `unknown`. Without a system bus, without a
  graphical session, after a lost connection or a failed read, the state is `unknown`,
  and no launcher starts, whatever the configuration.
- **Deciding:** every launcher press goes through one place in the mixer (`launch`). It
  starts the app when the screen is `unlocked`, or when it is `locked` or `inactive` and
  the launcher has `when_locked = true`.
- **Audit:** while `locked` or `inactive`, every button press is a warning, launchers
  with what they did. Sliders and knobs are not reported; nothing is reported while
  `unknown`.

## Starting apps

Launcher buttons (ADR 0019, LAUNCH-*):

- **Finding apps:** desktop files in `applications/` under `$XDG_DATA_HOME` and each
  `$XDG_DATA_DIRS` entry, read on every press, so an app installed later is found. Their
  `Exec` line is parsed as the Desktop Entry specification says.
- **Starting:** the systemd user manager (`go-systemd`, over the existing session bus)
  starts each app in a transient unit `app-apptrol-<id>@<random>.service`, with
  `Type=exec` (a program that cannot run fails the start), `ExitType=cgroup` (a wrapper
  that exits does not end the app) and `CollectMode=inactive-or-failed` (finished units
  are removed). The app is never a child of Apptrol and outlives it. An app with only
  D-Bus activation is started through `org.freedesktop.Application.Activate`.
- **Commands** are argument lists run without a shell; `~/` is expanded.
- **Already running** (`if_running = "skip"`), checked on the press: active units with
  the desktop ID in their name, in both spellings (as written and escaped), then the
  user's processes in `/proc` by program name. Steam games are passed to Steam
  unchecked; wrappers (`flatpak`, shells, …) are looked for only by unit.
- **The connection** to the systemd user manager is opened on the first press and kept;
  it is not tied to that press, and after an error the next press connects again.

## Safety of launcher commands

A launcher `command` runs exactly what the user configured, with the user's rights; the
README and docs/config.md say it is the user's responsibility. Validation refuses a short
list of catastrophic commands (LAUNCH-12, [ADR 0022](adr/0022-launcher-command-safety.md), "Blocked commands" in docs/config.md): deleting
everything, wiping a disk, fork bombs, running a download, changing rights on everything,
and `sudo`, `su` or `doas`, which need a terminal. This is a safety net against
copy-paste accidents, not a security boundary: plain argument lists are checked
reliably, but text for a shell (`sh -c`) can always be disguised. Desktop IDs come from
installed packages and are not checked; apps that need root ask for the password in a
dialog.

## Files at runtime

| Path | Contents |
|---|---|
| `~/.config/apptrol/config.toml` | Configuration (watched for changes) |
| `~/.local/state/apptrol/state.json` | Positions and user mutes |
| `~/.local/state/apptrol/apptrol.log` | Log file, when the file output is on |

## Testing

- `mixer`: table-driven tests per requirement ID against a simulated audio server and
  controller, plus a fuzz test over random event sequences — the bulk of the tests.
- `service`: the whole loop against fake adapters: config reload, state, shutdown.
- `controller`: MIDI decoding and the CC map with byte-level tests and fuzzing.
  `controller/rawmidi`: discovery, plug/unplug and busy devices against fake devices; the
  real controller is checked by hand (`apptrol test`, docs/testing.md).
- `audio/pulse`: integration tests against a headless PipeWire in CI.
- `desktop`: integration tests in a private session bus with fake media players, locally
  (`make test-desktop`) and in CI; a bus restart and a missing bus included.
- `session`: the same private bus stands in for the system bus, with a fake logind that
  sends the sleep signal and offers a user and a graphical session: wake-ups and their
  repeats, a signal from another program, lock, unlock, another session in front, no
  graphical session, an unknown user, an unreadable session, logind starting late, a bus
  restart; a missing bus without one. The mixer's fuzz test checks that no launcher ever
  starts in a state that does not allow it.
- `launcher`: desktop files and `Exec` parsing against temporary folders, with fuzzing;
  starting apps against a fake systemd, and against the real user manager with harmless
  units (`make test-launcher`, not in CI, which has no user systemd).
- `config`, `state`: unit tests and fuzzing on malformed input; the blocked-command check
  is fuzzed too.
