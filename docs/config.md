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

A complete example is in [`examples/config.toml`](https://github.com/Dzobash/apptrol/blob/main/examples/config.toml).

> This page describes the Phase 1 format. Sections marked *(later)* are reserved and
> will be documented when the phase that uses them is built.

---

## `[controller]`

| Key | Type | Default | Description |
|---|---|---|---|
| `port` | string | `"nanoKONTROL2"` | ALSA card id of the controller (case-insensitive). It is the name in brackets in `cat /proc/asound/cards`. The card number can change between boots; the id does not. |
| `at_login_screen` | string | `"keep"` | What happens to the controller while the login screen is in front, e.g. after *Switch user* before anyone has logged in: `"keep"` (the controller stays with whoever had it last; if that is you, sliders and mute keep working, launchers start nothing) or `"release"` (Apptrol lets go of it and turns its LEDs off, so nobody at the login screen changes your audio). The lock screen of your own session is not the login screen. See [Several users](#several-users). |

### Several users

Each user can run their own Apptrol, with their own configuration and saved state:
`systemctl --user enable --now apptrol` once in their own login. The controller works for
whoever is in front of the computer. When another user's session comes to the front,
your Apptrol turns its LEDs off and lets go of the controller; when you come back, it
takes the controller again within a few seconds and sets its LEDs. An Apptrol in a login
without a screen (e.g. over SSH) never takes the controller from the person at the desk.

Apptrol does not react to the click on *Switch user* itself, but to who is in front of
the computer, as the system reports it: your session, the login screen, or another user.
`at_login_screen` decides what happens while the **login screen** is in front:

```text
In front:        you             login screen    other user      login screen    you
                                 (you switch)    (logged in)     (they switch)   (you log in)

keep (default):  yours           yours           theirs          theirs          yours
release:         yours           nobody's        theirs          nobody's        yours
```

- **`keep`:** the login screen changes nothing; the controller stays with whoever had it.
  Coming from you, your sliders and mute keep working there. Coming back from the other
  user, it stays with them until you have logged in.
- **`release`:** while the login screen is in front, the controller belongs to nobody: all
  LEDs are off and it does nothing.

When your session leaves the front, the system takes the speakers from it too, so music
you were playing pauses. It does not resume by itself when you come back: press ▶ or the
column's R.

---

## `[log]`

| Key | Type | Default | Description |
|---|---|---|---|
| `level` | string | `"warn"` | `debug`, `info`, `warn` or `error`. Use `info` to see what Apptrol does (mute, solo, apps found), e.g. when something does not work as expected. For one run in a terminal, `apptrol --log-level debug` overrides it without changing the file. |
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
| `name` | string | the id | Display name, used in logs (and later in popups and `apptrol setup`). |
| `type` | string | `"app"` | `"app"` for playback streams, `"input"` for a capture device (microphone). |
| `match` | list of strings | — (required) | Case-insensitive name fragments. See below. |
| `max_volume` | integer | `100` | Volume in percent with the control at the top, from 1 to 150. The control spans 0 to this value: with `150`, the middle is 75 %. Below 100 it works as a cap, e.g. `80` for games that are always too loud. Above 100 the audio is amplified in software and can distort; read [Hearing safety](#hearing-safety) first. |
| `talk_over_volume` | integer | `25` | Inputs only. The volume in percent (0–100) that apps go down to during talk-over (see [Buttons](#layoutsnamebuttons)). Apps already below it stay where they are. |

### Hearing safety

Apptrol sets volumes the moment a control moves. Keep this in mind before you raise
`max_volume`:

- **Above 100 % the sound is amplified in software**, past the level the app or device
  normally reaches. Loud sound, above all on headphones, can damage your hearing.
- **The first move of a control sets its volume at once** (the controller has priority).
  If a slider sits at the top while the volume was turned down elsewhere, touching it
  jumps straight to its full level.
- **We recommend `max_volume` of 100 or less.** Use values above 100 only for a quiet
  source, and keep the slider low until you have heard the result.

You are responsible for the volumes you set. Apptrol is provided "as is", without
warranty of any kind ([LICENSE](https://github.com/Dzobash/apptrol/blob/main/LICENSE)).

### How matching works

- **`type = "app"`** — a playback stream belongs to the app if any fragment is found in its
  application name (`application.name`) or program name (`application.process.binary`).
  All matching streams are controlled together, so a browser with three playing tabs is
  one app. A match list can also cover several programs, e.g. `["vivaldi", "firefox"]`.
- **Media players** (for R and the media keys) belong to an app the same way: a fragment
  found in the player's bus name (without `org.mpris.MediaPlayer2.` and `.instance…`), the
  name it gives itself, or its desktop ID. With `level = "debug"` the log shows every
  player and why it matched or not. Chromium-based apps often call their player
  `chromium`, so `"chromium"` in a match list catches every one of them (Chrome, but also
  e.g. Wavebox or Electron apps); `"chrome"` matches only Chrome, by its name. Players on
  other devices (KDE Connect) are never matched.
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

## `[layouts.<name>.buttons]`

What the buttons do in this layout. Every button has a default; set only the ones you
want to change. A value is either a **mode** or a **launcher** that starts an app.

```toml
[layouts.default.buttons]
m8     = { mode = "hold_to_talk", talk_over = true }   # hold M8: mic live, music down
s8     = { mode = "talk_over" }                        # hold S8: music down only
r3     = { app = "discord", if_running = "skip" }      # R3 opens Discord unless it runs
record = { app = "com.obsproject.Studio" }             # ● starts OBS
```

### Buttons on a column with an app

| Button | Default | Can be |
|---|---|---|
| `s1` … `s8` | solo | — (always solo) |
| `m1` … `m8` | mute | — (always mute) |
| `r1` … `r8` | `play_pause` | `play_pause` (play or pause this app), `off`, or a launcher |

### Buttons on a column with an input (microphone)

| Button | Default | Can be |
|---|---|---|
| `m1` … `m8` | `mute` | `mute`: press to mute, press again to go live. Use it as a push-to-talk toggle. <br> `hold_to_talk`: live only while you hold M, muted otherwise (also while Apptrol is stopped). <br> Add `talk_over = true` to turn the other apps down while the microphone is live through M. |
| `s1` … `s8` | `cough` (`off` with `hold_to_talk`) | `cough`: muted while you hold S. <br> `talk_over`: other apps go down while you hold S; the microphone stays as it is. <br> `off` |
| `r1` … `r8` | `off` | `off`, or a launcher |

The S and R LEDs of an input column stay lit, so you can see which column is the
microphone; the M LED is lit while the microphone is live.

**Talk-over** turns every app of the layout down to the input's `talk_over_volume`
(default 25 %), never up. An app whose control you have not moved yet is muted instead,
because Apptrol does not know its volume. Moving a control during talk-over takes effect
when talk-over ends.

### Launchers

`record`, `marker_set`, `marker_prev`, `marker_next` and any `r` button can start an app:

| Key | Description |
|---|---|
| `app` | A desktop ID, e.g. `"com.obsproject.Studio"` or `"firefox_firefox"` (`apptrol list apps` shows them). |
| `command` | A program and its arguments, run without a shell: `["konsole", "-e", "htop"]`. Use `["sh", "-c", "…"]` for pipes. |
| `if_running` | `"start"` (default): start it on every press. `"skip"`: not if it already runs. |
| `when_locked` | `false` (default): the button starts nothing while the screen is locked. `true`: it does, for anyone at the controller; see below. |

Set `app` or `command`, not both.

**At the lock screen** launchers start nothing: the lock screen does not stop the
controller, and an app or command started there runs unseen, by whoever pressed the
button. Everything else (sliders, knobs, M, S, the media keys, the microphone buttons)
keeps working. Every button pressed while the screen is locked is written to the log as a
warning, so you can see afterwards what was touched:

```text
WARN button pressed while the screen is locked apptrol.component=mixer apptrol.button=M2 apptrol.screen.state=locked
```

For a launcher that is useful while you are away, such as a script that turns the lights
off, set `when_locked = true` on that button only:

```toml
record = { command = ["~/bin/lights-off"], when_locked = true }
```

Each such button is a warning on every configuration load and in `apptrol check`, and
each of its presses at the lock screen is a warning in the log. When Apptrol cannot tell
whether the screen is locked (see [Known issues](known-issues.md#the-lock-screen)), no launcher starts,
`when_locked` or not.

**How `"skip"` knows an app runs:** when you press the button, Apptrol looks for a running
systemd unit with the app's desktop ID in its name (apps started from the menu, by
Apptrol, Flatpak and Snap apps all have one), then for a process of yours with the app's
program name (apps started from a terminal). Two exceptions:

- **Steam games** (`steam steam://rungameid/…`) are always passed to Steam, which never
  starts a running game twice. Steam itself can stay in a game's unit after the game has
  ended, so a unit says nothing about the game.
- **Apps started through a wrapper** (`flatpak`, `sh`, `env`, `python`, …) are only looked
  for by their unit: the wrapper's name does not say which app runs.

An app whose wrapper hides its program name can still be missed and start twice. With
`level = "debug"`, the log says what was checked.

Each app runs in a systemd unit of its own, `app-apptrol-<desktop ID>@<random>.service`:
it is not a child of Apptrol and keeps running when Apptrol stops or restarts.
`journalctl --user -u 'app-apptrol-*'` shows what an app printed. The Record LED flashes
briefly on a press. Apps that need root (e.g. Synaptic) work through their desktop ID:
they ask for your password in a dialog.

> **Your commands are your responsibility.** A `command` runs exactly what you write,
> with your user's rights. The authors accept no liability for what a command you
> configure does. The list below is a safety net against accidents, not protection.

Rules checked on load (each problem is reported on its own line):

- Unknown button names and modes are rejected.
- `m` and `s` settings need an input on that column's slider; they cannot start apps.
- `play_pause` needs an app on the column's slider.
- `talk_over = true` is only for M buttons; `if_running` only for launchers.
- `cough` together with `hold_to_talk` is rejected: release M to mute.
- A `command` from the list of blocked commands below is rejected.

A problem makes the whole file invalid: Apptrol keeps the previous valid configuration,
or waits for a valid one at start, and logs which line and why.

### Blocked commands

Some commands are refused in a `command`, because they cannot be undone or wreck the
system. They are the ones security guides and the guardrails of AI coding agents agree
on. The check looks at the program and its arguments, after `env`, `nohup`, `pkexec` and
similar wrappers, and inside `sh -c "…"` text:

| Group | Examples | Why |
|---|---|---|
| Delete everything | `rm -rf /`, `rm -rf ~`, `rm -rf /*`, `rm -r -f $HOME`, `rm -rf /home` | All your files are gone |
| Wipe a disk | `mkfs…`, `wipefs`, `dd … of=/dev/sda`, `… > /dev/nvme0n1` | The disk's data is destroyed |
| Fork bomb | `:(){ :\|:& };:` | Freezes the computer |
| Download and run | `curl … \| sh`, `wget -qO- … \| bash` | Runs unknown code from the internet |
| Rights on everything | `chmod -R … /`, `chown -R … /` | Breaks the system's security |
| Root in a terminal | `sudo …`, `su …`, `doas …` | They ask for the password in a terminal, which a launcher does not have. Use `pkexec` (it asks in a dialog) or the app's desktop ID |

Everything else is allowed, e.g. `rm -rf /tmp/cache`, `pkexec synaptic` or
`["sh", "-c", "pgrep spotify || spotify"]`. The error names the group:

```text
layouts.default.buttons.r4.command: blocked: deletes all your files ("rm -rf /" or "rm -rf ~"); see "Blocked commands" in docs/config.md
```

This is not security: text inside `sh -c` can always be written so that no check
recognises it (research in 2026 found most such lists can be bypassed). It catches
copy-paste accidents. A plain command, without `sh -c`, is checked reliably, because no
shell rewrites it.

## `[media]`

| Key | Type | Default | Description |
|---|---|---|---|
| `player` | string | — | An app id from `[apps]`, on a control or not. The media keys (◀◀ ▶▶ ■ ▶) then control that app's player only, and do nothing while it is not running; without it, the player that most recently started playing. |

---

## Reserved sections *(later)*

| Section | Phase | Purpose |
|---|---|---|
| `[osd]` | 1.6 | On-screen feedback on/off. |
| `[layouts.<name>]` switch options | 2 | Behaviour on layout switch, fixed values, per-control overrides. |
