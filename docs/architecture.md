# Apptrol — Architecture

How the service is put together. Decisions and their reasons are in
[ADR 0015](adr/0015-service-architecture.md); requirements in [requirements.md](requirements.md).

## Overview

```
 nanoKONTROL2 ──► controller ──┐                          ┌──► audio ──────► PipeWire
  (raw MIDI)      (decode MIDI,│       events   ┌───────┐ │    (volume, mute)
                   drive LEDs) ├───────────────►│ mixer │─┤
 config.toml ───► config ──────┤                │ (pure │ ├──► controller ─► LEDs
                  (load,       │◄───────────────│ logic)│ │
                   validate,   │     actions    └───────┘ └──► state ──────► state.json
                   watch)      │
 PipeWire ──────► audio ───────┘
  (streams appear / disappear)
```

- **`mixer`** holds all behaviour: assignments, mute, solo, LED states, what a new stream
  gets. It receives events and returns actions. It does no I/O, so it is tested completely
  with plain unit tests.
- **Adapters** (`controller`, `audio`, `config`, `state`, `logging`) talk to the outside
  world. The service uses the controller and the audio server through two small
  interfaces (`service.Controller`, `service.Audio`); tests use in-memory fakes (QA-06).
- **`service`** runs one event loop that owns all state: it takes events from the adapters,
  passes them to the mixer, and carries out the returned actions. One goroutine owns the
  state, so there are no data races by design.

## Packages

| Package | Responsibility |
|---|---|
| `cmd/apptrol` | Command line: `run`, `list`, `check`, `test`, `version`; wires everything together |
| `internal/service` | Event loop; the `Controller` and `Audio` interfaces; turns mixer actions into adapter calls; config reload; shutdown |
| `internal/mixer` | Core logic (pure): matching, positions, `max_volume`, user mutes, solo, LED computation |
| `internal/controller` | MIDI decoding; the nanoKONTROL2 CC/LED map, including which buttons have LEDs |
| `internal/controller/rawmidi` | Linux raw MIDI backend: discovery by sound card id, plug/unplug, read/write |
| `internal/audio/pulse` | PulseAudio-protocol backend for PipeWire (`pipewire-pulse`); reconnects; `apptrol list` data |
| `internal/config` | TOML loading, validation (including overlap warnings), file watching |
| `internal/state` | Saved state: JSON, atomic, batched writes |
| `internal/logging` | Log outputs (journald, rotating file) and formats; keeps every record on one line |
| `internal/logattr` | Names of log attributes and error types, following the OpenTelemetry conventions (ADR 0016); explained for users in [`logging.md`](logging.md) |
| `internal/version` | Build information |
| `examples` | The example configuration, built into the binary for the first start (CFG-09) |

Dependencies point inwards: adapters and `service` import `mixer`'s types; `mixer` imports
nothing from the adapters.

## Event flow

1. An adapter produces an event: *slider 3 moved to 90*, *M pressed on column 2*,
   *stream 57 (Spotify) appeared*, *config reloaded*, *controller connected*.
2. The service passes it to `mixer.Handle(event)`.
3. The mixer updates its model and returns actions: *set stream 57 to 71 %*, *mute
   input "GoXLR"*, *LED M2 on*, *state changed*.
4. The service executes them. Failures are logged; they never stop the loop.

Bursts of slider events are coalesced before step 2 (CTRL-07): the loop reads every event
already waiting, and of several positions of one control only the last is handled.

The configuration file is checked once a second; after a change the loop reloads it
(CFG-06). On SIGTERM or Ctrl+C the loop saves the state (including the solo, which the next
start restores), ends solo and turns the LEDs off before the adapters are stopped (SVC-05,
STATE-04, SVC-07, LED-08).

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
- `config`, `state`: unit tests and fuzzing on malformed input.
