# Log attributes

Every attribute and error type Apptrol writes. How to find and collect the logs is in
[Reading Apptrol's logs](logging.md).

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
| `apptrol.app.id` | The id of an app, input or output in the configuration (`[apps.<id>]`, `[inputs.<id>]`, `[outputs.<id>]`); also on the `config_invalid` record of an id defined in two tables, or of a `type` in `[apps]` that is not valid |
| `apptrol.app.name` | The app's `name` in the configuration |
| `apptrol.app.type` | `app`, `input` or `output` |
| `apptrol.app.match` | The target's `match` list; on a monitor warning, the offending fragment only |
| `apptrol.solo.previous_control` | Where solo was before it moved |
| `apptrol.solo.previous_app` | The app solo moved away from |
| `apptrol.volume_percent` | A volume, in percent (100 = normal) |
| `apptrol.wanted_percent` | The volume Apptrol set, when PipeWire changed it back |
| `apptrol.muted` | A mute state |
| `apptrol.wanted_muted` | The mute state Apptrol set, when PipeWire changed it back |
| `apptrol.action` | A change that could not be made, or an ownership answer the audio adapter ignored (`KeepOutput`, `ReleaseOutput`) |
| `apptrol.led` | An LED, e.g. `S1` |
| `apptrol.stream.id` | PipeWire's number for a playing stream |
| `apptrol.stream.app_name` | The name the app gives its stream (what `match` compares) |
| `apptrol.stream.corked` | Whether the app has paused the stream (`true`) or it plays (`false`) |
| `apptrol.device.name` | An input or output device's unique name (what `match` compares, with the description) |
| `apptrol.device.description` | An input or output device's readable name |
| `apptrol.device.matches` | All input or output devices that match |
| `apptrol.device.selected_by` | How an output's device was chosen: `match` (a fixed output); `default` later, with `device = "default"` |
| `apptrol.device.controls` | Conflicting output controls: owner first, then ignored control, e.g. `slider7, knob7` (OUT-04, ADR 0035) |
| `apptrol.device.index` | The audio server's number for an output device (changes when it is added again) |
| `apptrol.audio.channels` | How many channels a device has, as read or written |
| `apptrol.audio.result` | The audio server's answer to a request: `success` or `failed` |
| `apptrol.audio.guard.state` | What the new-output guard made of a read: `restore_volume` (set again), `at_wanted_volume`, `no_volume_set` (no control set it, or it was released), `expired`, `no_guard` (not new), `identity_changed` (new, or a new name or description: the mixer decides first), `ownership_pending` (no answer for the newest reported identity yet; no guard write); for a mute change not taken over within the guard's window (#177): `restore_mute` (the other value: set again), `at_wanted_mute` (the value Apptrol set) or `ownership_pending` |
| `apptrol.audio.guard.remaining_ms` | Milliseconds left in the new-output guard's 3 seconds |
| `apptrol.audio.ownership.answer_generation` | The output identity an ignored `KeepOutput` or `ReleaseOutput` answered for |
| `apptrol.audio.ownership.generation` | The newest output identity the audio adapter reported, which the answer lost to |
| `apptrol.audio.server` | The audio server and its version |
| `apptrol.retry.delay_s` | Seconds until Apptrol tries again |
| `apptrol.controller.port` | The controller's sound card id from the configuration |
| `apptrol.controller.port_in_use` | The sound card id in use until restart |
| `apptrol.controller.device` | The controller's device file, e.g. `/dev/snd/midiC1D0` |
| `apptrol.controller.sound_cards` | The sound cards there are, when the controller is not found |
| `apptrol.controller.reason` | Why the controller disconnected, e.g. `unplugged` |
| `apptrol.controller.grace` | Why opening the controller is retried at debug, not reported as an error: `started` (Apptrol just started), `plugged_in` (just plugged in) or `taken_back` (just taken back from another user); for 5 seconds after that moment |
| `apptrol.midi.controller` | A MIDI message's controller number |
| `apptrol.midi.value` | A MIDI message's value |
| `apptrol.midi.channel` | A MIDI message's channel (1–16) |
| `apptrol.config.controls` | How many controls the configuration assigns |
| `apptrol.config.table` | The configuration table an entry belongs in: `apps` or `inputs` (on `config uses an old form`), `outputs` or `inputs` (on the `config_invalid` record of a `type` in `[apps]` that is not valid) |
| `apptrol.warning` | A warning about the configuration or the saved state |
| `apptrol.state.positions` | How many positions were restored |
| `apptrol.state.mutes` | How many mutes were restored |
| `apptrol.state.solo` | The restored solo, e.g. `slider1` |
| `apptrol.log.level` | The log level set with `--log-level` (on the start line) |
| `apptrol.log.level_source` | Where the log level comes from: `flag` (`--log-level`) or `config` (on the start line) |
| `apptrol.held.reason` | Why a held button (cough, talk-over, hold-to-talk) ended without being released: `controller_disconnected`, `controller_released` (another user came to the front), `config_changed` or `stopping` |
| `apptrol.talk_over_percent` | The volume apps go down to during talk-over |
| `apptrol.button_mode` | What a button from the configuration does: its mode, or `launcher` (on `button configured`) |
| `apptrol.button_talk_over` | `true` when an M button also turns apps down (`talk_over = true`) |
| `apptrol.launcher.desktop_id` | The app a launcher starts |
| `apptrol.launcher.command` | The command a launcher runs |
| `apptrol.launcher.unit` | The systemd unit an app was started in, e.g. `app-apptrol-discord@1a2b3c4d.service` (`journalctl --user -u <unit>` shows the app's output) |
| `apptrol.launcher.running_found_by` | With `if_running = "skip"`: how a running app was found, `unit` or `process` |
| `apptrol.launcher.running_unit` | The unit a running app was found in |
| `apptrol.launcher.checked` | What was looked at when an app was not found running (debug) |
| `apptrol.desktop.bus_address` | The session bus Apptrol connected to |
| `apptrol.desktop.players` | How many media players were found on connecting |
| `apptrol.player.bus_name` | A media player's name on the bus, e.g. `org.mpris.MediaPlayer2.spotify` |
| `apptrol.player.identity` | The name a media player gives itself, e.g. `Spotify` |
| `apptrol.player.desktop_entry` | The player's desktop ID, if it reports one |
| `apptrol.player.status` | `Playing`, `Paused` or `Stopped` |
| `apptrol.player.matched_by` | What made a media player belong to an app: `bus_name`, `identity` or `desktop_entry` |
| `apptrol.player.ignored_reason` | Why a media player is never used: `other_device` (e.g. a phone through KDE Connect), `proxy` (`playerctld`) or `duplicate` (`plasma-browser-integration`) |
| `apptrol.player.command` | `Play`, `Pause`, `Stop`, `Next` or `Previous`, sent to a media player |
| `apptrol.player.selection` | How the media keys chose their player: `most_recent`, or `pinned` with `[media] player` |
| `apptrol.session.bus_address` | The system bus Apptrol connected to, to learn when the computer wakes up and whether the screen is locked |
| `apptrol.session.resend` | After a wake-up, which repeat of sending every LED this is: `1`, `2` or `3` (0.5, 2 and 5 seconds after waking up; debug) |
| `apptrol.session.id` | logind's id of your graphical login session, whose lock state Apptrol follows; on `apptrol.seat.front` records, the session in front |
| `apptrol.controller.at_login_screen` | `[controller] at_login_screen`: `keep` (the login screen changes nothing: the controller stays with whoever had it) or `release` (it belongs to nobody there) |
| `apptrol.status` | The text of the Status line that `systemctl --user status apptrol` shows, as Apptrol sent it: the version first, then the state and any active solo (including `not playing` when neither a matched uncorked stream nor a matched MPRIS player reports playback) |
| `apptrol.seat.front` | Whose session is in front of the computer: `this_user`; `other_user` (Apptrol lets go of the controller); `login_screen` (kept or let go, as `[controller] at_login_screen` says); `nobody`; `unknown` (it cannot be told: the controller is held as before) |
| `apptrol.screen.state` | `unlocked`; `locked`; `inactive` (another user's session is in front); `unknown` (it cannot be told). Launchers start apps only when `unlocked`, or with `when_locked` also when `locked` or `inactive`; the media buttons and R send nothing when `inactive` |
| `apptrol.screen.reason` | Why the state is `unknown`: `no_system_bus`, `system_bus_lost`, `no_graphical_session` (e.g. logged in only over SSH) or `unreadable` |

## Errors

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
| `controller_permission_denied` | You may not open the controller (see [Permissions](install.md#permissions)). |
| `controller_open_failed` | The controller could not be opened for another reason. |
| `led_failed` | An LED could not be set (debug level). |
| `desktop_bus_unreachable` | No D-Bus session bus (is a desktop session running?); media players are not available, everything else works. |
| `desktop_bus_lost` | The connection to the session bus broke; Apptrol reconnects by itself. |
| `media_player_unreadable` | A media player did not answer when asked for its name; it is ignored. At debug: the bus could not say who owns a player's name, usually because the player just closed; it is skipped. |
| `media_command_failed` | A media player refused *Play* or *Pause*, or did not answer in time. |
| `app_start_failed` | A launcher's app could not be started: not installed, a broken `Exec` line, a program that does not exist, or systemd refused; `exception.message` says which. |
| `system_bus_unreachable` | No D-Bus system bus (warning). Launchers are blocked, because Apptrol cannot tell whether the screen is locked, and after the computer wakes up the LEDs are not sent again until they change; everything else works. |
| `system_bus_lost` | The connection to the system bus broke (warning); launchers are blocked until Apptrol has reconnected by itself. |
| `screen_state_unreadable` | logind did not say whether the screen is locked (warning); launchers are blocked, and Apptrol asks again every 5 seconds. |
| `seat_unreadable` | logind did not say whose session is in front of the computer (warning). Apptrol holds the controller as before, and asks again every 5 seconds. |
| `status_not_sent` | The Status line could not be sent to systemd (warning); Apptrol keeps running, and the line shows the last text that arrived. |
