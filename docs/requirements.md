# Apptrol — Requirements

| | |
|---|---|
| **Status** | Phase 1 (0.1.0) and Phase 1.5 (0.2.0) released; output volume and separate configuration tables specified for Phase 1.6 (0.3.0); later phases outlined |
| **Last updated** | 2026-10-15 |
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
| **Target** | What a control acts on: an *app* (one or more playback streams), an *input* (a capture device such as a microphone) or, from Phase 1.6, an *output* (a playback device such as speakers, headphones or a channel of an audio interface). |
| **Default output** | The output the audio server plays new sound on, as chosen in the desktop's volume applet. |
| **Monitor** | A capture device that records what an output plays, named `<output>.monitor`. Never an input or an output for Apptrol. |
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
popups, multiple layouts, pick-up, a terminal setup and a tray icon. See section 6 and
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
| HW-08 | Within 5 seconds of the controller's device appearing (plugged in while Apptrol runs), a "permission denied" when opening it MUST be logged at debug and retried every 100 ms, as access is granted a moment later. The same MUST apply within 5 seconds of Apptrol starting: a second user's Apptrol starts with their login, before the system gives them the rights. Afterwards it MUST be logged as the error of HW-06's kind (`controller_permission_denied`). The debug record MUST name the moment it follows (`apptrol.controller.grace`). *(Until 0.2.0: always an error. Until 0.2.1: an error at once if the device was present at start.)* | MUST | 1 |

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
[0019](adr/0019-launcher-and-column-buttons.md),
[0020](adr/0020-microphone-column-buttons.md) and
[0022](adr/0022-launcher-command-safety.md). Section 4.15 and the requirements marked
*Phase 1.6* are planned for 0.3.0, designed in ADRs
[0026](adr/0026-separate-tables-for-apps-inputs-outputs.md) and
[0027](adr/0027-output-volume.md); until they are built, the other requirements describe
the released behaviour. The requirements marked *0.2.1* come with that release, designed
in [ADR 0029](adr/0029-controller-follows-the-user-in-front.md).

### 4.1 Controls and volume

