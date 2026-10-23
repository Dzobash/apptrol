# Apptrol — Architecture

How the service is put together. Decisions and their reasons are in
[ADR 0015](adr/0015-service-architecture.md); requirements in [requirements.md](requirements.md).

## Overview

```
 nanoKONTROL2 ──► controller ──┐                          ┌──► audio ──────► PipeWire
  (raw MIDI)      (decode MIDI,│                          │    (volume and mute, including outputs)
                   drive LEDs) │       events   ┌───────┐ ├──► controller ─► LEDs
 config.toml ───► config ──────┼───────────────►│ mixer │─┤
                  (load,       │                │ (pure │ ├──► desktop ────► media players
                   validate,   │◄───────────────│ logic)│ │    (play, pause, next …)
                   watch)      │     actions    └───────┘ ├──► launcher ───► systemd
 PipeWire ──────► audio ───────┤                          │    (start an app in its own unit)
  (streams, inputs and         │                          └──► state ──────► state.json
   outputs appear / go)        │
 session bus ───► desktop ─────┤
  (media players appear / go,  │
   playing or paused)          │
 system bus ────► session ─────┘
  (the computer woke up,
   the screen locked / unlocked)
```

- **`mixer`** holds all behaviour: assignments, mute, solo, the held microphone buttons,
  which output device each output control sets (and that nothing else touches it),
  which media player belongs to which control, what R, the media keys and the launcher
  buttons do (and what they may do in each screen state), and every LED. It receives events and returns actions. It does no I/O and
  has no clock, so it is tested completely with plain unit tests.
- **Adapters** (`controller`, `audio`, `desktop`, `session`, `launcher`, `systemd`,
  `config`, `state`, `logging`) talk to the outside world. The service uses them through
  small interfaces (`service.Controller`, `service.Audio`, `service.Desktop`,
  `service.Power`, `service.Launcher`, `service.Status`); tests use
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
| `internal/service` | Event loop; the `Controller`, `Audio`, `Desktop`, `Power`, `Launcher` and `Status` interfaces; turns mixer actions into adapter calls; keeps the Status line up to date; the Record LED's flash (the mixer has no clock); config reload and its warnings, each logged by its text or with its own message and attributes; shutdown |
| `internal/mixer` | Core logic (pure): matching streams, inputs, output devices and media players to controls, output volume, positions, `max_volume`, user mutes, solo, held microphone buttons and talk-over, R, media keys, launcher presses, what is blocked in each screen state, LED computation |
| `internal/controller` | MIDI decoding; the nanoKONTROL2 CC/LED map, including which buttons have LEDs |
| `internal/controller/rawmidi` | Linux raw MIDI backend: discovery by sound card id, plug/unplug, read/write; lets go of the controller and takes it back (ADR 0029) |
| `internal/audio/pulse` | PulseAudio-protocol backend for PipeWire (`pipewire-pulse`): playback streams, capture devices and output devices, their volume and mute; the new-device guard; reconnects; `apptrol list` data |
| `internal/desktop` | D-Bus session bus (`godbus`, ADR 0017): finds MPRIS media players and follows them, sends them commands; reconnects; never starts a service |
| `internal/session` | logind on the D-Bus system bus (`godbus`, ADR 0023, ADR 0024): reports each wake-up, repeated while the controller starts, whether the user's graphical session is unlocked, locked, behind another session or unknown, and whose session is in front at the seat (ADR 0029); reconnects |
| `internal/launcher` | Installed apps from their desktop files (XDG folders, `Exec` parsing) for `apptrol list apps`; starts launcher apps through systemd (`go-systemd`, ADR 0019) |
| `internal/systemd` | The unit's Status line: sends `STATUS=` to systemd through go-systemd's `daemon` package (pure Go, no `sdjournal`, ADR 0017); sends nothing without `NOTIFY_SOCKET` (SVC-13, ADR 0007) |
| `internal/config` | TOML loading: `[apps]`, `[inputs]` and `[outputs]` into one map of targets, output selector validation and monitor warnings (CFG-20, CFG-21), the old `type` form with a warning (ADR 0026); validation (including button rules, blocked commands and overlap warnings), its problems as `config.Problem`; warnings as `config.Warning`; file watching |
| `internal/state` | Saved state: JSON, atomic, batched writes |
| `internal/logging` | Log outputs (journald, rotating file) and formats; keeps every record on one line |
| `internal/logattr` | Names of log attributes and error types, following the OpenTelemetry conventions (ADR 0016); explained for users in [`log-attributes.md`](log-attributes.md) |
| `internal/version` | Build information |
| `examples` | The example configuration, built into the binary for the first start (CFG-09) |

