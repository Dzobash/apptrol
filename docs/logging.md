# Reading Apptrol's logs

Apptrol writes what it does to the log: when it starts, which app it found for which
control, every button you press, and anything that goes wrong. This page explains how to
read those lines and how to find the ones you need. Where the log goes and how detailed it
is is set in the [configuration](config.md#log).

## Where to look

```bash
journalctl --user -u apptrol -f            # follow the log of the running service
journalctl --user -u apptrol -p warning    # only warnings and errors
journalctl --user -u apptrol --since today
```

If you turned on the file output, the log is also in `~/.local/state/apptrol/apptrol.log`
(JSON by default). Started by hand in a terminal (`apptrol`), Apptrol prints the same
lines, in colour and with timestamps.

## What a line looks like

Every line is one event, on one line. It starts with the level and a short message that
says what happened; after that come *attributes*, `name=value` pairs with the details.

```text
INFO muted apptrol.component=mixer apptrol.layout=default apptrol.control=slider2 apptrol.app.id=browser apptrol.app.name=Browser apptrol.app.type=app apptrol.button=M2
```

Read it as: *the mixer muted the app "Browser" (id `browser`, an app, not an input) on
slider 2 of the default layout, because you pressed M on column 2.*

The same line in the JSON log file:

```json
{"time":"2026-09-30T13:45:56.412+02:00","level":"info","msg":"muted","apptrol.component":"mixer","apptrol.layout":"default","apptrol.control":"slider2","apptrol.app.id":"browser","apptrol.app.name":"Browser","apptrol.app.type":"app","apptrol.button":"M2"}
```

### Levels

| Level | Shown by default | What you see |
|---|---|---|
| `error` | yes | Something failed: an invalid configuration, the audio server is gone, the controller cannot be opened. |
| `warn` | yes | Something needs your attention: the controller is not plugged in, an app in the configuration matches nothing. |
| `info` | no (`level = "info"`, or `apptrol --log-level info` for one run) | What Apptrol does: start and stop, configuration loaded, apps found, mute and solo. |
| `debug` | no (`level = "debug"`, or `apptrol --log-level debug` for one run) | Every volume change, every MIDI message, buttons that have no function. |

### Which part of Apptrol wrote it

Every line has `apptrol.component`:

| Component | Covers |
|---|---|
| `service` | Starting and stopping |
| `config` | The configuration file: loading, reloading, problems |
| `state` | The saved positions, mutes and solo |
| `audio` | The connection to PipeWire |
| `controller` | The nanoKONTROL2: found, connected, unplugged, MIDI messages |
| `mixer` | What your sliders, knobs and buttons do: volume, mute, solo, matching apps |

### Errors

A line about an error has two more attributes: `error.type`, a fixed word for the kind of
error, and `exception.message`, the details.

```text
ERRO the changed configuration is invalid; keeping the current settings apptrol.component=config file.path=/home/you/.config/apptrol/config.toml error.type=config_invalid exception.message="apps.y.type: \"bogus\" is not valid (use \"app\" or \"input\")"
ERRO the changed configuration is invalid; keeping the current settings apptrol.component=config file.path=/home/you/.config/apptrol/config.toml error.type=config_invalid exception.message="apps.z.match: missing; list at least one name fragment"
```

A configuration with several problems gives one line per problem.

| `error.type` | Meaning |
|---|---|
| `config_invalid` | The configuration has a problem; `exception.message` names the setting. |
| `config_unreadable` | The configuration file cannot be read (permissions?). |
| `config_removed` | The configuration file was deleted; Apptrol keeps the current settings. |
| `example_not_created` | On first start, the example configuration could not be written. |
| `log_setup_failed` | A log output could not be set up (for example the log file's folder). |
| `state_unreadable` | The saved state is damaged; Apptrol starts without it. |
| `state_not_saved` | The state could not be written. |
| `audio_server_unreachable` | PipeWire (`pipewire-pulse`) is not running or not reachable. |
| `audio_connection_lost` | The connection to PipeWire broke; Apptrol reconnects by itself. |
| `audio_change_failed` | PipeWire refused a volume or mute change. |
| `controller_busy` | Another program holds the controller; `fuser <device>` names it. |
| `controller_permission_denied` | You may not open the controller (see the README on permissions). |
| `controller_open_failed` | The controller could not be opened for another reason. |
| `led_failed` | An LED could not be set (debug level). |

## Examples

Pressing buttons (level `info`; the last three at `debug`):

```text
INFO solo on apptrol.component=mixer apptrol.layout=default apptrol.control=slider1 apptrol.app.id=spotify apptrol.app.name=Spotify apptrol.app.type=app apptrol.button=S1
INFO solo moved apptrol.component=mixer apptrol.layout=default apptrol.control=slider2 apptrol.app.id=browser apptrol.app.name=Browser apptrol.app.type=app apptrol.button=S2 apptrol.solo.previous_control=slider1 apptrol.solo.previous_app=Spotify
INFO solo off apptrol.component=mixer apptrol.layout=default apptrol.control=slider2 apptrol.app.id=browser apptrol.app.name=Browser apptrol.app.type=app apptrol.button=S2
INFO muted apptrol.component=mixer apptrol.layout=default apptrol.control=slider8 apptrol.app.id=mic apptrol.app.name=Microphone apptrol.app.type=input apptrol.button=M8
DEBU button has no function: solo needs an app on this column apptrol.component=mixer apptrol.layout=default apptrol.control=slider8 apptrol.app.id=mic apptrol.app.name=Microphone apptrol.app.type=input apptrol.button=S8
DEBU button has no function: no app on this column apptrol.component=mixer apptrol.layout=default apptrol.control=slider5 apptrol.button=M5
DEBU button has no function yet apptrol.component=mixer apptrol.button=cycle
```

Muted or unmuted somewhere else, e.g. in the desktop's volume applet (Apptrol follows):

```text
INFO unmuted outside Apptrol apptrol.component=mixer apptrol.layout=default apptrol.control=slider1 apptrol.app.id=spotify apptrol.app.name=Spotify apptrol.app.type=app apptrol.stream.id=10 apptrol.stream.app_name=Spotify
INFO unmuted outside Apptrol, but solo keeps it silent apptrol.component=mixer apptrol.layout=default apptrol.control=slider2 apptrol.app.id=browser apptrol.app.name=Browser apptrol.app.type=app apptrol.stream.id=11 apptrol.stream.app_name=Firefox process.executable.name=firefox
```

An app starts playing and is found:

```text
INFO stream matched apptrol.component=mixer apptrol.layout=default apptrol.control=slider2 apptrol.app.id=browser apptrol.app.name=Browser apptrol.app.type=app apptrol.stream.id=11 apptrol.stream.app_name=Firefox process.executable.name=firefox
```

A slider moves (level `debug`):

```text
DEBU volume changed apptrol.component=mixer apptrol.layout=default apptrol.control=slider1 apptrol.app.id=spotify apptrol.app.name=Spotify apptrol.app.type=app apptrol.volume_percent=71
```

Problems with the controller and PipeWire:

```text
WARN controller not found; waiting for it to be plugged in apptrol.component=controller apptrol.controller.port=nanoKONTROL2 apptrol.controller.sound_cards="PCH, GoXLRMini"
ERRO cannot connect to the audio server; retrying apptrol.component=audio error.type=audio_server_unreachable exception.message="connecting to the audio server: dial unix /run/user/1000/pulse/native: connect: no such file or directory"
```

## Finding lines

```bash
# everything about the controller
journalctl --user -u apptrol | grep apptrol.component=controller
# everything about one slider
journalctl --user -u apptrol | grep apptrol.control=slider3
# every configuration problem
journalctl --user -u apptrol | grep error.type=config_invalid
# with the JSON log file and jq: all errors, one per line
jq -c 'select(.level == "error") | {time, msg, "error.type"}' ~/.local/state/apptrol/apptrol.log
```

## Collecting the logs

To send Apptrol's logs to a log store such as Grafana Loki, Elasticsearch or OpenSearch,
collect the **log file in JSON** (`outputs = ["journald", "file"]`; JSON is the file's
default format). Every line is one JSON object; timestamps have milliseconds, so events
within one second keep their order. The file is rotated by renaming (`apptrol.log` →
`apptrol.log.1`), which the usual collectors follow.

Values keep their type: numbers such as `apptrol.volume_percent` are JSON numbers, and each
attribute always has the same type. Attribute names are flat keys with dots in them
(`"apptrol.app.name"`), as OpenTelemetry writes them; no name is the start of another, so
stores that turn dots into nested objects (Elasticsearch) can do so without conflicts.

- **OpenTelemetry Collector** (`filelog` receiver with a `json_parser` operator): the names
  already follow the OpenTelemetry conventions and can be kept as they are.
- **Grafana Loki** (Grafana Alloy or Promtail): parse with `| json`, which turns the dots
  into underscores (`apptrol_component`, `error_type`). Use only fields with few values as
  labels — `level`, `apptrol_component`, `error_type` — and filter on the others in the
  query, e.g. `{job="apptrol"} | json | apptrol_control="slider2"`.
- **Elasticsearch / OpenSearch** (Filebeat with the `ndjson` parser, or Fluentd/Fluent Bit
  with a JSON parser): `apptrol.app.name` becomes the field `apptrol` → `app` → `name`.

Collecting from the journal works too, but there each line is text (or logfmt with
`[log.journald] format = "logfmt"`) that has to be parsed again.

## All attributes

Names follow the [OpenTelemetry semantic conventions](https://opentelemetry.io/docs/concepts/semantic-conventions/),
a common standard for log and monitoring data: where the standard has a name, Apptrol uses
it; everything else starts with `apptrol.`. The reasons are in
[ADR 0016](adr/0016-log-records-follow-opentelemetry.md).

From the OpenTelemetry conventions:

| Attribute | Meaning |
|---|---|
| `error.type` | The kind of error, see [Errors](#errors) |
| `exception.message` | The error's details |
| `file.path` | A file Apptrol reads or writes |
| `url.full` | A link to help |
| `service.name` | `apptrol` (on the start line) |
| `service.version` | Apptrol's version (on the start line) |
| `process.executable.name` | The program behind a stream, if the app reports it |

Apptrol's own:

| Attribute | Meaning |
|---|---|
| `apptrol.component` | Which part of Apptrol wrote the line |
| `apptrol.layout` | The active layout (`default`) |
| `apptrol.control` | A slider or knob: `slider1` … `knob8` |
| `apptrol.button` | A pressed button: `M3`, `S1`, `R2`, or a transport button such as `cycle` |
| `apptrol.app.id` | The app's id in the configuration (`[apps.<id>]`) |
| `apptrol.app.name` | The app's `name` in the configuration |
| `apptrol.app.type` | `app` or `input` |
| `apptrol.app.match` | The app's `match` list |
| `apptrol.solo.previous_control` | Where solo was before it moved |
| `apptrol.solo.previous_app` | The app solo moved away from |
| `apptrol.volume_percent` | A volume, in percent (100 = normal) |
| `apptrol.wanted_percent` | The volume Apptrol set, when PipeWire changed it back |
| `apptrol.muted` | A mute state |
| `apptrol.wanted_muted` | The mute state Apptrol set, when PipeWire changed it back |
| `apptrol.action` | A change that could not be made |
| `apptrol.led` | An LED, e.g. `S1` |
| `apptrol.stream.id` | PipeWire's number for a playing stream |
| `apptrol.stream.app_name` | The name the app gives its stream (what `match` compares) |
| `apptrol.stream.corked` | Whether the app has paused the stream (`true`) or it plays (`false`) |
| `apptrol.device.name` | An input device's unique name |
| `apptrol.device.description` | An input device's readable name |
| `apptrol.device.matches` | All input devices that match |
| `apptrol.audio.server` | The audio server and its version |
| `apptrol.retry.delay_s` | Seconds until Apptrol tries again |
| `apptrol.controller.port` | The controller's sound card id from the configuration |
| `apptrol.controller.port_in_use` | The sound card id in use until restart |
| `apptrol.controller.device` | The controller's device file, e.g. `/dev/snd/midiC1D0` |
| `apptrol.controller.sound_cards` | The sound cards there are, when the controller is not found |
| `apptrol.controller.reason` | Why the controller disconnected, e.g. `unplugged` |
| `apptrol.midi.controller` | A MIDI message's controller number |
| `apptrol.midi.value` | A MIDI message's value |
| `apptrol.midi.channel` | A MIDI message's channel (1–16) |
| `apptrol.config.controls` | How many controls the configuration assigns |
| `apptrol.warning` | A warning about the configuration or the saved state |
| `apptrol.state.positions` | How many positions were restored |
| `apptrol.state.mutes` | How many mutes were restored |
| `apptrol.state.solo` | The restored solo, e.g. `slider1` |
| `apptrol.log.level` | The log level set with `--log-level` (on the start line) |
| `apptrol.log.level_source` | Where the log level comes from: `flag` (`--log-level`) or `config` (on the start line) |
| `apptrol.held.reason` | Why a held button (cough, talk-over, hold-to-talk) ended without being released: `controller_disconnected`, `config_changed` or `stopping` |
| `apptrol.talk_over_percent` | The volume apps go down to during talk-over |
