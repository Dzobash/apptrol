<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/brand/apptrol-logo-dark.svg">
    <img src="docs/assets/brand/apptrol-logo.svg" alt="Apptrol" height="72">
  </picture>
</p>

<p align="center">
  <b>Control each app's volume on Linux with a hardware MIDI controller.</b><br>
  A per-app volume mixer for PipeWire, built for the Korg nanoKONTROL2.
</p>

> **Status: in development — not usable yet.** Phase 1 (the core mixer) is being built;
> configuration loading and `apptrol check` work. See the [roadmap](docs/roadmap.md).

## What it does

Put Spotify on the first slider, your browser on the second, Discord on the third and your
microphone on the last one. Moving a slider or knob changes that app's volume directly,
with no desktop mixer involved.

- Every slider and knob can control one app or one input (e.g. your microphone)
- **M** mutes the app on that slider, **S** solos it
- The button LEDs show what is muted and soloed
- Apps that start later get the slider's volume straight away
- Settings live in a simple TOML file that is reloaded when you save it
- Runs quietly in the background as a systemd user service
- Works on any desktop (KDE Plasma, GNOME, …), Wayland or X11

It works alongside an audio interface such as a GoXLR, or on its own with a normal sound card.

Planned later: media buttons, on-screen volume display, multiple layouts (e.g. *Work* and
*Gaming*) and a GUI.

## Before you install

**Your system needs** — every normal Linux desktop already has these:

- PipeWire with `pipewire-pulse` (the default on Ubuntu, Kubuntu, Fedora, Mint), or PulseAudio
- The kernel's USB audio/MIDI support, which makes the controller appear as a MIDI device
- systemd, to run Apptrol in the background (you can also start `apptrol` by hand)

**You do not need** any extra libraries: Apptrol is a single self-contained program.
`alsa-utils` is optional but handy for troubleshooting — `amidi -l` shows whether the
controller is detected.

**Controller settings.** The nanoKONTROL2 stores these itself; the factory defaults are
fine except for the LED mode:

| Setting | Needed | Why |
|---|---|---|
| Mode: **CC** (not a DAW mode) | Yes — factory default | Apptrol understands the controller's CC messages |
| Buttons: **Momentary** | Yes — factory default | Apptrol acts on the button press; with *Toggle*, every second press is ignored |
| LED mode: **External** | Only for LED feedback | Without it, all controls work, but the button LEDs only light while held instead of showing mute and solo |

To change a setting on Linux, use [SysEx Controls](https://github.com/soyersoyer/sysex-controls)
(`flatpak install flathub hu.irl.sysex-controls`, or `sysex-controls` in the AUR). On
Windows or macOS, Korg's KONTROL Editor does the same. The setting stays stored in the
controller, so it is a one-time step.

To check the controller and its settings, run `apptrol test`: it shows every slider, knob
and button you touch, and turns a button's LED on and off with each press. If an LED
lights only while you hold the button, the LED mode is still *Internal*.

**Permissions.** Your normal desktop login can use the controller automatically. Only in
unusual setups — such as running Apptrol from an SSH session — add your user to the
`audio` group.

## Installation

No release yet. Once there is one, download the package for your system from the
[Releases](https://github.com/Dzobash/apptrol/releases) page and install it:

```bash
sudo apt install ./apptrol_*_amd64.deb      # Debian, Ubuntu, Kubuntu
sudo dnf install ./apptrol-*.x86_64.rpm     # Fedora
```

## First steps

1. **Write your configuration.** Start from the example and edit it:
   ```bash
   mkdir -p ~/.config/apptrol
   cp /usr/share/doc/apptrol/examples/config.toml ~/.config/apptrol/
   ```
   `apptrol list` shows the names of the apps that are currently playing and of your input
   devices; `apptrol check` validates the file and shows which app is on which control.
   The [configuration reference](docs/config.md) explains every setting.
2. **Try it** in a terminal: run `apptrol`, move a slider, and watch the log. Ctrl+C stops
   it. Without a configuration file, Apptrol creates the example for you.
3. **Start Apptrol** for your user, now and at every login:
   ```bash
   systemctl --user daemon-reload
   systemctl --user enable --now apptrol
   ```
4. **Check the log** if something does not respond:
   ```bash
   journalctl --user -u apptrol -f
   ```

## Documentation

- [Requirements](docs/requirements.md)
- [Architecture](docs/architecture.md)
- [Roadmap](docs/roadmap.md)
- [Configuration reference](docs/config.md) and [example config](examples/config.toml)
- [Decision records](docs/adr/)
- [Testing](docs/testing.md)
- [Releasing](docs/releasing.md)
- [Brand guide](docs/brand.md) (logo, colours, type)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## About this project

Apptrol is a hobby project developed with substantial help from AI coding assistants
("vibe coded"). Design decisions are made and reviewed by a human and documented in the
[decision records](docs/adr/); the code is covered by automated tests and checks, but it
has not been audited.

## Disclaimer

Apptrol is provided **as is, without warranty of any kind**, and you use it at your own
risk. The authors are not responsible for any problems or damage that may result from using
it — including changes to your audio setup or to your controller's settings (for example
when using third-party tools such as SysEx Controls). The full terms are in the
[MIT license](LICENSE).

## License

[MIT](LICENSE)

---

Apptrol is an independent project and is not affiliated with, endorsed by or sponsored by
KORG Inc. KORG and nanoKONTROL are trademarks of KORG Inc. GoXLR is a trademark of its
respective owner.
