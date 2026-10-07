# Known issues

Problems you may run into, why they happen, and what to do. Some are bugs in Apptrol that
are being fixed; others come from other programs, which Apptrol deliberately does not work
around. Found another one? Please [open an issue](https://github.com/Dzobash/apptrol/issues).

## In Apptrol

### A "no permission" error when a second user logs in

*In 0.2.1; to be fixed in 0.2.2 ([#104](https://github.com/Dzobash/apptrol/issues/104)).*

When a second user logs in through *Switch user*, their Apptrol starts at that moment and
may log `no permission to open the controller; see the README section on permissions`,
followed a second later by `controller connected`. The system gives the new user the
rights to the controller a moment after their session comes to the front; Apptrol was just
too early. If `controller connected` follows, nothing is wrong.

### A restart with an invalid configuration loses the saved state

*In 0.1.0 and 0.2.0; fixed in 0.2.1 ([#77](https://github.com/Dzobash/apptrol/issues/77)).*

If Apptrol starts while `config.toml` has an error, it runs without assignments and waits
for a valid file, as intended. But it also saves that empty state, so the slider positions,
mutes and solo saved before are lost. Afterwards, talk-over mutes apps instead of turning
them down until you have moved their controls once.

**Workaround:** run `apptrol check` before restarting Apptrol or logging in again after
editing the configuration.

### After *Switch user*, the controller still acts on the first user's apps

*In 0.2.0; fixed in 0.2.1 ([#78](https://github.com/Dzobash/apptrol/issues/78)).*

The first user's Apptrol keeps the controller while another user is in front. Launchers
are blocked and every press is logged as a warning, but M, S, R and the media keys still
change the first user's apps. A second user's own Apptrol cannot get the controller.
From 0.2.1, Apptrol lets go of the controller while another user is in front, so each
user can run their own Apptrol.

### "No permission to open the controller" right after plugging it in

*In 0.1.0 and 0.2.0; fixed in 0.2.1 ([#75](https://github.com/Dzobash/apptrol/issues/75)).*

Sometimes Apptrol logs this error when the controller is plugged in, then connects a second
later. The system grants access to the controller a moment after it appears; Apptrol was
just too early. If the error is followed by `controller connected`, nothing is wrong.

### Release candidates: `checksums.txt` does not match the file names

*Release candidates of 0.2.0; fixed from 0.2.1-rc1
([#74](https://github.com/Dzobash/apptrol/issues/74)).* GitHub renamed `~` in the package
names to `.`, so `sha256sum -c` found no file. Final releases were not affected.

## Installing

### Discover shows a downloaded package as `apptrol_0`, and does not list it once installed

*How KDE Discover handles packages that do not come from a package source
([#97](https://github.com/Dzobash/apptrol/issues/97)).*

Opened in Discover, `apptrol_0.2.1_amd64.deb` appears as **`apptrol_0`** (the file name up
to its first dot), with **Unknown author**, **License: Unknown** and no icon: for a package
file, Discover reads only its version and size. Once installed, Apptrol does not appear in
Discover at all, not even under *Installed*.

The package does carry a name, developer, icon and description (AppStream metadata), and
the system finds them: `appstreamcli get io.github.dzobash.apptrol`. But Discover links an
app to its package through a package source's catalog, and a downloaded package has none.

**Nothing is wrong with the package:** installing it from Discover works as usual, and so
does installing it in a terminal: `sudo apt install ./apptrol_*_amd64.deb`. Upgrade and
remove it with `apt` as well ([installation guide](install.md#upgrading)).

## Volume

### An app's own volume control works on top of Apptrol's

Some apps have a volume control of their own that is applied inside the app: web pages
(YouTube's volume slider), browsers and players built on mpv (such as Haruna). The desktop
mixer does not see it, and neither does Apptrol. Both volumes multiply: a page at 50 % and
its slider at 80 % play at 40 %.

Other apps tie their own control to the volume the desktop sees (VLC, Elisa), so both move
together.

**What to do:** leave the app's own control at 100 % and use the slider.

### Spotify resets its volume when the track changes

*A bug in the Spotify client ([#67](https://github.com/Dzobash/apptrol/issues/67)).*

On Linux with PipeWire, Spotify does not keep its own volume setting in sync with its audio
stream. A change from outside (by Apptrol, or by the desktop's volume settings) is heard at
once, but as soon as the next track starts, Spotify applies the volume stored in its own
settings again.

**Workaround:** touch Spotify's slider after a track change. Apptrol does not reset it on
its own: a change another program makes stays until you touch that app's control.

### Apps go silent after PipeWire is restarted

Many apps lose their audio connection when PipeWire restarts (e.g. with
`systemctl --user restart pipewire pipewire-pulse`) and stay silent: browsers need the page
reloaded, others (such as Elisa) a restart. Apptrol itself reconnects and applies the
volumes, mutes and solo to each app again once it plays.

## Media players

### R pauses only one browser tab

*How browsers implement MPRIS ([#68](https://github.com/Dzobash/apptrol/issues/68)).*

A browser offers one media player for all its tabs, and *Pause* reaches only the tab whose
player you used last (played, paused, seeked or changed its volume), not the tab you
clicked on. The other tabs keep playing. M silences every tab, because it acts on each
tab's audio.

### R does nothing for a browser until a tab has played

A browser offers its media player only while a tab plays or has played since the browser
started. Tabs restored after a restart that have not started yet have none, so R does
nothing (the debug log says `no media player for this app`). Start the video once; from
then on R works.

### ■ only pauses Spotify

*Spotify's behaviour ([#69](https://github.com/Dzobash/apptrol/issues/69)).* The media
player standard says *Stop* stops, and *Play* then starts the track again from the
beginning; VLC and Elisa do this. Spotify treats *Stop* as a pause, so ▶ continues where it
was.

## The lock screen

### Some lock screens are not recognised

Apptrol learns that the screen is locked from logind, the login manager, which the lock
screens of KDE Plasma and GNOME tell. Some lock programs on minimal window manager setups
do not; there Apptrol sees an unlocked screen, so launchers work and nothing is logged as
pressed at the lock screen.

**To check:** lock the screen, and from a second computer over SSH run

```bash
loginctl show-session "$(loginctl show-user "$USER" -p Display --value)" -p LockedHint
```

It should say `LockedHint=yes`.

### Launchers never start without a graphical login session

If logind has no graphical session for you (some setups started without a display
manager), Apptrol cannot tell whether the screen is locked and, to be safe, starts nothing.
The log says `apptrol.screen.reason=no_graphical_session`.
