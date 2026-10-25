# Reading Apptrol's logs

Apptrol writes what it does to the log: when it starts, which app it found for which
control, every button you press, and anything that goes wrong. This page explains how to
read those lines and how to find the ones you need. Where the log goes and how detailed it
is is set in the [configuration](config.md#log). Every attribute and every `error.type`
is listed in [Log attributes](log-attributes.md).

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
| `desktop` | The D-Bus session bus: connected, lost, and the media players found, gone or changing status (debug) |
| `session` | logind, the login manager, on the D-Bus system bus: connected, lost; the computer going to sleep (debug) and waking up, when every LED is sent again; the screen locked or unlocked, which blocks or allows launchers |
| `launcher` | Starting apps from launcher buttons: which app, in which systemd unit, and failures |
| `systemd` | The Status line of `systemctl --user status apptrol`: each text sent (debug), that nothing is sent when Apptrol was not started by systemd (debug, once), and a failed send (warning) |
| `mixer` | What your sliders, knobs and buttons do: volume, mute, solo, matching apps |

### Errors

A line about an error has two more attributes: `error.type`, a fixed word for the kind of
error, and `exception.message`, the details.

```text
ERRO the changed configuration is invalid; keeping the current settings apptrol.component=config file.path=/home/you/.config/apptrol/config.toml apptrol.app.id=mic error.type=config_invalid exception.message="inputs.mic: the same id is defined in two tables ([apps] and [inputs])"
ERRO the changed configuration is invalid; keeping the current settings apptrol.component=config file.path=/home/you/.config/apptrol/config.toml error.type=config_invalid exception.message="apps.z.match: missing; list at least one name fragment"
```

A configuration with several problems gives one line per problem. A problem about one
app, input or output can name it: an id defined in two target tables adds
`apptrol.app.id`, so `grep apptrol.app.id=mic` finds it (CFG-18, ADR 0026). A `type`
in `[apps]` that is not valid also adds `apptrol.config.table`, the table the entry
belongs in: `outputs` for `type = "output"`, `inputs` for anything else (CFG-19,
CFG-20):

```text
ERRO the changed configuration is invalid; keeping the current settings apptrol.component=config file.path=/home/you/.config/apptrol/config.toml apptrol.app.id=speakers apptrol.config.table=outputs error.type=config_invalid exception.message="apps.speakers.type: \"output\" is not valid; [apps] holds only apps, and outputs go in [outputs.speakers]"
```

Every `error.type` is listed in [Log attributes](log-attributes.md#errors).

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

The computer sleeps and wakes up; the controller lost power, so every LED is sent again,
and repeated three times while the controller starts up (the `DEBU` lines at `debug`):

```text
DEBU system going to sleep apptrol.component=session
INFO system resumed; sending LEDs again apptrol.component=session
DEBU LEDs sent again after waking up apptrol.component=session apptrol.session.resend=1
DEBU LEDs sent again after waking up apptrol.component=session apptrol.session.resend=2
DEBU LEDs sent again after waking up apptrol.component=session apptrol.session.resend=3
```

Someone uses the controller while the screen is locked. Every button press is a warning,
so you see it at the default level; sliders and knobs are not reported. A launcher starts
nothing, unless it has `when_locked = true`:

```text
INFO launchers blocked: the screen is not known to be unlocked apptrol.component=session apptrol.screen.state=locked
WARN button pressed while the screen is locked apptrol.component=mixer apptrol.button=M2 apptrol.screen.state=locked
WARN launcher pressed while the screen is locked; nothing started apptrol.component=mixer apptrol.button=R1 apptrol.screen.state=locked apptrol.launcher.desktop_id=discord
WARN launcher pressed while the screen is locked; started (when_locked) apptrol.component=mixer apptrol.button=● apptrol.screen.state=locked apptrol.launcher.command=~/bin/lights-off
INFO launchers allowed: the screen is unlocked apptrol.component=session apptrol.session.id=<id>
```

A browser pauses its video for a moment while you seek with the mouse. R and ▶ stay lit
for pauses shorter than 500 ms, and the debug log says so; a longer pause (buffering, or
the browser's own pause button) darkens them (MEDIA-09, ADR 0033):

```text
DEBU playback status changed apptrol.component=desktop apptrol.player.bus_name=org.mpris.MediaPlayer2.chromium.instance<pid> apptrol.player.status=Paused
DEBU playback status changed apptrol.component=desktop apptrol.player.bus_name=org.mpris.MediaPlayer2.chromium.instance<pid> apptrol.player.status=Playing
DEBU short pause not shown on the LEDs apptrol.component=mixer apptrol.player.bus_name=org.mpris.MediaPlayer2.chromium.instance<pid> apptrol.player.identity=Chrome
DEBU pause shown on the LEDs apptrol.component=mixer apptrol.player.bus_name=org.mpris.MediaPlayer2.chromium.instance<pid> apptrol.player.identity=Chrome
```

After *Switch user*, with the login screen in front and `at_login_screen = "keep"`, the
session has lost the speakers: the media buttons and R send nothing, and each press is one
warning instead of `button pressed while the screen is locked` (MEDIA-11, ADR 0030).
Pressing ▶ and M2 there, then switching back:

```text
INFO launchers blocked: the screen is not known to be unlocked apptrol.component=session apptrol.screen.state=inactive
INFO media buttons blocked: this session is not in front apptrol.component=mixer apptrol.screen.state=inactive
WARN media button pressed while this session is not in front; nothing sent apptrol.component=mixer apptrol.button=▶ apptrol.screen.state=inactive
WARN button pressed while the screen is locked apptrol.component=mixer apptrol.button=M2 apptrol.screen.state=inactive
INFO launchers allowed: the screen is unlocked apptrol.component=session apptrol.session.id=<id>
INFO media buttons allowed: this session is in front apptrol.component=mixer apptrol.screen.state=unlocked
```

To see only what happened while the screen was locked:

```bash
journalctl --user -u apptrol | grep -E 'while the screen is locked|not in front'
```

A configuration in an old form still loads, with one warning per entry at every load
(until 1.0; [configuration](config.md#the-old-form-with-type)). The warning names the entry and
the table to move it to. Monitor warnings also have their own message; other warnings carry their text in
`apptrol.warning`:

```text
WARN config uses an old form; move the entry to its own table apptrol.component=config file.path=/home/you/.config/apptrol/config.toml apptrol.app.id=mic apptrol.config.table=inputs
WARN configuration warning apptrol.component=config file.path=/home/you/.config/apptrol/config.toml apptrol.warning="apps.spare: not assigned to any control"
```

To find every entry still in the old form:

```bash
journalctl --user -u apptrol | grep 'config uses an old form'
```

A match fragment ending in `.monitor` names a recording of an output, never an input
or output that Apptrol matches (CFG-21). Every load warns, including reloads, while the
configuration stays valid. `apptrol.app.match` contains the offending fragment here:

```text
WARN match names a monitor, which records an output and is never matched; use the output's name without .monitor apptrol.component=config file.path=/home/you/.config/apptrol/config.toml apptrol.app.id=speakers apptrol.app.match=test-output.monitor
```

Use the output's name without `.monitor` in `[outputs.<id>]`. Invalid output selectors
(such as both `match` and `device`) use the usual `config_invalid` error records, with
the reason in `exception.message` and `apptrol.component=config`.

An output with `device = "default"` on slider 6, while you pick the headphones in the
desktop's sound settings (OUT-03, [ADR 0038](adr/0038-default-output-follows-the-server.md)).
The change is logged once, then what the control does: the speakers are released and
keep their volume, the headphones keep theirs until slider 6 moves. Without a default
output, the control says it does nothing:

```text
INFO default output changed apptrol.component=mixer apptrol.device.name=alsa_output.usb-<headset>.analog-stereo apptrol.device.previous_name=alsa_output.pci-<card>.analog-stereo
INFO output released; no control holds the device apptrol.component=mixer apptrol.device.name=alsa_output.pci-<card>.analog-stereo
INFO output matched apptrol.component=mixer apptrol.layout=default apptrol.control=slider6 apptrol.app.id=main apptrol.app.name="Main volume" apptrol.app.type=output apptrol.device.name=alsa_output.usb-<headset>.analog-stereo apptrol.device.description="USB Headset" apptrol.device.selected_by=default
INFO default output keeps its volume until the control is moved apptrol.component=mixer … apptrol.control=slider6 … apptrol.device.name=alsa_output.usb-<headset>.analog-stereo
INFO no default output; the control does nothing until there is one apptrol.component=mixer … apptrol.control=slider6 …
```

When the default is a device a fixed output holds too, the earlier control owns it;
the record is about the control that yields, and `apptrol.device.controls` names the
owner first. It is allowed, so it is info, not a warning:

```text
INFO default output is the same device as another output; the earlier control owns it apptrol.component=mixer … apptrol.control=knob7 … apptrol.device.name=alsa_output.usb-<headset>.analog-stereo apptrol.device.controls="slider6, knob7"
```

A fixed output on slider 7 at connect, then moved; a second control on the same
device; the device unplugged (OUT-01, OUT-04, OUT-13,
[ADR 0027](adr/0027-output-volume.md)). The output records carry the layout, control
and target like every mixer record, with `apptrol.app.type=output`:

```text
INFO output matched apptrol.component=mixer apptrol.layout=default apptrol.control=slider7 apptrol.app.id=speakers apptrol.app.name=Speakers apptrol.app.type=output apptrol.device.name=alsa_output.pci-<card>.analog-stereo apptrol.device.description="Built-in Audio Analog Stereo" apptrol.device.selected_by=match
DEBU volume changed apptrol.component=mixer apptrol.layout=default apptrol.control=slider7 apptrol.app.id=speakers apptrol.app.name=Speakers apptrol.app.type=output apptrol.volume_percent=40
WARN output match ignored: an earlier control owns the device apptrol.component=mixer apptrol.layout=default apptrol.control=knob7 apptrol.app.id=also apptrol.app.name=Also apptrol.app.type=output apptrol.device.name=alsa_output.pci-<card>.analog-stereo apptrol.device.controls="slider7, knob7"
INFO output device gone; the control does nothing until a device matches apptrol.component=mixer apptrol.layout=default apptrol.control=knob7 apptrol.app.id=headphones apptrol.app.name=Headphones apptrol.app.type=output apptrol.device.name=alsa_output.usb-<headset>.analog-stereo
```

When the match fits several outputs, the first by name is used, with a warning that
names all of them; it comes again only when another device is chosen. A target that
fits none warns at connect and after every reload:

```text
WARN several output devices match; using the first (make the match more specific: apptrol list shows each device's unique name) apptrol.component=mixer … apptrol.control=slider7 … apptrol.device.name=alsa_output.pci-<card>.analog-stereo apptrol.device.matches="alsa_output.pci-<card>.analog-stereo, alsa_output.usb-<dock>.analog-stereo"
WARN no output device matches apptrol.component=mixer … apptrol.control=knob7 … apptrol.app.match=headset
```

M toggles the owner's mute. An outside mute is taken over only by that owner,
including on a knob (OUT-05/06, ADR 0035). Conflicts warn when they begin and at reload;
`apptrol.device.controls` lists the winner first, then the ignored control. A blocked
M press and a blocked volume movement explain why they do not change the device:

```text
INFO muted apptrol.component=mixer … apptrol.control=slider7 … apptrol.button=M7
INFO unmuted outside Apptrol apptrol.component=mixer … apptrol.control=slider7 … apptrol.device.name=<output>
DEBU output mute request completed apptrol.component=audio apptrol.device.name=<output> apptrol.muted=true apptrol.audio.result=success
DEBU output mute echo ignored: matches Apptrol's write apptrol.component=audio apptrol.device.name=<output> apptrol.muted=true
DEBU button ignored: output belongs to an earlier control apptrol.component=mixer … apptrol.control=slider8 … apptrol.button=M8 apptrol.device.name=<output>
DEBU position saved without changing volume: output belongs to an earlier control apptrol.component=mixer … apptrol.control=slider8 … apptrol.device.name=<output>
```

Outputs have no solo (ADR 0036). A press on S says so at debug:

```text
DEBU button has no function on an output column apptrol.component=mixer apptrol.layout=default apptrol.control=slider7 apptrol.app.id=speakers apptrol.app.name=Speakers apptrol.app.type=output apptrol.button=S7
```

For three seconds after an output appears, the audio adapter sets its volume again
if the audio server restores a remembered one over Apptrol's (OUT-05), as for streams
and inputs:

```text
DEBU audio server changed the volume of a new output; setting it again apptrol.component=audio apptrol.device.name=alsa_output.usb-<headset>.analog-stereo apptrol.volume_percent=100 apptrol.wanted_percent=40
```

At debug, the audio adapter also logs each read of an output device, with what the
guard made of it, and the server's answer to each volume it sets. A read that sees a
new description waits for the mixer: `output kept by a control; …` then checks the
volume it read (OUT-05, OUT-13). A request that succeeded was passed on to the
device, not necessarily applied: within the three seconds, the adapter reads the
output again 200 ms after each write and sets a volume the server lost again
(`output read again after a write`, #166). If 64 read-backs are already
waiting, which only happens when the adapter is stuck, the read is dropped and logged
as `output read again skipped; too many waiting`:

```text
DEBU output volume request completed apptrol.component=audio apptrol.device.name=alsa_output.usb-<headset>.analog-stereo apptrol.audio.channels=2 apptrol.wanted_percent=40 apptrol.audio.result=success
DEBU output read again after a write apptrol.component=audio apptrol.device.name=alsa_output.usb-<headset>.analog-stereo
DEBU output device read apptrol.component=audio apptrol.device.index=<index> apptrol.device.name=alsa_output.usb-<headset>.analog-stereo apptrol.device.description="USB Headset" apptrol.audio.channels=2 apptrol.volume_percent=100 apptrol.audio.guard.state=restore_volume apptrol.audio.guard.remaining_ms=2972 apptrol.wanted_percent=40
DEBU output device read apptrol.component=audio apptrol.device.index=<index> apptrol.device.name=alsa_output.usb-<headset>.analog-stereo apptrol.device.description="USB Headset Pro" apptrol.audio.channels=2 apptrol.volume_percent=100 apptrol.audio.guard.state=identity_changed apptrol.audio.guard.remaining_ms=2540 apptrol.wanted_percent=40
DEBU output kept by a control; checking the volume and mute last read apptrol.component=audio apptrol.device.name=alsa_output.usb-<headset>.analog-stereo apptrol.volume_percent=100 apptrol.muted=false apptrol.audio.guard.state=restore_volume
```

While the newest reported identity has no ownership answer, later volume notifications
log `apptrol.audio.guard.state=ownership_pending` and do not restore the volume.
An older answer cannot resume defense; the adapter waits for the newest generation's
answer without blocking notifications (CTRL-06). It logs each answer it ignores, with
the identity the mixer answered for and the newest one it reported:

```text
rename ─► identity 3 ─► rename ─► identity 4 ─► KeepOutput(3): ignored ─► KeepOutput(4): defended
```

```text
DEBU output ownership answer ignored; a newer identity was reported apptrol.component=audio apptrol.device.name=alsa_output.usb-<headset>.analog-stereo apptrol.action=KeepOutput apptrol.audio.ownership.answer_generation=3 apptrol.audio.ownership.generation=4
```

A volume Apptrol set and the server accepted counts as the last read, so
`output kept by a control; …` right after the first volume of a new output sets
nothing again. When nothing was read (the output was there at connect), that record
has no `apptrol.volume_percent`.

When no control holds an output any more (a reload took its control away, or its new
description no longer matches), the mixer releases it: a volume set elsewhere then
stays, even within the guard's three seconds (CTRL-06):

```text
INFO output released; no control holds the device apptrol.component=mixer apptrol.device.name=alsa_output.usb-<headset>.analog-stereo
```

The guard defends the mute the owning control set in the same way (#177). Within its
three seconds a mute change is never taken over: WirePlumber restoring a remembered
mute looks just like an outside change, so the guard sets the control's mute again.
After them, an outside change is taken over (`muted outside Apptrol`). If M's LED and
the device disagree after a plug-in, look for these lines:

```text
appears ─ write muted ─ echo ─ server: unmuted ─► not taken over; muted again
```

```text
DEBU output mute request completed apptrol.component=audio apptrol.device.name=alsa_output.usb-<headset>.analog-stereo apptrol.muted=true apptrol.audio.result=success
DEBU output mute echo ignored: matches Apptrol's write apptrol.component=audio apptrol.device.name=alsa_output.usb-<headset>.analog-stereo apptrol.muted=true
DEBU audio server changed the mute of a new output; setting it again apptrol.component=audio apptrol.device.name=alsa_output.usb-<headset>.analog-stereo apptrol.muted=false apptrol.wanted_muted=true
DEBU output mute change not taken over: the new-output guard holds the mute apptrol.component=audio apptrol.device.name=alsa_output.usb-<headset>.analog-stereo apptrol.muted=false apptrol.audio.guard.state=restore_mute apptrol.audio.guard.remaining_ms=2890
```

`apptrol.audio.guard.state=ownership_pending` on the last line means the device was
just renamed and the mixer has not decided yet who owns it; `KeepOutput` then sets the
owner's mute again. A released output's guard holds no mute, so its changes go to the
mixer at once.

To follow everything about outputs:

```bash
journalctl --user -u apptrol | grep -E 'apptrol.app.type=output|output (matched|device|released|kept|volume)|new output'
```

A slider moves (level `debug`):

```text
DEBU volume changed apptrol.component=mixer apptrol.layout=default apptrol.control=slider1 apptrol.app.id=spotify apptrol.app.name=Spotify apptrol.app.type=app apptrol.volume_percent=71
```

Problems with the controller and PipeWire:

```text
WARN controller not found; waiting for it to be plugged in apptrol.component=controller apptrol.controller.port=nanoKONTROL2 apptrol.controller.sound_cards="PCH, Mixer"
ERRO cannot connect to the audio server; retrying apptrol.component=audio error.type=audio_server_unreachable exception.message="connecting to the audio server: dial unix /run/user/<uid>/pulse/native: connect: no such file or directory"
```

## Finding lines

```bash
# everything about the controller
journalctl --user -u apptrol | grep apptrol.component=controller
# everything about one slider
journalctl --user -u apptrol | grep apptrol.control=slider3
# every configuration problem
journalctl --user -u apptrol | grep error.type=config_invalid
# every user's Apptrol in one timeline (needs the adm group), e.g. after Switch user
journalctl _SYSTEMD_USER_UNIT=apptrol.service --since today
# only one of them, by user id (id -u <user>)
journalctl _SYSTEMD_USER_UNIT=apptrol.service _UID=1001
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

Every attribute and every `error.type` is listed in [Log attributes](log-attributes.md).
