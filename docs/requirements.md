# Apptrol — Requirements

| | |
|---|---|
| **Status** | Phase 1 released (0.1.0); Phase 1.5 specified (0.2.0); later phases outlined |
| **Last updated** | 2026-10-02 |
| **Related** | [Roadmap](roadmap.md) · [Configuration reference](config.md) · [Decision records](adr/) |

## 1. Purpose

Apptrol turns a hardware MIDI controller into a per-application volume mixer on Linux.
Each slider or knob controls the volume of one application (or an input such as a
microphone), so the operating system's software mixer is no longer needed for
day-to-day volume changes.

The first supported controller is the **Korg nanoKONTROL2**. Apptrol is designed to run
next to a main audio interface (for example a GoXLR Mini), but it does not depend on one
and works just as well with a plain internal sound card.

### 1.1 Conventions

- The key words **MUST**, **SHOULD** and **MAY** are used as described in
  [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).
- Every requirement has a stable ID (`AREA-NN`). IDs are never reused; a dropped
  requirement is marked *withdrawn* rather than deleted. Commits, issues and tests
  reference these IDs.
- **Phase** says when a requirement is delivered. Phases 1 and 1.5 are specified in full;
  later phases are outlined in section 6 and in the [roadmap](roadmap.md).
- A requirement that changes keeps its ID; its text describes the current behaviour,
  followed by how it was before, e.g. *(Until 0.1.x: …)*.

### 1.2 Terms

| Term | Meaning |
|---|---|
| **Control** | A physical slider or knob on the controller. |
| **Column** | One of the eight vertical strips on the nanoKONTROL2: a slider, a knob above it, and the S / M / R buttons next to it. |
| **Target** | What a control acts on: an *app* (one or more playback streams) or an *input* (a capture device such as a microphone). |
| **App** | A target defined in the config, matched against playback streams by name. |
| **Layout** | A set of control → target assignments. Phase 1 has exactly one layout, `default`. |
| **User mute** | Mute set by pressing a column's M button. |
| **Solo** | A temporary state in which only one column's app stays audible. |
| **Position** | The last known physical value of a control (0–127). *Unknown* until the control is first moved, unless restored from saved state. |
| **Media player** | A program that offers playback control over MPRIS on the session bus (Spotify, a browser, VLC, …). A browser offers one, for its most recently used playing tab. |
| **Launcher** | A button set up to start an app (Record, the Marker buttons, or an R button). |
| **Desktop ID** | The name of an installed app's `.desktop` file without `.desktop`, e.g. `com.obsproject.Studio` (Flatpak) or `firefox_firefox` (Snap). |
| **Held state** | A state that lasts only while a button is held: cough, push-to-talk live, talk-over. |

## 2. Scope

### 2.1 In scope (Phase 1)

- Sliders and knobs set the volume of configured apps and inputs.
- M (mute) and S (solo) buttons for slider columns, with LED feedback.
- The controller always has priority over other volume changes.
- State is saved and restored across restarts.
- A TOML configuration file that already uses the layout structure, reloaded automatically.
- Structured logging to journald and/or a log file.
- Runs as a systemd user service on any Linux desktop with PipeWire.

### 2.2 Out of scope for Phase 1

Media buttons, launcher buttons, functions for R and for S on an input column, on-screen
popups, multiple layouts, pick-up, a terminal setup and a tray icon, moving apps between
outputs. See section 6 and
the [roadmap](roadmap.md).

### 2.3 Never in scope

- Audio routing or effects (EQ, compression, mixing streams together, replacing the
  microphone signal with a tone ("bleep")).
- Windows or macOS support.
- Replacing the audio interface's own software (e.g. the GoXLR utility).

## 3. Hardware assumptions

| ID | Requirement | Level | Phase |
|---|---|---|---|
| HW-01 | Apptrol MUST support the Korg nanoKONTROL2 in its factory **CC mode** (not a DAW mode) with buttons set to **Momentary** (factory default). | MUST | 1 |
| HW-02 | LED feedback requires the controller's **LED mode set to "External"** (a one-time setting, e.g. with SysEx Controls on Linux or KONTROL Editor on Windows/macOS). With LED mode "Internal", all controls MUST still work; the LEDs then only light while a button is held. | MUST | 1 |
| HW-03 | When the controller connects, Apptrol MUST log an **info** message that LEDs show mute and solo only with LED mode "External", pointing to the README. It is not a warning, because Apptrol cannot yet tell which mode is set. | MUST | 1 |
| HW-04 | The controller mapping (CC numbers) SHOULD be defined in one place in the code so other controllers can be added later. | SHOULD | 1 |
| HW-05 | The controller MUST be found by its ALSA card id (from `[controller] port`), not by a fixed card or device number. | MUST | 1 |
| HW-06 | If the controller's MIDI device is busy (held by another program), Apptrol MUST log a clear error naming the device and retry. | MUST | 1 |
| HW-07 | A command `apptrol test` SHOULD show what the controller sends and toggle the LED of each pressed button, so the controller and its settings can be checked without a configuration. | SHOULD | 1 |

