# Installing Apptrol

Everything about getting Apptrol running: what it needs, the controller's settings,
installing, upgrading, building from source and removing it. For what to put into the
configuration, see the [configuration reference](config.md).

## What your system needs

Every normal Linux desktop already has these:

- PipeWire with `pipewire-pulse` (the default on Ubuntu, Kubuntu, Fedora, Mint), or
  PulseAudio
- The kernel's USB audio/MIDI support, which makes the controller appear as a MIDI device
- systemd, to run Apptrol in the background (you can also start `apptrol` by hand)
- For the R buttons, the media keys and launchers: a desktop session with D-Bus, which
  every desktop has

You do **not** need any extra libraries: Apptrol is a single self-contained program.
`alsa-utils` is optional but handy for troubleshooting: `amidi -l` shows whether the
controller is detected.

### Tested on

So far, Apptrol has been tried on one real computer: Kubuntu with KDE Plasma (Wayland) and
PipeWire on an amd64 PC, with a Korg nanoKONTROL2 and a GoXLR Mini. The automated tests
also run against PipeWire on Ubuntu in CI. Other distributions and desktops, PulseAudio
instead of PipeWire, and the arm64 packages should work, but nobody has tried them yet. If
you run Apptrol somewhere else, please
[open an issue](https://github.com/Dzobash/apptrol/issues) and say whether it worked;
that helps everyone.

## Controller settings

The nanoKONTROL2 stores its settings itself. The factory defaults are fine, except for the
LED mode:

| Setting | Needed | Why |
|---|---|---|
| Mode: **CC** (not a DAW mode) | Yes, factory default | Apptrol understands the controller's CC messages |
| Buttons: **Momentary** | Yes, factory default | Apptrol acts on the press, and for the microphone buttons on the release; with *Toggle*, every second press is ignored |
| LED mode: **External** | Only for LED feedback | Without it, all controls work, but a button's LED lights only while you hold it, instead of showing mute and solo |

To change a setting on Linux, use [SysEx Controls](https://github.com/soyersoyer/sysex-controls)
(`flatpak install flathub hu.irl.sysex-controls`, or `sysex-controls` in the AUR). On
Windows or macOS, Korg's KONTROL Editor does the same. The setting stays stored in the
controller, so it is a one-time step.

**Check it** with `apptrol test` (stop the service first, `systemctl --user stop apptrol`:
only one program can read the controller). It shows every slider, knob and button you
touch, and turns a button's LED on and off with each press. If an LED lights only while
you hold the button, the LED mode is still *Internal*.

## Permissions

Your normal desktop login can use the controller automatically: the system gives the user
at the screen access to it. Only in unusual setups, such as running Apptrol from an SSH
session, add your user to the `audio` group and log in again:

```bash
sudo usermod -aG audio "$USER"
```

## Installing a package

Download the package for your system from the
[Releases](https://github.com/Dzobash/apptrol/releases) page and install it:

```bash
sudo apt install ./apptrol_*_amd64.deb      # Debian, Ubuntu, Kubuntu
sudo dnf install ./apptrol-*.x86_64.rpm     # Fedora
```

Then start Apptrol for your user, now and at every login. The package cannot do this for
you: Apptrol runs in your login, not for the whole computer, as it controls your sound.

```bash
systemctl --user daemon-reload
systemctl --user enable --now apptrol
```

`daemon-reload` makes systemd read the newly installed service file; `enable --now` starts
Apptrol at every login (`enable`) and right now (`--now`).

### Checking a download

Each release has a `checksums.txt` with the SHA-256 of every file. In the folder with the
downloaded files:

```bash
sha256sum -c checksums.txt --ignore-missing
```

It should print `OK` for your package. This shows the download is complete and unchanged.
(For release candidates the file names do not match yet; see
[#74](https://github.com/Dzobash/apptrol/issues/74).)

## Upgrading

Install the newer package the same way, then restart Apptrol:

```bash
systemctl --user daemon-reload
systemctl --user restart apptrol
```

`enable` is not needed again: it stays set across upgrades. `apptrol --version` shows the
version you have.

## Building from source

You need [Go](https://go.dev/dl/) 1.24 or newer, `git` and `make`; nothing else, as Apptrol
is pure Go without C libraries. (Ubuntu and Kubuntu 25.04 or newer ship a recent enough Go
as `golang-go`; on older releases, install it from go.dev.)

```bash
git clone https://github.com/Dzobash/apptrol.git
cd apptrol
git checkout v0.2.0        # optional: a release instead of the newest code
make build                 # creates bin/apptrol
./bin/apptrol --version
```

`./bin/apptrol` runs it in the terminal. To run it as a service, as the packages do, install
the program and the systemd unit for your user:

```bash
install -Dm755 bin/apptrol ~/.local/bin/apptrol
install -Dm644 packaging/systemd/apptrol.service ~/.config/systemd/user/apptrol.service
sed -i "s|/usr/bin/apptrol|$HOME/.local/bin/apptrol|" ~/.config/systemd/user/apptrol.service
systemctl --user daemon-reload
systemctl --user enable --now apptrol
```

To update, pull the new code, run `make build` and the first `install` line again, then
`systemctl --user restart apptrol`. To build your own `.deb` and `.rpm` packages instead,
run `make snapshot` (needs [GoReleaser](https://goreleaser.com/install/)); they end up in
`dist/`. For changing the code, see [CONTRIBUTING.md](https://github.com/Dzobash/apptrol/blob/main/CONTRIBUTING.md).

## First start

1. **Write your configuration** in `~/.config/apptrol/config.toml`. Apptrol creates it from
   the example on its first start; to begin editing before that, copy the example:
   ```bash
   mkdir -p ~/.config/apptrol
   cp /usr/share/doc/apptrol/examples/config.toml ~/.config/apptrol/
   ```
   `apptrol list` shows the names of the apps that are playing and of your input devices;
   `apptrol list apps` the installed apps, for launcher buttons; `apptrol check` validates
   the file and shows which app is on which control.
2. **Try it** in a terminal, with the service stopped: run `apptrol --log-level info`,
   press an M button, and watch the log. Ctrl+C stops it. `--log-level debug` shows every
   detail, for this run only.
3. **Check the log** if something does not respond. By default it shows only warnings and
   errors; set `level = "info"` in the `[log]` section to see everything Apptrol does:
   ```bash
   journalctl --user -u apptrol -f
   ```
   [Reading Apptrol's logs](logging.md) explains every part of a line.

## Launched apps do not open a window

Launcher buttons start apps through systemd. Some desktops, such as some Hyprland and Sway
setups, do not pass the display to systemd's user services, so a started app cannot open
its window. Add one line to your desktop's configuration so they do:

```text
# Hyprland (hyprland.conf)
exec-once = dbus-update-activation-environment --systemd WAYLAND_DISPLAY XDG_CURRENT_DESKTOP

# Sway (config); many distributions include this already via /etc/sway/config.d/
exec dbus-update-activation-environment --systemd WAYLAND_DISPLAY SWAYSOCK XDG_CURRENT_DESKTOP
```

KDE Plasma and GNOME do this already.

## Removing Apptrol

```bash
systemctl --user disable --now apptrol
sudo apt remove apptrol        # or: sudo dnf remove apptrol
```

Your configuration (`~/.config/apptrol/`) and saved state (`~/.local/state/apptrol/`) stay;
delete those folders too to remove everything. Mutes made with M stay set in the desktop's
volume settings after Apptrol is gone; unmute those apps there.
