# 0019. Launcher buttons, input column buttons and their configuration

- **Status:** Accepted
- **Date:** 2026-10-02

## Context

In Phase 1.5 every button except the layout buttons gets a function (ADR 0011, note of
2026-10-02). Record (●) and the three Marker buttons start apps; R on a column can be
changed from its default; on an input column, R and S act on the microphone and on the
other apps while held. Media players and R's play / pause are decided in ADR 0018; this
record covers starting apps, the held input modes, and how all of it is configured.

Measured on the reference system (Kubuntu, KDE Plasma) on 2026-10-02:

- **Desktop IDs** come in three forms: reverse-DNS for Flatpak (`com.obsproject.Studio`),
  `<snap>_<app>` for Snap (`firefox_firefox`, `vlc_vlc`), and plain names from the
  distribution (`discord`, `steam`).
- The systemd user manager's `XDG_DATA_DIRS` contains the Flatpak, Snap and system
  folders, so a service finds the same apps as the desktop. `DISPLAY` and
  `WAYLAND_DISPLAY` are set there too, so started apps can open windows.
- KDE starts apps as `app-<desktop-id>@<random>.service` or in
  `app-<desktop-id>-<pid>.scope`; Snap apps then also run in
  `snap.<snap>.<app>-<uuid>.scope`.
- Unit names are not always right: an app built on Chromium ran in a scope named after
  Chromium's desktop ID. Apps started from a terminal have no app unit at all.
- `Exec` lines contain field codes such as `%u` (`/snap/bin/firefox %u`), and desktop
  files can offer extra actions (e.g. "New Window").

## Decision

### Starting apps