Factory CC numbers of the nanoKONTROL2 (MIDI channel 1). Buttons send 127 on press and 0 on release.

| Element | Columns 1–8 | | Element | CC |
|---|---|---|---|---|
| Slider | 0–7 | | Track ◀ / ▶ | 58 / 59 |
| Knob | 16–23 | | Cycle | 46 |
| S button | 32–39 | | Marker Set / ◀ / ▶ | 60 / 61 / 62 |
| M button | 48–55 | | ◀◀ / ▶▶ | 43 / 44 |
| R button | 64–71 | | ■ / ▶ / ● | 42 / 41 / 45 |

## 4. Requirements

Sections 4.1–4.10 are delivered in Phase 1 (0.1.0); sections 4.11–4.14 and the
requirements marked *Phase 1.5* in other sections in Phase 1.5 (0.2.0), designed in
ADRs [0017](adr/0017-desktop-services-over-dbus.md),
[0018](adr/0018-media-players-through-mpris.md),
[0019](adr/0019-launcher-and-column-buttons.md) and
[0020](adr/0020-microphone-column-buttons.md).

### 4.1 Controls and volume

| ID | Requirement | Level |
|---|---|---|
| CTRL-01 | Each slider and each knob MAY be assigned one target in the active layout. Unassigned controls MUST do nothing. | MUST |
| CTRL-02 | Moving a control MUST set the volume of its target to the control's position, mapped linearly from 0–127 to 0 % – the app's `max_volume` (CTRL-03). | MUST |
| CTRL-03 | Volume MUST stay between 0 % and the app's `max_volume` (default 100 %). `max_volume` MAY be set per app from 1 to 150 %; the control then spans 0 to that value, so it also works as a cap below 100 %. | MUST |
| CTRL-04 | For an **app** target, the volume MUST be applied to every playback stream that matches the app, including several streams of the same program (e.g. several browser tabs). | MUST |
| CTRL-05 | For an **input** target, the volume MUST be applied to the matching capture device. | MUST |
| CTRL-06 | Apps and inputs that are not assigned to any control MUST NOT be touched. | MUST |
| CTRL-07 | A burst of control events (moving a slider quickly) SHOULD be coalesced so that the latest value is applied without flooding the audio server. The final position MUST always be applied. | SHOULD |
| CTRL-08 | A volume change SHOULD reach the audio server within 50 ms of the control being moved. | SHOULD |

### 4.2 Controller priority

