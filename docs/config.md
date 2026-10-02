# Apptrol — Configuration reference

Apptrol reads a single [TOML](https://toml.io) file:

- Default location: `~/.config/apptrol/config.toml` (`$XDG_CONFIG_HOME/apptrol/config.toml`)
- Other location: `apptrol --config /path/to/config.toml`
- If there is no file, Apptrol creates it from the example on its first start.
- The file is **reloaded automatically** when saved. If the new version is invalid, the
  error is logged and the previous configuration stays active.
- `apptrol check` validates the file without starting the service and shows what is
  assigned where. It lists every problem at once, for example:

  ```
  apptrol check: ~/.config/apptrol/config.toml: 2 problems
    - layouts.default.slider9: unknown control (use slider1–slider8 or knob1–knob8)
    - log.levle: unknown setting
  ```

  Unknown settings are rejected on purpose: they are almost always typos.

A complete example is in [`examples/config.toml`](../examples/config.toml).

> This page describes the Phase 1 format. Sections marked *(later)* are reserved and
> will be documented when the phase that uses them is built.

---

## `[controller]`

| Key | Type | Default | Description |
|---|---|---|---|
| `port` | string | `"nanoKONTROL2"` | ALSA card id of the controller (case-insensitive). It is the name in brackets in `cat /proc/asound/cards`. The card number can change between boots; the id does not. |

---

## `[log]`

| Key | Type | Default | Description |
|---|---|---|---|
| `level` | string | `"info"` | `debug`, `info`, `warn` or `error`. For one run in a terminal, `apptrol --log-level debug` overrides it without changing the file. |
| `outputs` | list | `["journald"]` | Where logs go: `"journald"`, `"file"`, or both. |

### `[log.journald]`

| Key | Type | Default | Description |
|---|---|---|---|
| `format` | string | `"text"` | `text` or `logfmt`. |

Under systemd, each line carries its severity, so `journalctl --user -u apptrol -p warning`
shows only warnings and errors. Started from a terminal, this output prints coloured text
to the terminal instead.

How to read the log lines, and every attribute and error type in them, is explained in
[Reading Apptrol's logs](logging.md).

### `[log.file]`

| Key | Type | Default | Description |
|---|---|---|---|
| `path` | string | `"~/.local/state/apptrol/apptrol.log"` | Log file. `~` is expanded. The directory is created if missing. |
| `format` | string | `"json"` | `text`, `json` or `logfmt`. |
| `max_size` | string | `"10MB"` | Rotate when the file reaches this size (`KB`, `MB`, `GB`). |
| `max_files` | integer | `5` | Number of rotated files to keep. |

**Logging to `/var/log`.** Apptrol runs as your user, and normal users cannot write to
`/var/log`. To use it, create a directory owned by your user once:

```bash
sudo install -d -o "$USER" -g "$USER" -m 0750 /var/log/apptrol
```

and set `path = "/var/log/apptrol/apptrol.log"`. Apptrol rotates the file itself, so no
`logrotate` rule is needed.

---

## `[apps.<id>]`

Defines something a control can act on. `<id>` is your own short name (letters, digits,
`-`, `_`), used in layouts. Each app is defined once and can be used in any layout.

| Key | Type | Default | Description |
|---|---|---|---|
| `name` | string | the id | Display name, used in logs (and later in popups and the GUI). |
| `type` | string | `"app"` | `"app"` for playback streams, `"input"` for a capture device (microphone). |
| `match` | list of strings | — (required) | Case-insensitive name fragments. See below. |
| `max_volume` | integer | `100` | Volume in percent with the control at the top, from 1 to 150. The control spans 0 to this value: with `150`, the middle is 75 %. Below 100 it works as a cap, e.g. `80` for games that are always too loud. Above 100 the audio is amplified in software and can distort. |

### How matching works

- **`type = "app"`** — a playback stream belongs to the app if any fragment is found in its
  application name (`application.name`) or program name (`application.process.binary`).
  All matching streams are controlled together, so a browser with three playing tabs is
  one app. A match list can also cover several programs, e.g. `["vivaldi", "firefox"]`.
- **`type = "input"`** — the capture device whose name or description contains a fragment.
  If several devices match, the first one is used and a warning is logged. Monitors of
  outputs are not input devices and are never matched.

### When several devices have the same description

Some audio interfaces offer several inputs that all carry the same description. A GoXLR
Mini, for example, shows up as three capture devices, all described as "GoXLRMini". A match
on `"GoXLR"` then catches all three; Apptrol uses the first one (by name) and logs a
warning:

```
WARN several input devices match; using the first. Make the match more specific: …
```

Every device also has a **unique name**. `apptrol list` shows it in the NAME column:

```
Input devices. Put part of DESCRIPTION or NAME into an input's match list:
  DESCRIPTION  NAME                                                        VOLUME  CONTROL
  GoXLRMini    alsa_input.usb-TC-Helicon_GoXLRMini-00.HiFi__Headset__source  100%    slider8 (Microphone)
  GoXLRMini    alsa_input.usb-TC-Helicon_GoXLRMini-00.HiFi__Line4__source    100%    -
  GoXLRMini    alsa_input.usb-TC-Helicon_GoXLRMini-00.HiFi__Line5__source    100%    -
```

Put the part of the name that tells them apart into `match`:

```toml
[apps.mic]
name  = "Microphone"
type  = "input"
match = ["GoXLRMini-00.HiFi__Headset"]
```

To find out which input is your microphone, assign one, speak, and move its slider:
you hear the level change (or watch it in your desktop's volume settings). The same
approach works for apps: if two programs report the same name, use the BINARY column.

**One app per stream.** A stream (or input device) is controlled by only one app. If the
match lists of two apps in the layout both catch it, it goes to the app on the first
control, in the order slider1 to slider8, then knob1 to knob8; the other app's control does
nothing for it. `apptrol check` and the log warn when two match lists overlap in this way
(one fragment contains the other, such as `"fire"` and `"firefox"`).

Run `apptrol list` to see the exact names of everything that is currently playing and every
input device, and which control each one is on.

A browser is always **one** app: all tabs share the browser's stream name, so YouTube in one
tab cannot be controlled separately from another tab.

---

## `[layouts.<name>]`

Assigns apps to controls. Phase 1 uses only `[layouts.default]`; other layouts are
accepted and ignored until Phase 2.

| Key | Value | Description |
|---|---|---|
| `slider1` … `slider8` | app id | Sliders, left to right. The S and M buttons in the same column act on this app. |
| `knob1` … `knob8` | app id | Knobs, left to right. Volume only. |

Rules checked on load:

- Every value must be the id of an app defined in `[apps]`.
- An app may be assigned to only one control per layout.
- Unassigned controls do nothing.
- Apps whose match lists overlap get a warning (see "One app per stream" above).

---

## Reserved sections *(later)*

| Section | Phase | Purpose |
|---|---|---|
| `[media]` | 1.5 | Media buttons; optional `player` to pin one MPRIS player. |
| Buttons in `[layouts.<name>]` | 1.5 | Launchers for Record and the Marker buttons; R overrides per column (launcher, push-to-talk). |
| Talk-over level | 1.5 | How far apps are turned down while S is held on an input column. |
| `[osd]` | 1.6 | On-screen feedback on/off. |
| `[layouts.<name>]` switch options | 2 | Behaviour on layout switch, fixed values, per-control overrides. |
| `outputs` in `[apps.<id>]` | Backlog | Allowed outputs for an R override. |