1. **Desktop files** are found as the [Desktop Entry](https://specifications.freedesktop.org/desktop-entry-spec/latest/)
   and [Base Directory](https://specifications.freedesktop.org/basedir-spec/latest/)
   specifications say: in `applications/` under `$XDG_DATA_HOME` and each
   `$XDG_DATA_DIRS` entry, the first one found winning. `Hidden=true` files count as not
   installed.
2. **`Exec` is parsed** as the specification says: quoting and escapes, field codes for
   files and links (`%f %F %u %U`) removed, `%c` replaced by the name, `%i` and `%k`
   dropped. A file with `DBusActivatable=true` and no `Exec` is started through
   `org.freedesktop.Application.Activate`.
3. **systemd starts the app**, not Apptrol: a transient unit
   `app-apptrol-<desktop-id>@<random>.service` (for a `command`, its program's name in
   place of the desktop ID), through `coreos/go-systemd` (ADR 0017). The app is never a
   child of Apptrol, gets the user manager's environment, and keeps running when Apptrol
   stops or restarts. Finished units are removed (`CollectMode=inactive-or-failed`).
4. **`command`** is a list of arguments, run without a shell; `~/` at the start of an
   argument is expanded, as in the configuration's paths. Pipes and `&&` need an explicit
   `["sh", "-c", "…"]`.
5. **`if_running`**: `start` (default) always starts it, and most apps then show their
   open window themselves; `skip` does nothing if the app already runs.
6. **"Already running"** is checked only when the button is pressed, in two steps:
   first the user manager's units (`app-*<desktop-id>-*.scope`,
   `app-*<desktop-id>@*.service`, and `snap.<snap>.<app>-*.scope` for a Snap ID); if none
   is found, the running processes, by the program name from `Exec` (or `command`).
   Together they find apps started from the menu, by Apptrol and from a terminal; an app
   whose wrapper hides its program name can still be missed.
7. **The Record LED** flashes briefly on a press to confirm it. It does not show whether
   the app runs: that cannot always be detected (point 6). Marker buttons have no LED.

### Input column buttons

8. **R and S on an input column** each have a mode:
   - `cough`: the input is muted while the button is held (R's default)
   - `talk_over`: while held, every app target of the layout goes down to the input
     app's `talk_over` level (S's default)
   - `push_to_talk`: the input is live only while the button is held, muted otherwise
   - `off`: the button does nothing
   R can also be a launcher. On an app column, R is `play_pause` (default, ADR 0018),
   `off` or a launcher; S on an app column stays solo.
9. **Held states are kept apart from the M mute**, like solo (MUTE-04): an input is muted
   when it is muted with M, or coughing, or set to push-to-talk and not held. The M LED
   shows whether the input is live, whatever muted it (LED-04), so with push-to-talk it
   is lit only while the button is held. Talk-over
   never raises a volume: an app below the talk-over level stays where it is. A control
   moved during talk-over is remembered and applied on release.
10. **Held states end** when the button is released, when the controller disconnects
    (the release would never arrive), and when Apptrol stops. They are never saved.

### Configuration

11. Buttons are configured per layout, so each layout (Phase 2) has its own:

    ```toml
    [layouts.default.buttons]
    record      = { app = "com.obsproject.Studio" }
    marker_set  = { app = "discord", if_running = "skip" }
    marker_prev = { command = ["konsole", "-e", "htop"] }
    r3          = { app = "discord" }           # a launcher instead of play / pause
    r4          = { mode = "off" }
    r8          = { mode = "push_to_talk" }     # on an input column
    s8          = { mode = "off" }              # on an input column
    ```

    Button names: `record`, `marker_set`, `marker_prev`, `marker_next`, `r1` … `r8`, and
    `s1` … `s8` for input columns. `[media] player` names an app from `[apps]` to pin the
    media keys to (ADR 0018). `talk_over` (0–100, default 25) is set on an input app.
12. **`apptrol check`** rejects, with one record per problem (ADR 0016): `app` and
    `command` together or neither, unknown button names, a mode on the wrong kind of
    column, an `s` override on an app column, an unknown app in `[media] player`, and
    `talk_over` outside 0–100. A desktop ID that is not installed is only a warning: it
    may be installed later.
13. **`apptrol list apps [search]`** prints the desktop ID, name and source (`system`,
    `flatpak`, `snap`, `user`) of every installed app, without `Hidden` or `NoDisplay`
    ones; the search is case-insensitive over ID and name.
14. **The example configuration documents every new setting**: commented out, with an
    example value, the accepted values and the default, so a line is uncommented rather
    than typed. `docs/config.md` has the full reference.

### Logging

15. Following ADR 0016 and the rule that every decision is logged with its reason:

    | Level | Message | Attributes (besides layout, control, app and button, LOG-14) |
    |---|---|---|
    | info | `starting app` | `desktop_id` or `command`, `unit` |
    | info | `app already running; not started` | `desktop_id`, `running_found_by` (`unit`, `process`), `running_unit` or `process.executable.name` |
    | debug | `app not running` | `desktop_id`, what was checked |
    | info | `cough on`, `cough off`, `talk-over on`, `talk-over off`, `push-to-talk live`, `push-to-talk muted` | `talk_over_percent` for talk-over |
    | info | `held state ended` | `reason` (`controller_disconnected`, `stopping`) |
    | warn | `desktop ID not installed` | `desktop_id` |
    | error | `app could not be started` | `desktop_id` or `command`, `error.type=app_start_failed`, `exception.message` |

    New attributes: `apptrol.launcher.desktop_id`, `apptrol.launcher.command`,
    `apptrol.launcher.unit`, `apptrol.launcher.running_found_by`,
    `apptrol.launcher.running_unit`, `apptrol.held.reason`,
    `apptrol.talk_over_percent`; the executable uses OpenTelemetry's
    `process.executable.name`. New error type: `app_start_failed`.

## Consequences

- Apps started from a button belong to the desktop session; restarting or upgrading
  Apptrol never closes them.
- Desktops that do not pass the display to the systemd user manager (e.g. some Hyprland
  and Sway setups) need one line in their configuration; the README explains it with
  the feature.
- `skip` can miss an app whose wrapper hides its program name, and start it again;
  the log says what was checked.
- Requirement IDs for launchers, the input modes and the button configuration are
  given when Phase 1.5 starts. LED-04 (input column LEDs) and SOLO-07 (S on an input
  column) change with it.
- The configuration format grows by one section per layout; existing files stay valid.

## Notes

- 2026-10-02: The input column buttons (points 8–10, and the input-column parts of
  points 11, 12 and 15) are replaced by [ADR 0020](0020-microphone-column-buttons.md):
  M becomes the microphone button (`mute` or `hold_to_talk`), S gets `cough` or
  `talk_over`, and R on an input column is `off` or a launcher.