| ID | Requirement | Level |
|---|---|---|
| PRIO-01 | The controller always has priority: whenever a control is moved, its target MUST be set to the control's position, regardless of any volume change made elsewhere (e.g. in the desktop's mixer). | MUST |
| PRIO-02 | Volume changes made outside Apptrol MUST NOT be reverted actively; they are overridden the next time the control is moved. *(Rationale: some apps lower other apps' volume on purpose, e.g. voice-chat ducking.)* | MUST |
| PRIO-03 | When a new playback stream appears that matches an assigned app, its volume MUST be set to the control's known position immediately. | MUST |
| PRIO-04 | When a new stream appears and the control's position is *unknown*, the stream's volume MUST be left unchanged. | MUST |
| PRIO-05 | A new stream of an app that is user-muted or silenced by solo MUST be muted when it appears. | MUST |

### 4.3 Mute (M)

| ID | Requirement | Level |
|---|---|---|
| MUTE-01 | Pressing M on a slider column MUST toggle the user mute of that column's target; on an input column only in mode `mute` (INPUT-01). | MUST |
| MUTE-02 | Knobs have no M button; a knob's target MUST only be silenced by solo (SOLO-03) or by a mute made outside Apptrol (MUTE-07). | MUST |
| MUTE-03 | Pressing M on a column without a slider target MUST do nothing. | MUST |
| MUTE-04 | User mute and solo are separate states. An app's stream is muted when it is user-muted **or** silenced by solo. | MUST |
| MUTE-05 | Pressing M on any column while solo is active MUST toggle that column's user mute; the effect becomes audible once solo ends. | MUST |
| MUTE-07 | When an assigned app or input is muted or unmuted outside Apptrol (e.g. in the desktop's volume applet, or with a microphone mute key), Apptrol MUST take over the new state as the control's mute: the M LED follows and the state is saved. The mute applies to the whole app, so its other streams follow. Solo still applies: an app unmuted outside Apptrol while another app is soloed MUST be muted again. An input in mode `hold_to_talk` is the exception: the change is undone (INPUT-03). Changes Apptrol made itself, and settings the audio server restores on new streams (PRIO-03), MUST NOT count as outside changes. *(Unlike volume (PRIO-02), mute is a two-state setting the controller can show, so the controller and the desktop stay in agreement.)* | MUST |

### 4.4 Solo (S)

| ID | Requirement | Level |
|---|---|---|
| SOLO-01 | Pressing S on a slider column whose target is an app MUST turn solo on for that column. | MUST |
| SOLO-02 | While solo is active, every other **app** target in the layout (on sliders and on knobs) MUST be silenced. | MUST |
| SOLO-03 | Solo MUST NOT affect input targets. | MUST |
| SOLO-04 | Only one column can be soloed at a time. Pressing S on another column MUST move the solo to that column. | MUST |
| SOLO-05 | Pressing S on the soloed column MUST turn solo off. Every app then returns to its own state: user-muted apps stay muted, all others become audible. Solo MUST NOT return to a previously soloed column. | MUST |
| SOLO-06 | A soloed column's own user mute still applies (soloing a user-muted app results in silence). | MUST |
| SOLO-07 | Pressing S on an input column MUST NOT solo; it acts in the column's S mode (INPUT-01, default cough). *(Until 0.1.x: it did nothing.)* | MUST |

### 4.5 LED feedback

| ID | Requirement | Level |
|---|---|---|
| LED-01 | **App column** — the S LED MUST be lit only on the soloed column. | MUST |
| LED-02 | **App column** — the M LED MUST be lit when the column's app is user-muted. It MUST NOT light up for apps that are only silenced by solo. | MUST |
| LED-03 | **App column** — the R LED MUST show whether the column's app plays and R can control it (MEDIA-09). *(Until 0.1.x: always off.)* | MUST |
| LED-04 | **Input column** — S, M and R LEDs MUST be lit by default so the column is recognisable as an input. S and R stay lit whatever their mode. The M LED MUST be lit only while the input is live: it turns off when the input is muted, with M or by a held state (INPUT-05). *(Until 0.1.x: only the M mute counted.)* | MUST |
| LED-05 | Columns without a slider target MUST have all LEDs off. | MUST |
| LED-06 | Transport button LEDs MUST be off, except the ▶ LED, lit while the media-key player plays (MEDIA-06), and the Record LED's flash when it starts an app (LAUNCH-08). *(Until 0.1.x: always off.)* | MUST |
| LED-07 | LEDs MUST be re-sent whenever the controller (re)connects, the audio server (re)connects, and the configuration or state changes. After a controller connect they MUST be sent again once the controller has started up (it ignores LED messages for a moment after being plugged in); after an audio server connect, again 2 seconds later (a PipeWire restart can reset the controller's LEDs). | MUST |
| LED-08 | When Apptrol stops, it SHOULD turn all LEDs off, so no LED shows a state that no longer applies. | SHOULD |

### 4.6 Other buttons

| ID | Requirement | Level |
|---|---|---|
| BTN-01 | Track ◀ / ▶ and Cycle MUST do nothing; they are reserved for layouts (Phase 2). Record and the Marker buttons do nothing unless set up as launchers (LAUNCH-01). *(Until 0.1.x: R, the transport and the Marker buttons did nothing too.)* | MUST |

### 4.7 State

| ID | Requirement | Level |
|---|---|---|
| STATE-01 | Apptrol MUST save, per layout and per control, the last known position and the mute (set with M, or taken over from outside Apptrol, MUTE-07). | MUST |
| STATE-02 | State MUST be saved after every change, at most once per second, and written atomically (write to a temporary file, then rename). | MUST |
| STATE-03 | On start, saved positions and user mutes MUST be restored and applied to matching streams and inputs. | MUST |
| STATE-04 | The soloed column MUST be saved and restored on start, if it still holds an app. (Until 0.1.0-rc2: solo was not saved.) | MUST |
| STATE-05 | If the state file is missing or unreadable, Apptrol MUST start with all positions *unknown* and no user mutes, leave current volumes unchanged, and log a warning (unless it is the first start). | MUST |
| STATE-06 | The state file MUST be stored at `$XDG_STATE_HOME/apptrol/state.json` (default `~/.local/state/apptrol/state.json`). | MUST |
| STATE-07 | State of controls whose assignment was removed from the config SHOULD be discarded. | SHOULD |

### 4.8 Configuration

| ID | Requirement | Level |
|---|---|---|
| CFG-01 | Configuration MUST be a TOML file at `$XDG_CONFIG_HOME/apptrol/config.toml` (default `~/.config/apptrol/config.toml`). A different path MAY be given with `--config`. | MUST |
| CFG-02 | Apps and inputs MUST be defined once in an `[apps.<id>]` table; layouts MUST refer to them by id. | MUST |
| CFG-03 | Assignments MUST live in a `[layouts.<name>]` table. Phase 1 MUST use the layout named `default`; other layouts MUST be accepted and ignored with an info log. | MUST |
| CFG-04 | An app MUST be matched by a list of case-insensitive substrings compared with the stream's `application.name` and `application.process.binary`. | MUST |
| CFG-05 | An input MUST be matched by a case-insensitive substring of the capture device's name or description. | MUST |
| CFG-06 | Apptrol MUST reload the configuration automatically when the file is saved, without restarting. | MUST |
| CFG-07 | An invalid configuration MUST be rejected with an error log naming the problem; the previous valid configuration MUST stay active. If there is no valid configuration at start, Apptrol MUST start, log the error and wait for a valid file. | MUST |
| CFG-08 | Validation MUST reject: unknown control names, references to undefined apps, the same app assigned to more than one control in a layout, and empty match lists. | MUST |
| CFG-09 | If no configuration file exists, Apptrol SHOULD create a commented example file and log where it is. | SHOULD |
| CFG-10 | A command `apptrol list` SHOULD print the currently playing streams and capture devices with the names used for matching, to help write the config. | SHOULD |
| CFG-11 | A command `apptrol check` SHOULD validate the configuration file and exit. | SHOULD |
| CFG-12 | When two apps of the active layout can match the same streams (or input devices), because one match fragment contains another, Apptrol SHOULD warn and name the app that gets them: the one on the first control in the order slider1–slider8, knob1–knob8. | SHOULD |
| CFG-13 | *Phase 1.5.* Buttons MUST be configured per layout in `[layouts.<name>.buttons]`, with the names `record`, `marker_set`, `marker_prev`, `marker_next`, `r1`–`r8`, `s1`–`s8` and `m1`–`m8`. A value is a launcher (`app` = a desktop ID, or `command` = a list of arguments; optional `if_running`) or a `mode`; an `m` button MAY also set `talk_over = true` (INPUT-04). | MUST |
| CFG-14 | *Phase 1.5.* Validation MUST reject, with one record per problem: a launcher with both `app` and `command` or neither, an unknown button name, an unknown `mode` or `if_running` value, a mode that does not fit the button or column (`play_pause` on an input column; `mute` or `hold_to_talk` on anything but M; `cough` or `talk_over` as a mode on anything but S; `cough` together with `hold_to_talk`), an `m` or `s` setting on a column without an input, `talk_over = true` on anything but an `m` button, and a launcher on an M or S button. | MUST |
| CFG-15 | *Phase 1.5.* `[media] player` MAY name an app from `[apps]` to pin the media keys to (MEDIA-06); an unknown app MUST be rejected. | MUST |
| CFG-16 | *Phase 1.5.* An input app MAY set `talk_over_volume`, the volume in percent (0–100) that apps go down to during talk-over; default 25. On an app, or outside 0–100, it MUST be rejected. | MUST |
| CFG-17 | *Phase 1.5.* The example configuration (CFG-09) MUST document every Phase 1.5 setting: commented out, with an example value, the accepted values and the default. | MUST |

### 4.9 Logging

| ID | Requirement | Level |
|---|---|---|
| LOG-01 | Apptrol MUST use structured logging (charmbracelet/log). | MUST |
| LOG-02 | The log level MUST be configurable: `debug`, `info`, `warn`, `error`; the default MUST be `warn`. *(Until 0.1.x: `info`.)* | MUST |
| LOG-03 | Two outputs MUST be available, usable separately or together: **journald** and **file**. | MUST |
| LOG-04 | Each output MUST have its own format: journald `text` or `logfmt`; file `text`, `json` or `logfmt`. Timestamps MUST have millisecond precision. | MUST |
| LOG-05 | When running under systemd, the journald output MUST mark each line with its severity so that `journalctl -p` filtering works. When started from a terminal, it MUST print coloured text to the terminal instead. | MUST |
| LOG-06 | The file output MUST default to `$XDG_STATE_HOME/apptrol/apptrol.log`; the path MUST be configurable (e.g. to `/var/log/apptrol/` if the user has prepared that directory). | MUST |
| LOG-07 | The file output MUST rotate by size, keeping a configurable number of old files. | MUST |
| LOG-08 | Log level and outputs MUST be updated on config reload without restarting. | MUST |
| LOG-09 | Events MUST be logged at these levels: **info** — start/stop, config loaded/reloaded, controller connected/disconnected, stream matched to a control, mute/solo changes; **warn** — config entry that matches nothing, controller not found, state file unreadable; **error** — invalid config, lost connection to the audio server; **debug** — every volume change, raw MIDI message and button press without a function. | MUST |
| LOG-10 | Every log record MUST carry `apptrol.component`: `service`, `config`, `state`, `audio`, `controller`, `desktop`, `launcher` or `mixer` (ADR 0016). *(Until 0.1.x: without `desktop` and `launcher`.)* | MUST |
| LOG-11 | Attribute names MUST follow the OpenTelemetry semantic conventions: their attribute where one exists (`error.type`, `exception.message`, `file.path`, …), otherwise a name in the `apptrol.` namespace; lower case, dot-separated, snake_case within a part. No name may be the start of another, and each name MUST always carry the same type of value, so that log stores can map them. | MUST |
| LOG-12 | Every record about an error MUST carry `error.type` (a fixed word for the kind of error) and, where there is an error message, `exception.message`. | MUST |
| LOG-13 | Every record MUST be one line with a fixed message; values go into attributes. Several problems (e.g. in the configuration) MUST be logged as one record each. | MUST |
| LOG-14 | A record about a control MUST name the layout, the control and, if assigned, the app on it (id, name, type); a record caused by a button MUST name the button. Every button press MUST be logged: mute and solo changes at info, presses without a function at debug. | MUST |
| LOG-15 | *Phase 1.5.* Every decision MUST be logged with its reason, so a feature can be troubleshot from its logs alone: a media player matched to a control (info, with what matched), not matched or ignored (debug, with the reason); an app started (info, with its unit), not started because it runs (info, with how that was found) or failed to start (error); held states starting and ending (info, with the reason they ended). Records follow ADR 0018 (point 9), ADR 0019 (point 15) and ADR 0020 (point 9). | MUST |
| LOG-16 | *Phase 1.5.* `--log-level` SHOULD set the log level for one run without changing the configuration file. It MUST win over the configured level for the whole run, also after a reload; an invalid value MUST stop Apptrol with a clear message; the start record MUST say where the level came from (flag or configuration). | SHOULD |

### 4.10 Service and runtime

| ID | Requirement | Level |
|---|---|---|
| SVC-01 | Apptrol MUST run as a **systemd user service** (per logged-in user, not as root). | MUST |
| SVC-02 | Apptrol MUST NOT require root privileges for normal operation. | MUST |
| SVC-03 | If the controller is not connected, Apptrol MUST keep running, wait for it, and pick it up when it is plugged in. Unplugging MUST be handled the same way. | MUST |
| SVC-04 | If the connection to the audio server is lost (e.g. PipeWire restart), Apptrol MUST reconnect and re-apply the current state. | MUST |
| SVC-05 | Apptrol MUST shut down cleanly on SIGTERM/SIGINT, saving state first. | MUST |
| SVC-06 | `apptrol --version` MUST print the version, commit and build date. | MUST |
| SVC-07 | On shutdown, Apptrol MUST end solo and unmute every app it silenced by solo. The audio server remembers mutes per app, so otherwise those apps would stay muted after Apptrol exits. User mutes (M) stay. Both are saved first and restored on the next start (STATE-03, STATE-04). | MUST |

### 4.11 Desktop connection *(Phase 1.5)*

| ID | Requirement | Level |
|---|---|---|
| DESK-01 | Apptrol MUST connect to the D-Bus session bus at start; every feature that needs D-Bus MUST share this connection, except calls to systemd, which go through go-systemd (ADR 0017). | MUST |
| DESK-02 | Without a session bus, or when the connection is lost, sliders, knobs, M, S and the input modes MUST keep working; features that need D-Bus do nothing. Apptrol MUST log an error and reconnect, then find the media players again. | MUST |
| DESK-03 | Apptrol MUST NOT start a D-Bus service as a side effect: names that are only activatable are never called. | MUST |

### 4.12 Media players and R on app columns *(Phase 1.5)*

| ID | Requirement | Level |
|---|---|---|
| MEDIA-01 | Apptrol MUST find every media player (bus name `org.mpris.MediaPlayer2.*` with a running owner) when it connects, and follow players appearing and disappearing while it runs. | MUST |
| MEDIA-02 | Players on other devices (`kdeconnect.*`), proxies (`playerctld`) and duplicates (`plasma-browser-integration`) MUST be ignored, before any matching. | MUST |
| MEDIA-03 | A player MUST belong to an app when one of the app's match fragments occurs, case-insensitively, in the player's bus name (without the prefix and without an `.instance…` suffix), in its `Identity`, or in its `DesktopEntry` if it has one. A player that matches apps on several controls belongs to the first control, in the order of CFG-12. | MUST |
| MEDIA-04 | Apptrol MUST send `Play` or `Pause` chosen from the player's `PlaybackStatus`, never the toggle `PlayPause`. | MUST |
| MEDIA-05 | ▶ MUST send `Pause` while the media-key player plays and `Play` otherwise, also when it is stopped (the player then starts the track again, or does nothing without one); ■ MUST send `Stop`, ◀◀ `Previous` and ▶▶ `Next`. Without a player, they do nothing (debug log). | MUST |
| MEDIA-06 | The media-key player MUST be the player that most recently started playing, among all players that are not ignored, on a control or not; without such history a paused one before a stopped one, then by bus name. With `[media] player` set (CFG-15), only that app's players count, whether or not the app is on a control; if it has none, the media keys MUST do nothing. | MUST |
| MEDIA-07 | R on an app column (mode `play_pause`, the default) MUST pause every playing player of the column's app; if none plays, it MUST play the paused player that started playing most recently, or without such history the first paused one by bus name. A stopped player MUST NOT be started. | MUST |
| MEDIA-08 | If the column's app has no player (e.g. mpv without its MPRIS plugin), R MUST do nothing (debug log). | MUST |
| MEDIA-09 | The R LED of an app column in mode `play_pause` MUST be lit while one of the column's app's players reports `Playing`; otherwise off. It MUST follow status changes and players appearing or disappearing. In mode `off` or as a launcher, it MUST be off. *(Until 2026-10-02 planned to follow the corked state of the app's streams; see ADR 0018, notes.)* | MUST |
| MEDIA-10 | The audio adapter MUST report whether each stream is corked, and every change of it. | MUST |

### 4.13 Launcher buttons *(Phase 1.5)*

| ID | Requirement | Level |
|---|---|---|
| LAUNCH-01 | Record, the Marker buttons and any R button set up as a launcher (CFG-13) MUST start their app when pressed. Without a launcher, Record and the Marker buttons do nothing (debug log). | MUST |
| LAUNCH-02 | A desktop ID MUST be resolved as the Desktop Entry and Base Directory specifications say: `applications/` under `$XDG_DATA_HOME`, then each `$XDG_DATA_DIRS` entry; the first file found wins. A file with `Hidden=true` counts as not installed. | MUST |
| LAUNCH-03 | `Exec` MUST be parsed as the Desktop Entry specification says: quoting and escapes, `%f %F %u %U` removed, `%c` replaced by the app's name, `%i` and `%k` dropped. A file with `DBusActivatable=true` and no `Exec` MUST be started through `org.freedesktop.Application.Activate`. | MUST |
| LAUNCH-04 | An app MUST be started by systemd as a transient user unit (`app-apptrol-<desktop-id>@<random>.service`), never as a child process of Apptrol. It MUST keep running when Apptrol stops or restarts; its unit is removed after it exits. | MUST |
| LAUNCH-05 | A `command` MUST be run as a list of arguments, without a shell; `~/` at the start of an argument MUST be expanded. | MUST |
| LAUNCH-06 | With `if_running = "start"` (the default) the app MUST be started on every press; with `"skip"`, not if it already runs (LAUNCH-07). | MUST |
| LAUNCH-07 | Whether an app runs MUST be checked when the button is pressed: first the user manager's units for its desktop ID (`app-*<id>-*.scope`, `app-*<id>@*.service`, and `snap.<snap>.<app>-*.scope` for a Snap ID), then the running processes by the program name from `Exec` or `command`. | MUST |
| LAUNCH-08 | When Record starts an app, its LED MUST flash briefly (about 0.3 s). | MUST |
| LAUNCH-09 | `apptrol list apps [search]` SHOULD print the desktop ID, name and source (`system`, `flatpak`, `snap`, `user`) of every installed app, without `Hidden` or `NoDisplay` ones; the search is case-insensitive over ID and name. | SHOULD |
| LAUNCH-10 | `apptrol check` and every configuration load SHOULD warn about desktop IDs that are not installed; this MUST NOT make the configuration invalid. | SHOULD |
| LAUNCH-11 | An app that cannot be started MUST be logged as an error (`app_start_failed`); Apptrol keeps running. | MUST |
| LAUNCH-12 | Validation MUST reject a launcher `command` that deletes everything (`rm` with `-r` and `-f` on `/`, `/*`, `~`, `$HOME` or `/home`), wipes a disk (`mkfs*`, `wipefs`, `dd` to `/dev/…`, writing to a disk device), is a fork bomb, runs a download (`curl`/`wget` piped to a shell), changes rights on everything (`chmod -R`/`chown -R` on `/`), or uses `sudo`, `su` or `doas`; also after wrappers and inside `sh -c` text. The error MUST name the group and point to docs/config.md. Desktop IDs are not checked. It is a safety net against accidents, not security. | MUST |

### 4.14 Input column buttons *(Phase 1.5)*

| ID | Requirement | Level |
|---|---|---|
| INPUT-01 | M, S and R on an input column MUST each act in a mode (CFG-13). M: `mute` (default) or `hold_to_talk`, with the option `talk_over`. S: `cough` (default; `off` with `hold_to_talk`), `talk_over` or `off`. R: `off` (default) or a launcher. | MUST |
| INPUT-02 | `cough`: the input MUST be muted while S is held. | MUST |
| INPUT-03 | `hold_to_talk`: the input MUST be live only while M is held, and muted otherwise: from the moment this mode is configured, after a restart and while Apptrol is stopped. A mute or unmute made outside Apptrol MUST be undone (unlike MUTE-07). A saved M mute of the control MUST be dropped. | MUST |
| INPUT-04 | Talk-over is active while S in mode `talk_over` is held, and, with `talk_over = true` on M, while the input is live through M (`hold_to_talk`: M held; `mute`: not muted with M; a cough does not end it). While it is active, every app target of the layout MUST go down to the input's `talk_over_volume` (CFG-16), but never up: an app below it stays where it is. With talk-over active from several inputs, the lowest volume applies. An app whose control position is unknown (PRIO-04) MUST be muted instead, as its volume is unknown. When talk-over ends, every app MUST return to its control's position, and an app muted by talk-over MUST be unmuted unless it is muted otherwise (MUTE-04). Inputs are not changed. | MUST |
| INPUT-05 | Held states MUST be kept apart from the M mute, like solo (MUTE-04): an input is muted when it is muted with M (mode `mute`), or coughing, or in `hold_to_talk` and M is not held. | MUST |
| INPUT-06 | A control moved during talk-over MUST have its position remembered and applied when talk-over ends; a stream appearing during talk-over MUST get the lower of the talk-over volume and its control's position. | MUST |
| INPUT-07 | A held state (M in `hold_to_talk`, S in `cough` or `talk_over`) MUST end when its button is released, when the controller disconnects, when a configuration reload changes the button's mode or the column's input, and when Apptrol stops. Held states MUST NOT be saved. | MUST |
| INPUT-08 | Apptrol MUST act on the release message (value 0) of M and S on input columns; the controller's buttons must be set to Momentary (HW-01). | MUST |

## 5. Non-functional requirements

| ID | Requirement | Level | Phase |
|---|---|---|---|
| NFR-01 | Apptrol MUST run on Linux with PipeWire (through `pipewire-pulse`). It SHOULD also work on plain PulseAudio. | MUST | 1 |
| NFR-02 | The service MUST NOT depend on a specific desktop environment. It MUST work on KDE Plasma and GNOME, on Wayland and X11. | MUST | 1 |
| NFR-03 | Apptrol MUST be written in Go and ship as a single binary without runtime dependencies (no C libraries; the controller is read through the kernel's raw MIDI device). | MUST | 1 |
| NFR-04 | CPU usage while idle SHOULD be close to 0 %; memory use SHOULD stay below 30 MB. | SHOULD | 1 |
| NFR-05 | Releases MUST be built by CI and published with binaries for x86_64 and arm64, .deb and .rpm packages (including the systemd user unit) and checksums. | MUST | 0 |
| NFR-06 | The project MUST be published under the MIT license. | MUST | 0 |
| NFR-07 | Documentation MUST NOT use Korg trademarks in the project name or logo, and MUST state that the project is not affiliated with Korg. | MUST | 0 |

### 5.1 Quality assurance

How these are met is described in [ADR 0012](adr/0012-testing-strategy.md) and
[testing.md](testing.md).

| ID | Requirement | Level | Phase |
|---|---|---|---|
| QA-01 | Every push and pull request MUST run CI: format check, `go vet`, `golangci-lint`, tests with the race detector, vulnerability scan, and a build with a smoke test. | MUST | 0 |
| QA-02 | Changes SHOULD only reach `main` when CI passes. Once the repository is public, this MUST be enforced with branch protection. | SHOULD | 0 |
| QA-03 | Tests MUST run against the minimum Go version from `go.mod` and against the latest stable Go release. | MUST | 0 |
| QA-04 | CI MUST fail when total test coverage drops below the configured minimum (currently 75 %). Logic packages (matching, mixer state, config, saved state) SHOULD reach at least 85 %. | MUST | 0 |
| QA-05 | Dependencies MUST be checked with `govulncheck`; a known vulnerability in code Apptrol actually calls MUST fail CI. | MUST | 0 |
| QA-06 | Access to the controller, to the audio server and (from Phase 1.5) to desktop services over D-Bus MUST go through interfaces, so that all behaviour can be tested with fakes, without hardware, PipeWire or a desktop session. | MUST | 1 |
| QA-07 | Every MUST requirement that can be tested without hardware MUST have at least one automated test. Test names SHOULD include the requirement ID. *(Until 0.1.x: Phase 1 requirements.)* | MUST | 1 |
| QA-08 | Configuration parsing and MIDI message decoding MUST have fuzz tests; CI SHOULD run each for a short time on every push. | MUST | 1 |
| QA-09 | Integration tests against a real, headless PipeWire SHOULD run in CI. They run only when `APPTROL_PULSE_TEST=1` is set. | SHOULD | 1 |
| QA-10 | Automated tests (except integration tests) MUST NOT need network access, hardware or a desktop session. | MUST | 0 |
| QA-11 | A bug fix SHOULD include a test that fails without the fix. | SHOULD | 1 |
| QA-12 | Before each release, the manual hardware checklist in [testing.md](testing.md) MUST be completed on a real controller. | MUST | 1 |
| QA-13 | Integration tests against a private D-Bus session bus (`dbus-run-session`) SHOULD run in CI. They run only when `APPTROL_DBUS_TEST=1` is set. | SHOULD | 1.5 |

## 6. Later phases (outline)

These are agreed directions, not yet full requirements. They will be refined and given IDs
before the phase starts. See the [roadmap](roadmap.md).

### Phase 1.5 — Media, launcher and column buttons
Specified in full: sections 4.11–4.14 and the requirements marked *Phase 1.5* (CFG-13 to
CFG-17, LOG-15, LOG-16), with changes to SOLO-07, LED-03, LED-04, LED-06, BTN-01 and LOG-10.
Desktops that do not pass the display to systemd user services (e.g. some Hyprland and
Sway setups) need one line in their config so started apps can open windows; the README
explains it with the feature.

### Phase 1.6 — On-screen display
- On-screen feedback when a volume or mute changes: KDE's native volume OSD when available, a desktop notification elsewhere (e.g. GNOME).
- A desktop notification when something needs attention, e.g. an invalid configuration or the controller unplugged (ADR 0021).

### Phase 2 — Layouts
- Several layouts (e.g. *Work*, *Gaming*), each with its own assignments.
- Every layout keeps its own positions, user mutes and solo state, and restores them when it becomes active. Switching layouts does not change the state of apps that are not part of the new layout.
- Per layout, what happens on switch: set controls to a **fixed value** (a layout-wide default with optional per-control overrides), restore **this layout's last values**, or **carry over** the physical positions from the previous layout.
- **Pick-up** (as on the GoXLR): after a switch, a control does nothing until its physical position crosses the target's current level. The M LED of a waiting column blinks.
- Track ◀ / ▶ step through layouts; Cycle + S / M / R on a column jumps directly to a layout (up to 24).
- Optional automatic switching (e.g. when a game starts).

### Phase 3 — Setup and tray *(ADR 0021; no desktop GUI)*
- `apptrol setup`: a terminal interface in the same binary that shows the layout as the controller's columns and lets you assign apps (picked from what is playing now) and set the buttons.
- Every change is checked like `apptrol check` and saved to `config.toml`, keeping its comments and layout; the file stays the only place settings live (Q-6), and the service reloads it.
- A tray icon in the service, showing status only: running and fine, or needing attention (the tooltip names the problem); no icon means Apptrol is not running. A small menu opens the configuration file, the log, or `apptrol setup` in a terminal.
- The tray icon appears wherever the desktop has a tray (StatusNotifierItem); desktops with only an XEmbed tray (e.g. i3bar) and GNOME without the AppIndicator extension show none, and nothing depends on it.

### Later (unscheduled)
- A man page and shell completions (bash, zsh, fish) in the packages, written without a command-line framework (ADR 0021).

### Backlog (unscheduled)
- **Move app to another output**, as a further R override: cycle an app through a per-app list of allowed output devices; R LED shows when the app is not on its home output. Needs further discussion.
- Recording control beyond starting an app (e.g. start/stop recording in OBS via its WebSocket API, with the Record LED showing the state).
- Read the controller's LED mode over SysEx (read-only) and log a **warning** only when it is "Internal", replacing the HW-03 hint.
- Support for other MIDI controllers.

## 7. Open questions

| # | Question | Phase |
|---|---|---|
| Q-1 | When switching from a layout where an app is user-muted to a layout that does not contain that app, should the app stay muted or become audible until you switch back? | 2 |
| Q-2 | Should R also cycle an input column between input devices? | Backlog |
| Q-3 | Should an output move made with R persist after the app restarts, and is it per layout or global? | Backlog |
| Q-4 | How does the GUI talk to the service (D-Bus or a local socket)? **Decided 2026-10-02: D-Bus** (session bus), recorded in ADR 0017. **No longer applies** since ADR 0021: there is no GUI program. | 3 |
| Q-5 | Should the journald output use the native journal protocol (structured fields) instead of stdout with severity prefixes? Phase 1 uses severity prefixes. | Backlog |
| Q-6 | Where do settings live when they can be changed outside the file? **Decided 2026-10-02: in `config.toml` only** (ADR 0021): `apptrol setup` edits the file, keeping its comments; nothing overrides it. Open: how to write the file without losing comments. | 3 |