Dependencies point inwards: adapters and `service` import `mixer`'s types; `mixer` imports
nothing from the adapters.

The portable packages are `mixer`, `config`, `state`, `controller` (without its
`rawmidi` backend), `logattr` and `version`. `make check` and CI's existing Build job
cross-compile them for Windows without cgo (NFR-11, ADR 0013). OS-specific code stays
inside adapters; settings express user intent, such as `at_login_screen`, while the
session adapter obtains the state from logind on Linux. The service still imports
Linux adapters, and logging detects journald through Linux file metadata; neither
is part of this guard. Windows adapters and path changes remain in #92.

## Event flow

1. An adapter produces an event: *slider 3 moved to 90*, *M pressed on column 2*,
   *S released on column 8*, *stream 57 (Spotify) appeared*, *output "USB Headset"
   appeared*, *media player Spotify now playing*, *config reloaded*, *controller connected*, *controller disconnected*,
   *the computer woke up*, *the screen is locked*.
   Releases and disconnects only matter for buttons that act while held (INPUT-*,
   ADR 0020).
2. The service passes it to `mixer.Handle(event)`.
3. The mixer updates its model and returns actions: *set stream 57 to 71 %*, *mute
   input "GoXLR"*, *set output "USB Headset" to 50 %*, *mute an output*, *LED M2 on*, *pause the Spotify
   player*, *start OBS*, *state changed*.
4. The service executes them. Failures are logged; they never stop the loop. Player
   commands and app starts are only handed over: their adapters log the outcome later.

Bursts of slider events are coalesced before step 2 (CTRL-07): the loop reads every event
already waiting, and of several positions of one control only the last is handled.

The configuration file is checked once a second; after a change the loop reloads it
(CFG-06). File metadata is compared before contents are read:

```
Linux:         inode + size + modification time ──► changed? ──► settle, read
Other systems:        size + modification time ──► changed? ──► settle, read
```

Linux retains the inode check, including replacements with equal size and time.
Other systems use only size and modification time and can miss a replacement
preserving both (NFR-11, ADR 0013). After a file it
could not read, the watcher reads it again every second until it can: fixing the
permissions changes none of those (#120). On SIGTERM or Ctrl+C the loop saves the state (including the solo, which the next
start restores), ends solo and turns the LEDs off before the adapters are stopped (SVC-05,
STATE-04, SVC-07, LED-08).

**An invalid input never causes a write** ([ADR 0025](adr/0025-state-kept-while-config-invalid.md)).
An invalid file on reload is rejected and the running settings stay. The shared config
validator identifies app, input and output settings misplaced under a layout and names
the table they belong in (`[apps.<id>]`, `[inputs.<id>]`, `[outputs.<id>]`); reload errors
and `apptrol check` use the same diagnostic (CFG-08, CFG-11). Without a valid configuration at start, the mixer is created with `mixer.NewWithoutConfig`: it controls
nothing and keeps the saved state unchanged, so its snapshot equals what is on disk and
the saver writes nothing (apart from positions of controls moved meanwhile). The first
valid configuration applies the kept state like a normal start (STATE-08).

## Configuration

