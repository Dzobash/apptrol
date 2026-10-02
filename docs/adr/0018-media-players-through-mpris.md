# 0018. Media players through MPRIS: finding, matching and controlling them

- **Status:** Accepted
- **Date:** 2026-10-02

## Context

In Phase 1.5, ◀◀ ▶▶ ■ ▶ control media playback, and R on an app column plays and pauses
that column's app, with its LED showing that the app is playing. Media players offer
this through MPRIS on the session bus (ADR 0017). Each player has a bus name
(`org.mpris.MediaPlayer2.<name>`) and reports an `Identity` (a name for people), an
optional `DesktopEntry` (its desktop ID) and its `PlaybackStatus`.

Measured on the reference system (Kubuntu, KDE Plasma, PipeWire) on 2026-10-02:

| Bus name | `Identity` | `DesktopEntry` |
|---|---|---|
| `spotify` | `Spotify` | `spotify` |
| `firefox.instance_1_2320` | `Mozilla firefox_firefox` | `firefox_firefox` (Snap) |
| `chromium.instance225626` (Google Chrome) | `Chrome` | *(none)* |
| `vivaldi.instance227476` | `Vivaldi` | *(none)* |
| `vlc` | `VLC media player` | `vlc` |
| `kdeconnect.mpris_…` (a phone) | `Spotify - Pixel 10 Pro XL` | `org.kde.kdeconnect.app` |
| `plasma-browser-integration` | *(duplicates a browser's player; comes and goes)* | |
| `playerctld` | *(activatable proxy, not running)* | |

- The number in a browser's bus name changes every time the browser starts; Chromium
  based browsers all use `chromium` or their own name plus a process ID.
- `DesktopEntry` is missing for Chrome and Vivaldi. mpv has no MPRIS without the
  `mpv-mpris` plugin, although its volume works through PipeWire.
- A browser has **one** MPRIS player, even with several tabs playing. `Pause` and `Play`
  reach only the most recently used playing tab; the others keep playing.
- PipeWire has **one stream per tab**, and a paused tab's stream is corked (or closed
  and reopened corked). Idle apps (Viber) keep a corked stream open.

## Decision

1. **Finding players:** every bus name starting with `org.mpris.MediaPlayer2.` that has a
   running owner, followed live through `NameOwnerChanged`. Activatable names are never
   called, so Apptrol never starts a service by calling it.
2. **Ignored, before any matching:** `kdeconnect.*` (players on other devices),
   `playerctld` (a proxy) and `plasma-browser-integration` (a duplicate of the browsers'
   own players). Filtering first matters: the phone's Spotify reports "Spotify" too.
3. **Player and column:** an app's match list (CFG-04) is compared, as case-insensitive
   substrings, with the bus name without `.instance…`, the `Identity` and, if present,
   the `DesktopEntry`. No extra configuration. If a player matches apps on several
   controls, the first control gets it, in the order of CFG-12.
4. **Commands:** always `Play` or `Pause`, chosen from the current `PlaybackStatus`;
   never the toggle `PlayPause`, so a command reaching two players for the same media
   cannot cancel itself out.
5. **R on an app column:** pauses every playing player of the column's app; if none
   plays, plays the one that was active most recently.
6. **◀◀ ▶▶ ■ ▶:** act on the player that most recently started playing, or on the one
   pinned with `[media] player` (matched like an app). ▶ plays or pauses, ■ sends `Stop`,
   ◀◀ and ▶▶ send `Previous` and `Next`.
7. **R LED on an app column:** lit while the column's app has a stream that is not corked
   **and** a matching MPRIS player. The stream state comes from PipeWire (one stream per
   tab, so it covers every tab); the player decides whether R can do anything. A lit R
   always means that pressing it has an effect.
8. **Apps without MPRIS** (mpv without its plugin, Discord): R does nothing, logged at
   debug; the LED stays off.
9. **Logging** follows ADR 0016, so that every step from a player appearing to a button
   press can be traced: one line per record, a fixed message, attributes from
   `internal/logattr`. Records from the D-Bus adapter carry `apptrol.component=desktop`;
   decisions (which column, which player) are made in the mixer and carry `mixer`.

   | Level | Message | Attributes (besides layout, control and app, LOG-14) |
   |---|---|---|
   | info | `media player matched` | bus name, `Identity`, `DesktopEntry`, `matched_by` |
   | debug | `media player not matched` | bus name, `Identity`, `DesktopEntry` |
   | debug | `media player ignored` | bus name, `ignored_reason` (`other_device`, `proxy`, `duplicate`) |
   | debug | `media player gone`, `playback status changed` | bus name, `status` |
   | info | `paused`, `playing` (R) | button, bus name of each player commanded |
   | info | `media key` (◀◀ ▶▶ ■ ▶) | button, bus name, `selection` (`most_recent`, `pinned`) |
   | debug | `button has no function: no media player for this app` | button |
   | debug | `stream corked`, `stream uncorked` | stream id and name |
   | error | `media player command failed` | bus name, `error.type=media_command_failed`, `exception.message` |

   New attributes, in the `apptrol.` namespace (no OpenTelemetry convention exists for
   D-Bus or MPRIS): `apptrol.player.bus_name`, `apptrol.player.identity`,
   `apptrol.player.desktop_entry`, `apptrol.player.status` (`Playing`, `Paused`,
   `Stopped`), `apptrol.player.matched_by` (`bus_name`, `identity`, `desktop_entry`),
   `apptrol.player.ignored_reason`, `apptrol.player.selection` and
   `apptrol.stream.corked`. New error type: `media_command_failed`. Each gets a constant
   in `internal/logattr` and a row in `docs/logging.md`; the existing test enforces both.

   Example, troubleshooting "why does R not pause Chrome?":

   ```text
   INFO media player matched apptrol.component=mixer apptrol.layout=default apptrol.control=slider2 apptrol.app.id=browser apptrol.app.name=Browser apptrol.app.type=app apptrol.player.bus_name=org.mpris.MediaPlayer2.chromium.instance225626 apptrol.player.identity=Chrome apptrol.player.matched_by=identity
   DEBU media player ignored apptrol.component=mixer apptrol.player.bus_name=org.mpris.MediaPlayer2.kdeconnect.mpris_3f37… apptrol.player.ignored_reason=other_device
   INFO paused apptrol.component=mixer apptrol.layout=default apptrol.control=slider2 apptrol.app.id=browser apptrol.app.name=Browser apptrol.app.type=app apptrol.button=R2 apptrol.player.bus_name=org.mpris.MediaPlayer2.chromium.instance225626
   ```

## Consequences

- No new configuration for R: existing match lists find the players, including Chrome
  (through `Identity`) and Snap apps (`firefox_firefox`).
- R pauses only the last used tab of a browser; the other tabs keep playing. This is how
  browsers implement MPRIS, not an Apptrol bug, and it is listed under *Known issues* in
  the README when R ships. M silences every tab, because it acts on the streams.
- The audio adapter reports a stream's corked state to the mixer (`jfreymuth/pulse/proto`
  already provides it; changes arrive with the existing subscription).
- LED-03 (R LED off on app columns) ends with Phase 1.5; the new behaviour gets
  requirement IDs when the phase starts.
- Players on other devices cannot be controlled. If that is wanted later, it needs its
  own setting.
- With `level = "debug"`, the log shows every player Apptrol sees and why it was matched,
  ignored or left alone, so a wrong or missing match can be found without a debugger:
  `journalctl --user -u apptrol | grep apptrol.player`.
