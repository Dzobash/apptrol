# Apptrol — Configuration reference

Apptrol reads a single [TOML](https://toml.io) file:

- Default location: `~/.config/apptrol/config.toml` (`$XDG_CONFIG_HOME/apptrol/config.toml`)
- Other location: `apptrol --config /path/to/config.toml`
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
| `level` | string | `"info"` | `debug`, `info`, `warn` or `error`. |
| `outputs` | list | `["journald"]` | Where logs go: `"journald"`, `"file"`, or both. |

### `[log.journald]`

| Key | Type | Default | Description |
|---|---|---|---|
| `format` | string | `"text"` | `text` or `logfmt`. |

Under systemd, each line carries its severity, so `journalctl --user -u apptrol -p warning`
shows only warnings and errors. Started from a terminal, this output prints coloured text
to the terminal instead.

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

### How matching works

- **`type = "app"`** — a playback stream belongs to the app if any fragment is found in its
  application name (`application.name`) or program name (`application.process.binary`).
  All matching streams are controlled together, so a browser with three playing tabs is
  one app. A match list can also cover several programs, e.g. `["vivaldi", "firefox"]`.
- **`type = "input"`** — the capture device whose name or description contains a fragment.
  If several devices match, the first one is used and a warning is logged. Monitors of
  outputs are not input devices and are never matched.

Run `apptrol list` to see the exact names of everything that is currently playing and every
input device.

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

---

## Reserved sections *(later)*

| Section | Phase | Purpose |
|---|---|---|
| `[media]` | 1.5 | Media buttons; optional `player` to pin one MPRIS player. |
| `[osd]` | 1.5 | On-screen feedback on/off. |
| `[layouts.<name>]` switch options | 2 | Behaviour on layout switch, fixed values, per-control overrides. |
| `outputs` in `[apps.<id>]` | Backlog | Allowed outputs for the R button. |
| `[record]` | Backlog | Recording app and start/stop commands. |