`internal/config` turns the file into one `Config`. Apps, inputs and outputs have their
own tables (CFG-18, [ADR 0026](adr/0026-separate-tables-for-apps-inputs-outputs.md)), but are read
into **one map of targets**, `Config.Apps`, each with its `Type`: layouts and `[media]
player` name a target by its id alone, so an id may be in only one table, and the mixer
can keep looking up targets by id. A fixed output becomes a `mixer.Output` target in
`Config.Setup()`; the mixer chooses its device and sets its volume (see
[Output volume](#output-volume)). No adapter interface is new: output volume goes
through `service.Audio` like every other volume.

```
[apps.spotify]              ─┐
[inputs.mic]                ─┤
[outputs.main]              ─┼─► Config.Apps[id]  TypeApp / TypeInput / TypeOutput
[apps.old] type = "input"    ─┘                    TypeInput + warning
```

The config API for outputs (CFG-20, ADR 0027) is the existing `config.App`:

- `Type == config.TypeOutput` (`"output"`) identifies entries of `[outputs]`.
- `Device == config.DeviceDefault` (`"default"`) selects the server's default output;
  `Match` is empty in that case. Otherwise `Device` is empty and `Match` is nonempty.
- `Name` defaults to the id; `MaxVolume` defaults to 100 and accepts 1–150.
- `Config.Apps[id]`, sorted `Config.AppIDs()`, `Config.Layout()` and
  `Layout.Assignment(control)` work for every target. There is no separate outputs map.

The parser checks key presence, so an empty `match` alongside `device` is still an
error. `type`, `talk_over_volume`, media-player pinning and R's `play_pause` cannot make
an output behave like an input or app. `apptrol check` displays either the match list
or `device = "default"`. The first-control-wins overlap warning covers apps, inputs
and fixed outputs. Runtime output matching also catches overlaps between different
fragments that resolve to the same device ([ADR 0035](adr/0035-first-control-owns-matched-output.md)).

`Config.Setup()` passes a fixed output (`match`) as `mixer.Output`, never as an app, so
its match list is compared with output devices only, never with playback streams
(OUT-01). An output with `device = "default"` is still left out, with its assignment,
until following the default is built (#85, OUT-03); it gets the fixed warning
`following the default output is not implemented yet; this target is inactive`, with
`apptrol.app.id` and `apptrol.app.type=output`, at every load and in `apptrol check`
(ADR 0026 implementation note and its 2026-10-20 note). The parsed config stays intact.

```
[outputs.speakers] match = [...]   ──► mixer.Output     (sets the device's volume)
[outputs.main] device = "default"  ──► left out + warning, until #85 builds it
```

The old form (`[apps.<id>]` with `type`) is read until 1.0 (CFG-19). Each such entry adds
a `config.Warning`: a warning has a text for people, printed by `apptrol check`, and
optionally a fixed log message with attributes, so it can be searched for in the log
(ADR 0016). Warnings without one, such as an unused app, are logged by the service as
`configuration warning` with the text in `apptrol.warning`:

```
apptrol check   warning: apps.mic: type = "input" is the old form; move it to [inputs.mic]
log             WARN config uses an old form; move the entry to its own table … apptrol.app.id=mic apptrol.config.table=inputs
```

Monitor fragments on inputs (including the old form) and outputs add a warning with
ADR 0026's fixed message and `apptrol.app.id` / `apptrol.app.match`. The service logs it
at every startup and reload with `apptrol.component=config`; `apptrol check` prints its
text. Matching is case-insensitive, so the suffix check is too; whitespace is trimmed
as for any fragment. The configuration remains valid (CFG-21).

An invalid configuration is a `config.ValidationError`, a list of `config.Problem`s. A
problem works the same way: a text for people, and optionally attributes. The service
logs one record per problem under the fixed invalid-configuration message, with the text
in `exception.message` and the attributes added. Only an id defined in two tables has
attributes so far (CFG-18, ADR 0026 point 7):

```
apptrol check   - inputs.mic: the same id is defined in two tables ([apps] and [inputs])
log             ERRO the changed configuration is invalid; keeping the current settings … apptrol.app.id=mic error.type=config_invalid exception.message="inputs.mic: the same id …"
```

## Output volume

An output target names one output device ([ADR 0027](adr/0027-output-volume.md)). The
mixer keeps the output devices the audio adapter reports (`AudioSnapshot.Outputs`,
`OutputAdded`, `OutputRemoved`) and, for each output target on a control, the device it
has chosen (`outputFor`):

- **Choosing** (`chooseOutputs`, OUT-02): the current device while it still matches,
  otherwise the first match by name, case-insensitive on name or description. It shares
  `pickDevice` with inputs (CFG-05); nothing else of the input code is shared, so solo,
  talk-over and held buttons, which only look at apps and inputs, can never reach an
  output. Choosing runs after a snapshot, a device that appears, changes or goes, and a
  reload.
- **Volume** (`SetOutputVolume`, its own action, not `SetDeviceVolume`): when the
  control moves, and when a device is newly chosen while the control's position is
  known (OUT-05). An unknown position leaves the device alone (PRIO-04); an output
  moved to another control or given another `max_volume` gets its volume again on a
  reload, as inputs do. Outside changes are not reverted (PRIO-02): the adapter does not
  even report an output's volume.
- **Releasing** (`ReleaseOutput`, CTRL-06): a device that no output target holds any
  more after choosing (a reload took its control away or changed its match) is released;
  one another control still holds is kept, one that is gone has nothing to release. The
  adapter's new-output guard then stops defending Apptrol's volume on it, so a volume
  changed elsewhere within the guard's 3 seconds stays. Output volume writes and the
  guard take turns (`wire`, as mute writes do), so the guard never writes a position
  the slider has already left.

  ```
  reload: slider7 no longer on "main" ──► nobody holds Speakers ──► ReleaseOutput Speakers
                                                                     └─► the guard lets it go
  ```

  The mixer logs each release once at info (`output released; no control holds the
  device`), so the log says why an outside volume now stays.
- **Keeping** (`KeepOutput`, OUT-05, CTRL-06): a read that sees an output new, or with a new name
  or description, is not defended by the adapter at once, because a match on the
  description may have ended. The mixer decides: a device a control still holds
  is kept, even when it also gets `SetOutputVolume`, and the adapter checks the latest
  read against its guard without waiting for another change; one no control holds is
  released instead, and its volume stays.

  ```
  read: "Speakers Pro", 100 % ──► defense paused ──► OutputAdded ──► mixer
     still matches ──► KeepOutput    ──► guard: 100 % ≠ 30 % ──► 30 % again
     no longer     ──► ReleaseOutput ──► guard lets it go     ──► 100 % stays
  ```

  At debug, the adapter logs each read of an output (`output device read`, with
  `apptrol.audio.guard.state`) and the server's answer to each volume it sets.
  The server answers once it has passed a write on, not once the device has it, and
  PipeWire 1.0 can lose a write to a brand-new device without reporting anything. So
  within the guard's window, each accepted write schedules a read of the output
  200 ms later; `session` takes it from the `recheck` queue in turn with the change
  notifications (under `wire`) and defends it like a reported volume change (#166).

  ```
  write 30 % ─► success ─► (lost) ─► 200 ms: read 100 % ─► 30 % again ─► 200 ms: read 30 %
  ```

  Every reported output identity, including snapshots, carries an adapter-generated
  `Device.Generation`. The pure mixer only echoes it in `KeepOutput` or `ReleaseOutput`.
  The adapter accepts only the newest generation's answer; until then, subsequent
  volume-only notifications update the last read but cannot trigger a guard write
  (`ownership_pending` at debug). Such notifications do not report a new identity or
  advance its generation. A volume action does not acknowledge ownership, but a write
  the server accepted becomes the guard's last read, so the `KeepOutput` after the
  first volume of a new output does not set it twice. An ignored older answer is
  logged at debug with both generations. Generations
  are never reused on removal, resync or reconnect. All guard checks, ownership answers
  and output writes retain the `wire` then `mu` lock order; the adapter never waits
  for the mixer's answer.

  ```text
  rename (generation 2) -> OutputAdded(2) -> mixer answer buffered
  restored volume      -> ownership_pending -> no guard write
  KeepOutput(1)        -> stale answer ignored (logged: 1 vs 2)
  ReleaseOutput(2)     -> guard forgets its volume
  ```
- **Mute** (`SetOutputMute`, OUT-06): M toggles the owning slider's saved mute.
  `outputMuteSent` avoids redundant writes. `OutputMuteChanged` carries an outside
  change from the adapter; only the owner takes it over, including knobs. The service
  forwards output mute actions through `Audio.Apply`, as it does volume actions.
  Snapshot and re-plug apply the control's saved mute. Releasing the device unmutes it.
- **No solo** (OUT-07, ADR 0036): S on an output column does nothing and says so at
  debug; `toggleSolo` is never reached for it. App solo and talk-over never touch
  outputs (OUT-11): `effectiveMute` has no solo or talk-over rule for `Output`, so an
  output is muted only by its own saved mute. Next to app solo, a solo on outputs made
  everything too complicated: the two together could silence every app at once.
- **LEDs** (OUT-10): `ledStates` lights M on an output column while the slider's saved
  mute is on, from M or taken over from outside, as LED-02 does for apps; S and R stay
  off. A control whose device an earlier control owns (`blockedOutputs`) shows no mute,
  since it does not decide it (ADR 0035). `outputAdded` and `outputRemoved` sync the
  LEDs, because ownership, and with it M's LED, can move when a device comes or goes.

  ```text
            app column     output column    output column, earlier control owns device
  S         lit on solo    off              off
  M         saved mute     saved mute       off (its saved mute is kept for later)
  R         player state   off              off
  ```
- **Conflicting matches** (OUT-04, ADR 0035): each target first chooses a device using
  OUT-02. In control order (sliders then knobs), only the first target can claim that
  device. Later targets have no `outputFor` entry; `blockedOutputs` remembers their
  conflicts, ignores M, and limits warnings to conflict starts and reloads. The saved
  position still follows movement, ready for a later ownership transfer. Labels play
  no part in this decision. M's LED follows the same ownership (OUT-10).

  ```text
  M7 -> saved mute -> SetOutputMute -> audio adapter -> sink mute
  outside mute -> OutputMuteChanged -> owning control's saved mute
  ```

```
OutputAdded "USB Headset" ──► chooseOutputs ──► knob7 "phones" holds it (output matched)
                                             └─► knob7 position known? ──► SetOutputVolume 40 %
ControlMoved knob7 = 51   ──► SetOutputVolume { "alsa_output.usb-…", 0.40 } ──► audio ──► PipeWire
```

`apptrol list` asks the mixer for each output target's device (`OutputDevice`) and
shows the control in the output devices' CONTROL column (OUT-12).

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

- Playback streams ("sink inputs"), capture devices ("sources") and output devices
  ("sinks") are listed on connect and tracked through subscription events. Each kind
  arrives in the mixer as part of the snapshot after a (re)connect, then as single
  changes:

  ```
  connect ──► AudioSnapshot { Streams, Devices (inputs), Outputs }
  later   ──► StreamAdded / StreamRemoved    a playback stream
              DeviceAdded / DeviceRemoved    an input
              OutputAdded / OutputRemoved    an output (also when its description changes)
              OutputMuteChanged              an outside output mute (own echoes filtered)
  ```
- Output devices (OUT-12, [ADR 0027](adr/0027-output-volume.md)): the adapter reports
  each sink's unique name and description; a volume-only change is not an event.
  Mute changes are reported separately, including when the description changes in the
  same notification. The mute event comes first, so it belongs to the old owner;
  a newly matching control applies its saved mute afterwards. The mixer keeps devices
  and logs each one that no control acts on at debug
  (`output device not on a control`), so the log shows the exact name for a match list.
  `apptrol list` reads the default output from the server information and marks it
  with `*`.
- An app matches a stream when a configured fragment is found in `application.name` or
  `application.process.binary`; some apps (Spotify) only report the name.
- Mute changes made by others (e.g. the desktop's volume applet) are reported to the mixer,
  which takes them over (MUTE-07). Apptrol remembers the mute it set last: the first change
  to that value counts as Apptrol's own, a change to the other value as outside, even right
  after Apptrol's (so a hold-to-talk input unmuted then is muted again, INPUT-03). The
  expected value is dropped after a second, e.g. when the write failed. Mute writes and
  event handling take turns, so a mute is never compared with a newer expectation than the
  one it was read under. While the new-stream guard is active, every change counts as
  Apptrol's.

  ```
  Apptrol mutes ──► want = muted (1 s)
  change: muted   ──► Apptrol's own, want used up
  change: unmuted ──► outside ──► mixer takes it over (hold-to-talk: muted again)
  ```
- Volume is set per stream, per input and per output device (`SetSinkVolume` by the
  sink's name, OUT-01); mute per stream, per input and per output (`SetSinkMute`). The percentage is the one desktop
  mixers (KDE, GNOME, pavucontrol) show, so a slider at 50 % shows 50 % there. The top of
  a control is the target's `max_volume` (default 100 %, up to 150 %). All channels get
  the same volume, so an output's left/right balance is lost
  ([known issue](known-issues.md)). The adapter keeps each sink's channel count, as it
  does for streams and inputs.
- Monitors of outputs ("Monitor of GoXLR…") are not capture devices and are never
  matched. A monitor is a source, never a sink, so it is listed neither as an input nor
  as an output (OUT-12). The config validator warns about monitor fragments before the
  audio adapter is needed (CFG-21).
- WirePlumber restores an app's remembered volume shortly after the app starts, and an
  output's when it appears. When that lands after Apptrol has set the control's position,
  Apptrol sets it again: for 3 seconds after a stream, input or output appears, changes
  to what Apptrol set are undone (PRIO-03, OUT-05). For an output only the volume is
  defended. Mute uses the existing own-write attribution (`outMuteWant`) but no new
  guard: a late server mute restore can still be taken as an outside change. Extending
  that guard is a separate follow-up; full OUT-05 under that race remains pending.
  Output identity changes pause defense across notifications until the mixer answers
  the newest reported generation; see [Output volume](#output-volume).
  Volume writes and the guard take turns (`wire`, as mute writes do), so the guard never
  writes a position the control has moved away from in the meantime (#158, OUT-05):

  ```
  guard: reads 50 %, writes 100 % ─┐
  slider: 10 % ──── waits ─────────┴─► writes 10 % ─► guard defends 10 %
  ```
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
  At connect, the players already running are listed (`ListNames`, then `GetNameOwner`
  and their properties for each). Every call has its own 2-second timeout, so a player
  that does not answer is left out (a warning) without using up the time of the players
  after it (MEDIA-01, #119).
- **Commands** (`Play`, `Pause`, `Stop`, `Next`, `Previous`) are sent without waiting;
  a player that refuses is logged when its answer arrives.
- **LEDs from players:** R and ▶ follow the players' `PlaybackStatus`, which arrives
  milliseconds after a command, not the audio stream, which apps pause only seconds
  later (ADR 0018, notes). They go dark only after a player has stayed `Paused` for
  500 ms, so the 60–100 ms pauses a browser makes while you seek with the mouse do not
  blink them (MEDIA-09, ADR 0033). The mixer has no clock: a pause it did not ask for
  becomes the action `HoldPauseLED{BusName, Seq}`; the service runs a timer
  (`playerPauseDelay`) and sends `PlayerPauseSettled{BusName, Seq}` back through the
  event channel, and the mixer ignores it unless `Seq` names the player's running
  hold (a counter of the whole mixer, so a player that comes back under the same bus name
  never matches its predecessor's timer). A pause started by R or ▶ darkens the LEDs at once, and buttons always
  act on the player's real status; only the LEDs wait.

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
(LAUNCH-13 to LAUNCH-15, [ADR 0024](adr/0024-launchers-only-when-unlocked.md)). After
*Switch user* (`inactive`), the session has also lost the speakers, so the media buttons
send nothing either (MEDIA-11, [ADR 0030](adr/0030-three-screen-states.md)):

```text
                 sliders, knobs, M, S, mic   ▶ ■ ◀◀ ▶▶, R (play_pause)   launchers
unlocked         work                        work                        start
locked           work                        work                        only when_locked
inactive         work                        send nothing (warning)      only when_locked
unknown          work                        work                        never
```

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
- **Media buttons:** while `inactive`, `transportPressed` and `rPressed` check the state
  (`mediaBlocked`) before looking for a player, so a press sends nothing and is warned
  about even without a player. `locked` and `unknown` do not block them: the lock screen
  keeps the speakers, and playing into nothing is harmless. The mixer logs at info once
  when the state turns `inactive` and once when it leaves it (`mediaScreenChanged`).
  The ▶ and R LEDs still follow the players.
- **Audit:** while `locked` or `inactive`, every button press is one warning: launchers
  with what they did, media buttons while `inactive` with MEDIA-11's warning (so
  `lockedPress` skips them), other buttons with their name. Sliders and knobs are not
  reported; nothing is reported while `unknown`.

## Several users

A raw MIDI device has one reader at a time, and every user may run their own Apptrol. The
controller follows the person in front of the computer
([ADR 0029](adr/0029-controller-follows-the-user-in-front.md), SVC-08 to SVC-12):

- **Who is in front:** `internal/session` also reads logind's `seat0` (`GetSeat`), its
  `ActiveSession`, and that session's `User` and `Class`, on the same connection and
  signals as the lock state. It reports `this_user`, `other_user`, `login_screen` (class
  `greeter`), `nobody` or `unknown` as `mixer.SeatChanged`, only on a change. Reading the
  seat, not only the user's own session, keeps an SSH-only login from taking the
  controller and lets a session without a display manager (`startx`) keep it.
- **Deciding:** the mixer (`decideController`) lets go for `other_user`, and for
  `login_screen` with `[controller] at_login_screen = "release"`. With `keep`, the login
  screen changes nothing: whoever had the controller keeps it, so coming back from another
  user, it is taken only once this user is in front. Otherwise, also while `unknown`, it
  holds the controller. It decides again on every `SeatChanged` and after
  every configuration reload, and acts only on a change: it ends held states
  (`controller_released`), then returns `ReleaseController`, or `TakeController` when it
  ends.
- **Carrying it out:** the service turns every LED off first (`ledsOff`, as on shutdown),
  then calls the controller's `Release`, which closes the device and stops looking for
  it; a `Release` that overtakes a session before it connects is seen under the
  device's lock. `Take` looks again at once; the connect brings the LEDs back (LED-07).
  For 5 seconds after `Take`, "busy" (the other user's Apptrol has not let go yet) and
  "permission denied" (the system has not given this user the rights yet) are logged at
  debug and retried every 100 ms, as for permissions after plugging in (HW-08). The same
  holds for 5 seconds after Apptrol starts, as a second user's Apptrol starts with their
  login. The debug record names the moment in `apptrol.controller.grace` (`started`,
  `plugged_in`, `taken_back`); "busy" is not retried quietly after plugging in, as no
  other session can hold a controller just plugged in.

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

## The Status line

`systemctl --user status apptrol` shows a line that Apptrol keeps up to date (SVC-13,
[ADR 0007](adr/0007-systemd-user-service.md)). The mixer is not involved: the service
builds the text from what it already sees, and the `systemd` adapter sends it.

```
 controller events ─┐
 release / take ────┼──► service ──► text changed? ──► systemd adapter ─ ─► sender goroutine
 config (re)loaded ─┘      (version first, then the state)  (hands over,         │ newest text wins
 shutdown                                                    never waits)        ▼
                                                              sd_notify STATUS= ──► systemctl status:
                                                                                    Status: "v0.3.0 · …"
```

| State | Text after the version |
|---|---|
| No controller yet, or it was unplugged | `controller not found` |
| Controller connected | `controller connected · N controls assigned` |
| Another user's session in front | `controller released (another user in front)` |
| No valid configuration | `waiting for a valid configuration` |
| Shutdown has begun | `stopping` |

The first row that applies, from the bottom up, is shown: stopping beats everything,
then a released controller, then a missing configuration, then the controller.
Only a changed text is sent, so repeated "controller connected" events of the LED resend
cost nothing. Writing to systemd's socket has no time limit and blocks when systemd stops
reading, so the loop never writes: `SetStatus` hands the text to a sender goroutine and
returns at once, and if an earlier text has not gone out yet, only the newest does. At
shutdown the service calls `Flush`, which waits at most 200 ms for the last text
(`stopping`). Without `NOTIFY_SOCKET` (started in a terminal) the adapter sends nothing
and logs that once at debug, with `apptrol.component=systemd`. A failed send is a
warning (`error.type=status_not_sent`); Apptrol runs on. The unit has
`NotifyAccess=main` and stays `Type=simple`: systemd does not wait for a ready message.

## Files at runtime

| Path | Contents |
|---|---|
| `~/.config/apptrol/config.toml` | Configuration (watched for changes) |
| `~/.local/state/apptrol/state.json` | Positions and user mutes |
| `~/.local/state/apptrol/apptrol.log` | Log file, when the file output is on |

## Testing

- The portable Windows build runs in `make check` and CI's Build job (NFR-11).
  Config tests exercise the fallback metadata comparison on Linux as well, and
  verify Linux still detects an inode change with equal size and modification time.
- `mixer`: table-driven tests per requirement ID against a simulated audio server and
  controller, plus a fuzz test over random event sequences — the bulk of the tests. The
  simulated server keeps output volumes too; the fuzz test has outputs on slider 7 and
  knob 7 and checks that an output is set only while a target holds it, and
  that an output control changes no stream or input; a kept output is held by a target,
  a released one by none. Ownership tests check that reported generations are echoed
  unchanged in snapshot, change and reload answers.
- `service`: the whole loop against fake adapters: config reload, state, shutdown, a
  slider on an output reaching the audio adapter as `SetOutputVolume` and M as
  `SetOutputMute`; the Status line
  through every state, only changes sent, and the flush at shutdown (SVC-13).
  Output mute tests also cover persistence, outside mute on sliders and knobs,
  re-plug, conflicting matches and ownership transfer.
- `systemd`: the Status line reaches a unix datagram socket given as `NOTIFY_SOCKET`
  as `STATUS=…`; nothing is sent without one; a missing socket is a warning; a send that
  blocks, also on a real socket whose queue is full and unread, never holds up the
  caller, the newest text wins and `Flush` gives up after its time (SVC-13).
- `controller`: MIDI decoding and the CC map with byte-level tests and fuzzing.
  `controller/rawmidi`: discovery, plug/unplug and busy devices against fake devices; the
  real controller is checked by hand (`apptrol test`, docs/testing.md).
- `audio/pulse`: integration tests against a headless PipeWire in CI: streams, a test
  input and a silent test output that appear and go, the test output's volume set to
  30 % and read back, the new-output guard setting it back within its 3 seconds and
  leaving it after them, a first write the server accepts but loses being set again
  by the guard's read-back (#166), `apptrol list`'s data, and a reconnect. The
  read-back's cases that must not write (no guard, gone, new description, pending
  ownership, released, expired) are unit-tested through the `outputRead` hook. Turning sink
  notifications into events, and the new-output guard (a slider move during a guard
  write, release, keep after a new description), are unit-tested on their own, and so
  is a slider move during a new-stream or new-input guard's write (#158). Tests
  also force a second notification before a pending release, and stale ownership
  answers across renames and removal/reappearance, without sleeps (CTRL-06, QA-11).
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
- `packaging`: a test keeps the MIT text of the .deb's DEP-5 copyright file equal to
  `LICENSE` (NFR-09), and the unit's `Description=` equal to the AppStream summary, with
  `Type=simple` and `NotifyAccess=main` kept (SVC-13). CI's Packaging job builds the release as a snapshot and runs
  `lintian` on the .deb (NFR-05, NFR-08, NFR-09, #146); it is the only place CI runs
  GoReleaser before a release candidate is tagged.
- `test/container`: the developer-only runner checks the shipped binary and service
  unit against real systemd in rootless Ubuntu and Fedora. Its fixed mounts, limits,
  environment isolation and audit records are separate from the application service
  loop. `internal/repocheck` tests the runner with a fake Podman and keeps both
  agents' allow, ask and block lists equal (QA-15, QA-16, ADR 0034). See
  [rootless container checks](testing.md#rootless-container-checks).
- `config`, `state`: unit tests and fuzzing on malformed input; the blocked-command check
  is fuzzed too. Inputs are tested in both forms, `[inputs.<id>]` and the old `type`
  (CFG-18, CFG-19). Output tests cover selector presence, values, cross-table rules,
  layout references, CLI display and what `Setup` passes to the mixer (CFG-20, OUT-01). Monitor warnings are checked on load,
  in the CLI and on service startup/reload (CFG-21); documentation checks cover device
  discovery instructions (CFG-22). See [configuration cases](testing/cases/cfg.md).