| ID | Requirement | Level |
|---|---|---|
| CTRL-01 | Each slider and each knob MAY be assigned one target in the active layout. Unassigned controls MUST do nothing. | MUST |
| CTRL-02 | Moving a control MUST set the volume of its target to the control's position, mapped linearly from 0–127 to 0 % – the target's `max_volume` (CTRL-03), in the audio server's displayed percentage scale, not linear amplitude. For outputs this is the loudest channel's percentage, preserving the other channels' proportions (OUT-14); apps and inputs continue to set every channel equally. | MUST |
| CTRL-03 | Volume MUST stay between 0 % and the app's `max_volume` (default 100 %). `max_volume` MAY be set per app from 1 to 150 %; the control then spans 0 to that value, so it also works as a cap below 100 %. | MUST |
| CTRL-04 | For an **app** target, the volume MUST be applied to every playback stream that matches the app, including several streams of the same program (e.g. several browser tabs). | MUST |
| CTRL-05 | For an **input** target, the volume MUST be applied to the matching capture device. | MUST |
| CTRL-06 | Apps, inputs and outputs that are not assigned to any control MUST NOT be touched (outputs: OUT-01). | MUST |
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
| SOLO-03 | Solo MUST NOT affect input or output targets (OUT-07). | MUST |
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
| LED-06 | Transport button LEDs MUST be off, except the ▶ LED, lit while the media-key player plays (MEDIA-06), and the Record LED's flash when it starts an app (LAUNCH-08). The ▶ LED follows the same 500 ms pause rule as R (MEDIA-09, ADR 0033). *(Until 0.1.x: always off.)* | MUST |
| LED-07 | LEDs MUST be re-sent whenever the controller (re)connects, the audio server (re)connects, and the configuration or state changes. After a controller connect they MUST be sent again once the controller has started up (it ignores LED messages for a moment after being plugged in); after an audio server connect, again 2 seconds later (a PipeWire restart can reset the controller's LEDs). Taking the controller back after another user was in front counts as a controller connect (SVC-09). | MUST |
| LED-08 | When Apptrol stops, it SHOULD turn all LEDs off, so no LED shows a state that no longer applies. | SHOULD |
| LED-09 | When the computer wakes from sleep or hibernation, Apptrol MUST send every LED again, and again once the controller has started up (it loses power during sleep and starts with its LEDs off). Apptrol learns of the wake-up from systemd-logind on the D-Bus system bus; without a system bus it MUST log a warning, keep working and reconnect (ADR 0023). | MUST |

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
| STATE-08 | While no valid configuration is loaded (invalid, unreadable or missing at start), the saved state MUST stay unchanged, except for the positions of controls moved meanwhile, and MUST be applied when the first valid configuration arrives; only then does STATE-07 apply. An invalid input must never cause a write (ADR 0025). *(Until 0.2.0: a start with an invalid configuration erased the saved state.)* | MUST |

### 4.8 Configuration

| ID | Requirement | Level |
|---|---|---|
| CFG-01 | Configuration MUST be a TOML file at `$XDG_CONFIG_HOME/apptrol/config.toml` (default `~/.config/apptrol/config.toml`). A different path MAY be given with `--config`. | MUST |
| CFG-02 | Apps, inputs and outputs MUST each be defined once, in `[apps.<id>]`, `[inputs.<id>]` and `[outputs.<id>]` respectively (CFG-18); layouts MUST refer to them by id. | MUST |
| CFG-03 | Assignments MUST live in a `[layouts.<name>]` table. Phase 1 MUST use the layout named `default`; other layouts MUST be accepted and ignored with an info log. | MUST |
| CFG-04 | An app MUST be matched by a list of case-insensitive substrings compared with the stream's `application.name` and `application.process.binary`. | MUST |
| CFG-05 | An input (`[inputs.<id>]`) MUST be matched by a case-insensitive substring of the capture device's name or description. | MUST |
| CFG-06 | Apptrol MUST reload the configuration automatically when the file is saved, without restarting. | MUST |
| CFG-07 | An invalid configuration MUST be rejected with an error log naming the problem; the previous valid configuration MUST stay active. If there is no valid configuration at start, Apptrol MUST start, log the error and wait for a valid file. | MUST |
| CFG-08 | Validation MUST reject: unknown control names, references to undefined apps, inputs or outputs, the same target assigned to more than one control in a layout, and empty match lists. Settings of an app, input or output placed directly under a layout MUST be identified as such, with a hint naming the table they belong in (`[apps.<id>]`, `[inputs.<id>]`, `[outputs.<id>]`). | MUST |
| CFG-09 | If no configuration file exists, Apptrol SHOULD create a commented example file and log where it is. | SHOULD |
| CFG-10 | A command `apptrol list` SHOULD print the currently playing streams, capture devices and output devices (OUT-12) with the names used for matching and the control each app, input and output is on, to help write the config. | SHOULD |
| CFG-11 | A command `apptrol check` SHOULD validate the configuration file and exit. | SHOULD |
| CFG-12 | When two apps of the active layout can match the same streams (or input devices), because one match fragment contains another, Apptrol SHOULD warn and name the app that gets them: the one on the first control in the order slider1–slider8, knob1–knob8. | SHOULD |
| CFG-13 | *Phase 1.5.* Buttons MUST be configured per layout in `[layouts.<name>.buttons]`, with the names `record`, `marker_set`, `marker_prev`, `marker_next`, `r1`–`r8`, `s1`–`s8` and `m1`–`m8`. A value is a launcher (`app` = a desktop ID, or `command` = a list of arguments; optional `if_running`) or a `mode`; an `m` button MAY also set `talk_over = true` (INPUT-04). | MUST |
| CFG-14 | *Phase 1.5.* Validation MUST reject, with one record per problem: a launcher with both `app` and `command` or neither, an unknown button name, an unknown `mode` or `if_running` value, a mode that does not fit the button or column (`play_pause` on an input or output column; `mute` or `hold_to_talk` on anything but M; `cough` or `talk_over` as a mode on anything but S; `cough` together with `hold_to_talk`), an `m` or `s` setting on a column without an input (an entry of `[inputs]`), `talk_over = true` on anything but an `m` button, and a launcher on an M or S button. | MUST |
| CFG-15 | *Phase 1.5.* `[media] player` MAY name an app from `[apps]` to pin the media keys to (MEDIA-06); an id that is not in `[apps]`, such as an input or output, MUST be rejected. | MUST |
| CFG-16 | *Phase 1.5.* An input (`[inputs.<id>]`) MAY set `talk_over_volume`, the volume in percent (0–100) that apps go down to during talk-over; default 25. On an app or output, or outside 0–100, it MUST be rejected. | MUST |
| CFG-17 | *Phase 1.5.* The example configuration (CFG-09) MUST document every Phase 1.5 setting: commented out, with an example value, the accepted values and the default. | MUST |
| CFG-18 | *Phase 1.6.* Apps MUST be defined in `[apps.<id>]`, inputs in `[inputs.<id>]` and outputs in `[outputs.<id>]`, without a `type` key. An id MUST be unique over the three tables, as layouts and `[media] player` refer to targets by id alone; a duplicate MUST be rejected, naming both tables. `talk_over_volume` MUST be rejected outside `[inputs]` (ADR 0026). | MUST |
| CFG-19 | *Phase 1.6.* Until 1.0, `[apps.<id>]` with `type = "input"` MUST still be read as an input, and `type = "app"` as an app; each such entry MUST get one warning at every configuration load and in `apptrol check`, naming the table to move it to. Any other `type` value in `[apps]`, and `type` in `[inputs]` or `[outputs]`, MUST be rejected. | MUST |
| CFG-20 | *Phase 1.6.* An output MUST have exactly one of `match` and `device`; both or neither MUST be rejected. `device` MUST be `"default"` and MUST be rejected in `[apps]` and `[inputs]`. | MUST |
| CFG-21 | *Phase 1.6.* A match fragment of an input or output that ends in `.monitor` SHOULD be warned about at every configuration load and in `apptrol check`, saying that a monitor records an output and is never matched; the configuration MUST stay valid. | SHOULD |
| CFG-22 | *Phase 1.6.* The example configuration and `docs/config.md` MUST explain how to find the names of inputs and outputs (`apptrol list`; `pactl list short sources` and `pactl list short sinks` without Apptrol) and how `match` works for devices: a case-insensitive part of the name or description, the full name when a short part fits several devices, the first match used with a warning, monitors never matched. | MUST |
| CFG-23 | The example configuration and `docs/config.md` MUST warn about hearing safety where `max_volume` is described: above 100 % the sound is amplified in software and can damage hearing, the first move of a control sets its volume at once, 100 or less is recommended, and the user is responsible for the volumes they set (the software comes without warranty). From Phase 1.6 this also covers outputs, where nothing limits the volume after Apptrol. | MUST |
| CFG-24 | *0.2.1.* `[controller] at_login_screen` MUST accept `"keep"` (default) and `"release"` (SVC-10); any other value MUST be rejected. A reload MUST apply it at once. | MUST |

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
| LOG-10 | Every log record MUST carry `apptrol.component`: `service`, `config`, `state`, `audio`, `controller`, `desktop`, `session`, `launcher` or `mixer` (ADR 0016). *(Until 0.1.x: without `desktop`, `session` and `launcher`.)* | MUST |
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
| SVC-03 | If the controller is not connected, Apptrol MUST keep running, wait for it, and pick it up when it is plugged in. Unplugging MUST be handled the same way. While another user's session is in front, Apptrol does not look for it (SVC-09). | MUST |
| SVC-04 | If the connection to the audio server is lost (e.g. PipeWire restart), Apptrol MUST reconnect and re-apply the current state. | MUST |
| SVC-05 | Apptrol MUST shut down cleanly on SIGTERM/SIGINT, saving state first. | MUST |
| SVC-06 | `apptrol --version` MUST print the version, commit and build date. | MUST |
| SVC-07 | On shutdown, Apptrol MUST end solo and unmute every app it silenced by solo. The audio server remembers mutes per app, so otherwise those apps would stay muted after Apptrol exits. User mutes (M) stay. Both are saved first and restored on the next start (STATE-03, STATE-04). | MUST |
| SVC-08 | *0.2.1.* Apptrol MUST hold the controller unless a session of another user is in front at seat `seat0` (logind's `Seat.ActiveSession` and that session's `User` and `Class`). A session of the same user in front, graphical or not, and no session in front MUST NOT count as another user (ADR 0029). | MUST |
| SVC-09 | *0.2.1.* When another user's session comes to the front, Apptrol MUST turn every LED off, close the controller and stop looking for it; held states end (INPUT-07). When that is no longer so, it MUST look for the controller at once and connect as after plugging it in (LED-07). | MUST |
| SVC-10 | *0.2.1.* A login screen (session class `greeter`) in front MUST count as another user with `at_login_screen = "release"`. With `"keep"` it MUST change nothing: the controller stays with whoever had it, so an Apptrol that let go for another user takes it back only when its own user, or nobody, is in front (CFG-24). The lock screen of the user's own session is not a login screen. | MUST |
| SVC-11 | *0.2.1.* For 5 seconds after taking the controller back, and *(0.2.2)* for 5 seconds after Apptrol starts, "busy" (the other user's Apptrol has not let go yet) and "permission denied" (the system has not given this user the rights yet) when opening it MUST be logged at debug and retried every 100 ms; afterwards HW-06 and HW-08 apply. | MUST |
| SVC-12 | *0.2.1.* Without a system bus, or while the seat cannot be read, Apptrol MUST hold the controller whenever it is free, as before, and log a warning once (`seat_unreadable`). Every release and take-back MUST be logged at info with its reason (`apptrol.seat.front`), as in ADR 0029 (point 9). | MUST |
| SVC-13 | *0.3.0.* While running under systemd, Apptrol MUST keep the unit's Status line (`STATUS=` over sd_notify, shown by `systemctl --user status apptrol`) up to date: always the version first, then the state: `controller not found`, `controller connected · N controls assigned`, `controller released (another user in front)`, `waiting for a valid configuration` or `stopping`. An active app solo MUST replace `N controls assigned` with `solo on slider N (<configured app name>)`, and MUST also be appended when the controller is missing or released. The name MUST be followed by `not playing` when neither a matched uncorked stream nor a matched MPRIS player reporting `Playing` exists (the definition shared with #101). The text MUST follow solo on/off/move, saved-solo restoration, configuration changes and stream/player playback changes, without the LED pause delay. Waiting for valid configuration and stopping MUST retain their short texts. The mixer MUST expose only read-only facts; the service builds the text (ADR 0015, ADR 0007 note of 2026-10-25, #168). A text MUST be sent only when it changed, and sending MUST NOT hold up the event loop or the shutdown: systemd's socket can block, so a sender goroutine writes and shutdown waits at most 200 ms for the last text. Without `NOTIFY_SOCKET` (started in a terminal) nothing MUST be sent, and a failure to send MUST NOT stop Apptrol. The unit MUST NOT wait for a ready message (`Type=simple` with `NotifyAccess=main`, ADR 0007), and its `Description=` is Apptrol's one-line summary, the same as the AppStream summary (#99). | MUST |

### 4.11 Desktop connection *(Phase 1.5)*

| ID | Requirement | Level |
|---|---|---|
| DESK-01 | Apptrol MUST connect to the D-Bus session bus at start; every feature that needs D-Bus MUST share this connection, except calls to systemd, which go through go-systemd (ADR 0017), and the wake-up signal from the system bus (LED-09, ADR 0023). | MUST |
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
| MEDIA-09 | The R LED of an app column in mode `play_pause` MUST be lit while one of the column's app's players reports `Playing`; otherwise off. It MUST follow status changes and players appearing or disappearing, except that a pause MUST darken it only after the player has stayed `Paused` for 500 ms (a pause started by R or ▶ MUST darken it at once), so the short pauses of a browser video seeked with the mouse do not blink it; lighting up MUST stay immediate (ADR 0033). In mode `off` or as a launcher, it MUST be off. *(Until 2026-10-02 planned to follow the corked state of the app's streams; see ADR 0018, notes. Until 0.3.0 every pause darkened it at once.)* | MUST |
| MEDIA-10 | The audio adapter MUST report whether each stream is corked, and every change of it. | MUST |
| MEDIA-11 | While the screen state is `inactive` (*Switch user*, the login screen in front with `at_login_screen = "keep"`: the session has lost the speakers), ▶, ■, ◀◀, ▶▶ and R in mode `play_pause` MUST send nothing to players. Each press MUST be logged as one warning, `media button pressed while this session is not in front; nothing sent` with the button and the screen state, also when there is no player or the pinned app has none; it replaces LAUNCH-15's warning for that press. The state turning `inactive`, and leaving it, MUST each be logged once at info. Sliders, knobs, M, S and the microphone buttons keep working, and the ▶ and R LEDs keep following the players. While `locked` or `unknown`, the media buttons and R work as before. *(0.3.0, ADR 0030.)* | MUST |

### 4.13 Launcher buttons *(Phase 1.5)*

| ID | Requirement | Level |
|---|---|---|
| LAUNCH-01 | Record, the Marker buttons and any R button set up as a launcher (CFG-13) MUST start their app when pressed. Without a launcher, Record and the Marker buttons do nothing (debug log). | MUST |
| LAUNCH-02 | A desktop ID MUST be resolved as the Desktop Entry and Base Directory specifications say: `applications/` under `$XDG_DATA_HOME`, then each `$XDG_DATA_DIRS` entry; the first file found wins. A file with `Hidden=true` counts as not installed. | MUST |
| LAUNCH-03 | `Exec` MUST be parsed as the Desktop Entry specification says: quoting and escapes, `%f %F %u %U` removed, `%c` replaced by the app's name, `%i` and `%k` dropped. A file with `DBusActivatable=true` and no `Exec` MUST be started through `org.freedesktop.Application.Activate`. | MUST |
| LAUNCH-04 | An app MUST be started by systemd as a transient user unit (`app-apptrol-<desktop-id>@<random>.service`), never as a child process of Apptrol. It MUST keep running when Apptrol stops or restarts; its unit is removed after it exits. | MUST |
| LAUNCH-05 | A `command` MUST be run as a list of arguments, without a shell; `~/` at the start of an argument MUST be expanded. | MUST |
| LAUNCH-06 | With `if_running = "start"` (the default) the app MUST be started on every press; with `"skip"`, not if it already runs (LAUNCH-07). | MUST |
| LAUNCH-07 | Whether an app runs MUST be checked when the button is pressed: first the user manager's units for its desktop ID (`app-*<id>-*.scope`, `app-*<id>@*.service`, and `snap.<snap>.<app>-*.scope` for a Snap ID), then the running processes by the program name from `Exec` or `command`. Only active units count, and the desktop ID MUST be matched both as written and as systemd escapes it (`-` as `\x2d`). Steam games (`steam steam://…`) MUST be passed to Steam unchecked; for apps started through a wrapper (`flatpak`, a shell, `env`, `python`, …) only the units are checked. A check that fails MUST be logged, not taken as "not running". | MUST |
| LAUNCH-08 | When Record starts an app, its LED MUST flash briefly (about 0.3 s). | MUST |
| LAUNCH-09 | `apptrol list apps [search]` SHOULD print the desktop ID, name and source (`system`, `flatpak`, `snap`, `user`) of every installed app, without `Hidden` or `NoDisplay` ones; the search is case-insensitive over ID and name. | SHOULD |
| LAUNCH-10 | `apptrol check` and every configuration load SHOULD warn about desktop IDs that are not installed; this MUST NOT make the configuration invalid. | SHOULD |
| LAUNCH-11 | An app that cannot be started MUST be logged as an error (`app_start_failed`); Apptrol keeps running. | MUST |
| LAUNCH-12 | Validation MUST reject a launcher `command` that deletes everything (`rm` with `-r` and `-f` on `/`, `/*`, `~`, `$HOME` or `/home`), wipes a disk (`mkfs*`, `wipefs`, `dd` to `/dev/…`, writing to a disk device), is a fork bomb, runs a download (`curl`/`wget` piped to a shell), changes rights on everything (`chmod -R`/`chown -R` on `/`), or uses `sudo`, `su` or `doas`; also after wrappers and inside `sh -c` text. The error MUST name the group and point to docs/config.md. Desktop IDs are not checked. It is a safety net against accidents, not security (ADR 0022). | MUST |
| LAUNCH-13 | A launcher MUST start its app while the user's graphical session is unlocked and in front: logind reports it with `LockedHint` false and `Active` true. While it is locked, or another session is in front, a launcher MUST start nothing unless it has `when_locked = true` (LAUNCH-14). Whenever this cannot be told (no system bus, no graphical session, a lost connection, before the first answer), no launcher may start, whatever the configuration, and the press MUST be logged. Every other control works whatever the screen state (ADR 0024), except the media buttons and R while `inactive` (MEDIA-11, ADR 0030), as long as Apptrol holds the controller: while another user's session is in front, it does not (SVC-08), so `inactive` is only seen at the login screen with `at_login_screen = "keep"`. *(Until 0.2.0: Apptrol kept the controller while another user's session was in front.)* | MUST |
| LAUNCH-14 | `when_locked = true` on a launcher (only there; default `false`) MUST let it start its app also while the screen is locked or another session is in front. Every configuration load and `apptrol check` MUST warn about each launcher that has it. | MUST |
| LAUNCH-15 | While the screen is locked or another session is in front, every press of a button on the controller MUST be logged as one warning: a launcher with whether it started, a media button or R in mode `play_pause` while `inactive` with MEDIA-11's warning that nothing was sent, any other button with its name. Releases, sliders and knobs are not reported, and nothing is reported while the screen state is unknown. While another user's session is in front, Apptrol does not hold the controller (SVC-08), so no presses reach it. | MUST |

### 4.14 Input column buttons *(Phase 1.5)*

| ID | Requirement | Level |
|---|---|---|
| INPUT-01 | M, S and R on an input column MUST each act in a mode (CFG-13). M: `mute` (default) or `hold_to_talk`, with the option `talk_over`. S: `cough` (default; `off` with `hold_to_talk`), `talk_over` or `off`. R: `off` (default) or a launcher. | MUST |
| INPUT-02 | `cough`: the input MUST be muted while S is held. | MUST |
| INPUT-03 | `hold_to_talk`: the input MUST be live only while M is held, and muted otherwise: from the moment this mode is configured, after a restart and while Apptrol is stopped. A mute or unmute made outside Apptrol MUST be undone (unlike MUTE-07). A saved M mute of the control MUST be dropped. | MUST |
| INPUT-04 | Talk-over is active while S in mode `talk_over` is held, and, with `talk_over = true` on M, while the input is live through M (`hold_to_talk`: M held; `mute`: not muted with M; a cough does not end it). While it is active, every app target of the layout MUST go down to the input's `talk_over_volume` (CFG-16), but never up: an app below it stays where it is. With talk-over active from several inputs, the lowest volume applies. An app whose control position is unknown (PRIO-04) MUST be muted instead, as its volume is unknown. When talk-over ends, every app MUST return to its control's position, and an app muted by talk-over MUST be unmuted unless it is muted otherwise (MUTE-04). Inputs are not changed. | MUST |
| INPUT-05 | Held states MUST be kept apart from the M mute, like solo (MUTE-04): an input is muted when it is muted with M (mode `mute`), or coughing, or in `hold_to_talk` and M is not held. | MUST |
| INPUT-06 | A control moved during talk-over MUST have its position remembered and applied when talk-over ends; a stream appearing during talk-over MUST get the lower of the talk-over volume and its control's position. | MUST |
| INPUT-07 | A held state (M in `hold_to_talk`, S in `cough` or `talk_over`) MUST end when its button is released, when the controller disconnects or is released for another user (SVC-09), when a configuration reload changes the button's mode or the column's input, and when Apptrol stops. Held states MUST NOT be saved. | MUST |
| INPUT-08 | Apptrol MUST act on the release message (value 0) of M and S on input columns; the controller's buttons must be set to Momentary (HW-01). | MUST |

### 4.15 Output targets *(Phase 1.6)*

Designed in [ADR 0027](adr/0027-output-volume.md); fixed-output ownership updated
by [ADR 0035](adr/0035-first-control-owns-matched-output.md). Output balance
(OUT-14 to OUT-16), designed in [ADR 0039](adr/0039-output-volume-keeps-channel-balance.md),
is implemented for 0.3.0 (#156).

| ID | Requirement | Level |
|---|---|---|
| OUT-01 | A control assigned an output MUST set the output device's volume, mapped as for every target up to its `max_volume` (CTRL-02, CTRL-03), preserving its channel balance (OUT-14). Stream volumes MUST NOT be changed; outputs on no control MUST NOT be touched. | MUST |
| OUT-02 | An output with `match` MUST be matched by a case-insensitive part of the output device's name or description; monitors MUST NOT be matched. When several devices match, the first by name MUST be used and a warning MUST name all of them; the chosen device MUST be kept while it still matches. | MUST |
| OUT-03 | An output with `device = "default"` MUST act on the audio server's default output and follow every change of it at once. The new default MUST keep its own volume until the control is moved; the control's mute MUST apply to it at once. The previous default MUST keep its volume and MUST be unmuted if Apptrol muted it and no other control holds it. Without a default output, the control MUST do nothing. Only a move of the control MUST set the default output's volume: not a connect, reload or change of the default (ADR 0038). A device that appears and becomes the default within 3 seconds MUST get the control's mute defended as OUT-05 says, also when it first appeared with no owner; a volume an earlier owner set on it MUST NOT be defended once the default control holds it (ADR 0038 point 6). | MUST |
| OUT-04 | When fixed output targets choose the same device, the first control (sliders 1–8, then knobs 1–8) MUST own it. Later conflicting controls MUST NOT change its volume or mute; their M presses MUST NOT change saved mute. The conflict MUST be logged at warning when it begins and at reload, identifying the device, winning control and ignored control. Actual matched devices, not target display names or equality of match lists, decide conflicts (ADR 0035). The same rule MUST decide when the default output (`device = "default"`) is a device a fixed output holds; that overlap is allowed, so it MUST be logged at info instead, when it begins and at reload (ADR 0038). | MUST |
| OUT-05 | PRIO-01, PRIO-02 and MUTE-07 MUST apply to outputs as to apps: outside volume changes are not reverted, outside mutes are taken over. A device that appears and matches a fixed output MUST get the control's known position and mute. A volume or mute the audio server restores on the device afterwards MUST NOT count as an outside change: within 3 seconds of the device appearing, Apptrol MUST set the owning control's volume and mute again (as PRIO-03 and MUTE-07 do for streams). Within those 3 seconds, a mute change on a device no control holds MUST NOT be taken over by a control that gets the device afterwards (ADR 0027 note of 2026-10-26). | MUST |
| OUT-06 | M on an output column MUST toggle the output's mute (MUTE-01); the mute is saved per control (STATE-01). An output on a knob MUST only be silenced by an outside mute. | MUST |
| OUT-07 | Outputs have no solo: S on an output column MUST do nothing, and a press MUST be logged at debug. App solo MUST NOT touch outputs (SOLO-03). *(Planned until 2026-10-23: S solos the output; withdrawn by ADR 0036.)* | MUST |
| OUT-08 | *Withdrawn (ADR 0036).* Was: output solo saved and restored like app solo, and ended on shutdown. | – |
| OUT-09 | R on an output column MUST be `off` (default) or a launcher; `play_pause` MUST be rejected. R MUST NOT change the output or the default output. | MUST |
| OUT-10 | On an output column, the M LED MUST be lit while the control's output is muted, with M or by a mute taken over from outside Apptrol (LED-02); S and R MUST be off. A control whose device an earlier control owns MUST NOT light M (OUT-04, ADR 0035). *(Until 2026-10-23: S was to be lit while soloed; ADR 0036.)* | MUST |
| OUT-11 | Talk-over (INPUT-04) MUST NOT change outputs. | MUST |
| OUT-12 | `apptrol list` SHOULD print the output devices like the input devices: description, name, the control each is on, and `*` on the default output. Monitors MUST NOT be listed as inputs or outputs. | SHOULD |
| OUT-13 | Every output decision MUST be logged with its reason (LOG-15): a device matched (info, with `apptrol.device.selected_by` `match` or `default`), several or no devices matching (warning), the default output changing and keeping its volume (info), no default output (info), conflicting fixed controls on one device (warning, OUT-04), the default output on a device another control holds (info, OUT-04), devices on no control (debug); records as in ADR 0027 (point 12), updated for fixed-output ownership by ADR 0035 and for the default output by ADR 0038. | MUST |
| OUT-14 | Every output volume write, on a slider or knob and on a fixed or default output, MUST preserve the proportions of its channels in the server's displayed percentage scale: `requested × channel / loudest`, rounded to protocol units. The loudest channel MUST follow the control's requested percentage and no channel MUST exceed it. This MUST always apply, without a setting, to mono, stereo and multichannel outputs, including boost and silent channels. The output percentage in `apptrol list` MUST be the maximum channel percentage. Apps and inputs are unchanged (ADR 0039). | MUST |
| OUT-15 | Output balance MUST come from the server's channels and channel positions on connect, appearance and subsequent reads, subject to OUT-16. The adapter MUST retain the last usable nonzero balance for a live device and layout through zero volume, without drift from its own rounded writes or echoes. If all channels are zero and no balance is known, the next nonzero write MUST use equal channels. Removal and reconnect MUST discard cached balance; a new channel layout MUST NOT reuse old channel positions. Balance MUST NOT be saved to disk or carried between devices by a control, and mute MUST NOT change it. Outside changes after the guard window MUST stay and supply the balance for the next move (PRIO-02). | MUST |
| OUT-16 | Within OUT-05's three-second window, once a volume is requested, the output guard MUST defend the requested maximum-channel level while accepting the server's usable channel balance, including late restores and deliberate applet changes. Notifications and the 200 ms read-back MUST use the same path: retain ratios for own rounded echoes or all-zero reads (OUT-15), otherwise accept the read's balance, then correct the maximum to the requested level if it differs by more than 0.5 percentage points. Corrections MUST scale the latest balance; a balance-only change at the requested maximum MUST be kept. The latest move MUST replace the requested level. Generation-tagged ownership, release and `ForgetOutputVolume` MUST retain their existing effects; a pending ownership answer MUST NOT permit defense. A changed channel layout MUST discard obsolete channel positions and use the new layout's balance for any still-authorized defense. | MUST |

For OUT-05, the defended output volume is its maximum-channel level; server balance
changes are accepted even during the guard window (OUT-16). For OUT-13,
debug records MUST also explain the selected or retained balance and its source,
zero/layout fallbacks, per-channel reads and writes, and guard corrections, as in
ADR 0039's logging table.

OUT-16's same balance and level rules MUST also apply when `KeepOutput` checks
the latest read held while ownership was pending: use its channels and the latest
requested level, not a channel average or a flat write. A stale answer MUST do
nothing, and a keep answer MUST NOT restart an expired volume guard.

For OUT-15, a full resync after event overflow MUST discard cached balance just
like reconnect, even if the device name, server index and channel positions still
match. Nonzero snapshot levels supply fresh ratios; an all-zero snapshot MUST use
equal-channel fallback for the next nonzero write. This is an explicit exception
to retaining balance through zero for a live device (ADR 0039).

## 5. Non-functional requirements

| ID | Requirement | Level | Phase |
|---|---|---|---|
| NFR-01 | Apptrol MUST run on Linux with PipeWire (through `pipewire-pulse`). It SHOULD also work on plain PulseAudio. | MUST | 1 |
| NFR-02 | The service MUST NOT depend on a specific desktop environment. It MUST work on KDE Plasma and GNOME, on Wayland and X11. | MUST | 1 |
| NFR-03 | Apptrol MUST be written in Go and ship as a single binary without runtime dependencies (no C libraries; the controller is read through the kernel's raw MIDI device). | MUST | 1 |
| NFR-04 | CPU usage while idle SHOULD be close to 0 %; memory use SHOULD stay below 30 MB. | SHOULD | 1 |
| NFR-05 | Releases MUST be built by CI and published with binaries for x86_64 and arm64, .deb and .rpm packages (including the systemd user unit), checksums, and a build provenance attestation for every file. The file names MUST be the names `checksums.txt` lists, also for pre-releases. *(Until 0.2.0: no attestations, and pre-release packages named with `~`, which GitHub renames, #74.)* | MUST | 0 |
| NFR-06 | The project MUST be published under the MIT license. | MUST | 0 |
| NFR-07 | Documentation MUST NOT use Korg trademarks in the project name or logo, and MUST state that the project is not affiliated with Korg. | MUST | 0 |
| NFR-08 | Every release archive and package MUST contain the copyright notices and license texts of all libraries compiled into the binary, and of Go's standard library, generated at build time; a library with an unknown or restricted license MUST fail the build. *(Missing in 0.1.0, #33.)* | MUST | 1.5 |
| NFR-09 | The .deb package MUST install a machine-readable DEP-5 copyright file at `/usr/share/doc/apptrol/copyright`, with Apptrol's copyright notice and full MIT license text. CI MUST check it with `lintian` and keep its MIT text equal to `LICENSE` (#146). *(Missing until 0.3.0, #97.)* | MUST | 1.6 |
| NFR-10 | The .deb and .rpm packages MUST have a short summary that is not a sentence (no full stop) and an extended description of what Apptrol does and needs, as software centres show them. CI's lintian run MUST report no informational (`I:`) tags about them (#152). | MUST | 1.6 |
| NFR-11 | OS-specific code MUST stay in adapters, and settings MUST describe user intent rather than OS mechanisms. The portable packages (`internal/mixer`, `internal/config`, `internal/state`, `internal/controller`, `internal/logattr`, `internal/version`) MUST build for Windows with `CGO_ENABLED=0`, checked by `make check` and CI. The config watcher MUST retain Linux's inode check; other systems MUST compare modification time and size (ADR 0013, #93). This does not provide Windows adapters or change paths (#92). | MUST | 1.6 |

### 5.1 Quality assurance

How these are met is described in [ADR 0012](adr/0012-testing-strategy.md) and
[testing.md](testing.md).

| ID | Requirement | Level | Phase |
|---|---|---|---|
| QA-01 | Every push and pull request MUST run CI: format check, `go vet`, `golangci-lint`, tests with the race detector, vulnerability scan, and a build with a smoke test. Since 0.3.0 also the packaging: `goreleaser check`, a snapshot build and `lintian` on the .deb (errors fail) (#146). | MUST | 0 |
| QA-02 | Changes SHOULD only reach `main` when CI passes. Once the repository is public, this MUST be enforced with branch protection. | SHOULD | 0 |
| QA-03 | Tests MUST run against the minimum Go version from `go.mod` and against the latest stable Go release. | MUST | 0 |
| QA-04 | CI MUST fail when total test coverage drops below the configured minimum (currently 75 %). Logic packages (matching, mixer state, config, saved state) SHOULD reach at least 85 %. | MUST | 0 |
| QA-05 | Dependencies MUST be checked with `govulncheck`; a known vulnerability in code Apptrol actually calls MUST fail CI. | MUST | 0 |
| QA-06 | Access to the controller, to the audio server and (from Phase 1.5) to desktop services over D-Bus MUST go through interfaces, so that all behaviour can be tested with fakes, without hardware, PipeWire or a desktop session. | MUST | 1 |
| QA-07 | Every MUST requirement that can be tested without hardware MUST have at least one automated test. Test names SHOULD include the requirement ID. *(Until 0.1.x: Phase 1 requirements.)* | MUST | 1 |
| QA-08 | Configuration parsing and MIDI message decoding MUST have fuzz tests; CI SHOULD run each for a short time on every push. | MUST | 1 |
| QA-09 | Integration tests against a real, headless PipeWire SHOULD run in CI. They run only when `APPTROL_PULSE_TEST=1` is set. | SHOULD | 1 |
| QA-10 | Automated tests (except integration tests) MUST NOT need network access, hardware or a desktop session. | MUST | 0 |
| QA-11 | A bug fix MUST include a test that fails without the fix, named after the requirement it restores. Where no automated test can show the bug (hardware, another program), the fix MUST add a manual test case in [docs/testing/](testing/cases/README.md) instead (QA-14). | MUST | 1 |
| QA-12 | Before each release, the manual hardware checklist in [testing.md](testing.md) MUST be completed on a real controller: all of it for a MINOR release; for a PATCH release, the smoke set and every step that checks a requirement changed since the last release (ADR 0032). | MUST | 1 |
| QA-13 | Integration tests against a private D-Bus session bus (`dbus-run-session`) SHOULD run in CI. They run only when `APPTROL_DBUS_TEST=1` is set. | SHOULD | 1.5 |
| QA-14 | Every new feature or change of behaviour MUST add or update its manual test cases and fixtures in [docs/testing/](testing/cases/README.md), in the format there. A phase MUST NOT be released without test cases for it; each release's run MUST be recorded in `docs/testing/runs/` (ADR 0031). | MUST | 1.5 |
| QA-15 | The command lists for coding agents' tools ([`.claude/settings.json`](https://github.com/Dzobash/apptrol/blob/main/.claude/settings.json) for Claude Code, [`.codex/rules/apptrol.rules`](https://github.com/Dzobash/apptrol/blob/main/.codex/rules/apptrol.rules) for Codex) MUST allow, ask about and block the same commands; pushing and changes on GitHub MUST ask in both, and typed `docker`, `podman` and `buildah` MUST be blocked ([AGENTS.md](https://github.com/Dzobash/apptrol/blob/main/AGENTS.md) › Boundaries, QA-16, ADR 0034). | MUST | 1.6 |
| QA-16 | Every container a coding agent runs MUST run with rootless Podman through the repository's container script, with only the checkout mounted (read-only, plus `.tmp/<run>/` writable) and the script's fixed limits, and MUST leave a record in `.audit/` (recipe and one `runs.log` line). Both command lists MUST block `docker`, `podman` and `buildah` for agents (ADR 0034). CI's Systemd in containers job MUST attempt a per-distro result/time summary even after failure, using only this attempt's audit records; missing or invalid results MUST NOT appear as passes (#196). `make test-systemd RUNS=<n>` (a plain 1–1000, refused otherwise before any container) MUST run n checks per distro in one call under the shared lock, each with its own container, folder and `runs.log` line, and MUST start no new run after the first failure and exit non-zero naming it (#172). INT/TERM sent to the runner or its process group MUST stop dispatch, drain all started runs through normal cleanup and exactly one audit record each, print their summary and release the lock before returning the signal status; repeated signals MUST NOT interrupt draining (ADR 0034, cancellation note). | MUST | 1.6 |
| QA-17 | The `.deb` and `.rpm` that the release build produces SHOULD be installed with `apt` and `dnf` in rootless containers, through the same script and with the same restrictions as QA-16 (no network, so no dependency resolution). The service started from the installed files MUST show the Status line of SVC-13, and the check MUST fail when `dist/` holds no package built from the checked-out commit (ADR 0034, #172, #116). | SHOULD | 1.6 |
| QA-18 | `make test-clean` SHOULD remove the folders and logs that failed container runs keep (`.tmp/run-*`, `.audit/logs/run-*.log`) through the repository's container script, so they do not pile up. It MUST start no container and mount nothing, MUST touch only exact run names directly inside this checkout's `.tmp/` and `.audit/logs/`, MUST NOT follow links (also not one that appears while it waits for the lock), MUST leave a run folder alone that has a mount in it, MUST NOT run while another container run holds the script's lock, and MUST leave `.audit/runs.log` as it is except for appending its own record (`recipe=@clean`) (ADR 0034, #195). | SHOULD | 1.6 |

## 6. Later phases (outline)

These are agreed directions, not yet full requirements. They will be refined and given IDs
before the phase starts. See the [roadmap](roadmap.md).

### Phase 1.5 — Media, launcher and column buttons
Specified in full: sections 4.11–4.14 and the requirements marked *Phase 1.5* (CFG-13 to
CFG-17, LOG-15, LOG-16), with changes to SOLO-07, LED-03, LED-04, LED-06, BTN-01 and LOG-10.
Desktops that do not pass the display to systemd user services (e.g. some Hyprland and
Sway setups) need one line in their config so started apps can open windows;
[docs/install.md](install.md#launched-apps-do-not-open-a-window) explains it.

### Phase 1.6 — Output volume
- Separate configuration tables and output volume: specified in full (CFG-18 to CFG-22,
  section 4.15; ADR 0026, ADR 0027).

### Phase 1.7 — On-screen display and LEDs
- On-screen feedback when a volume or mute changes: KDE's native volume OSD when available, a desktop notification elsewhere (e.g. GNOME).
- A desktop notification when something needs attention, e.g. an invalid configuration or the controller unplugged (ADR 0021).
- An LED language beyond on and off: steady, blinking by itself, blinking on request, per screen state; used for problems, solo on a silent app, the media keys' column and a service menu (#100, #101, #103, #108).

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
- Recording control beyond starting an app (e.g. start/stop recording in OBS via its WebSocket API, with the Record LED showing the state).
- Read the controller's LED mode over SysEx (read-only) and log a **warning** only when it is "Internal", replacing the HW-03 hint.
- Support for other MIDI controllers.

## 7. Open questions

| # | Question | Phase |
|---|---|---|
| Q-1 | When switching from a layout where an app is user-muted to a layout that does not contain that app, should the app stay muted or become audible until you switch back? | 2 |
| Q-2 | Should R also cycle an input column between input devices? **No longer applies (2026-10-04):** moving apps or inputs between devices with R is dropped; R plays or pauses its column's app or starts an app (ADR 0019, ADR 0020), so it has no press left for it ([#61](https://github.com/Dzobash/apptrol/issues/61)). | – |
| Q-3 | Should an output move made with R persist after the app restarts, and is it per layout or global? **No longer applies (2026-10-04):** see Q-2. | – |
| Q-4 | How does the GUI talk to the service (D-Bus or a local socket)? **Decided 2026-10-02: D-Bus** (session bus), recorded in ADR 0017. **No longer applies** since ADR 0021: there is no GUI program. | 3 |
| Q-5 | Should the journald output use the native journal protocol (structured fields) instead of stdout with severity prefixes? Phase 1 uses severity prefixes. | Backlog |
| Q-6 | Where do settings live when they can be changed outside the file? **Decided 2026-10-02: in `config.toml` only** (ADR 0021): `apptrol setup` edits the file, keeping its comments; nothing overrides it. Open: how to write the file without losing comments. | 3 |
