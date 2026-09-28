# Apptrol

**Control each app's volume on Linux with a hardware MIDI controller.**
A per-app volume mixer for PipeWire, built for the Korg nanoKONTROL2.

> **Status: planning.** Requirements are agreed for the first version; code has not been
> written yet. See the [roadmap](docs/roadmap.md).

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

## Requirements

- Linux with PipeWire (`pipewire-pulse`) or PulseAudio
- A Korg nanoKONTROL2 in its default CC mode. For LED feedback, set the LED mode to
  **External** once in Korg's KONTROL Editor.

## Installation

No release yet. Once there is one, download the package for your system from the
[Releases](https://github.com/Dzobash/apptrol/releases) page and install it:

```bash
sudo apt install ./apptrol_*_amd64.deb      # Debian, Ubuntu, Kubuntu
sudo dnf install ./apptrol-*.x86_64.rpm     # Fedora
```

Then start it for your user:

```bash
systemctl --user daemon-reload
systemctl --user enable --now apptrol
```

## Documentation

- [Requirements](docs/requirements.md)
- [Roadmap](docs/roadmap.md)
- [Configuration reference](docs/config.md) and [example config](examples/config.toml)
- [Decision records](docs/adr/)
- [Testing](docs/testing.md)
- [Releasing](docs/releasing.md)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## About this project

Apptrol is developed with substantial help from AI coding assistants. Design decisions are
made and reviewed by a human and documented in the [decision records](docs/adr/).

## License

[MIT](LICENSE)

---

Apptrol is an independent project and is not affiliated with, endorsed by or sponsored by
KORG Inc. KORG and nanoKONTROL are trademarks of KORG Inc. GoXLR is a trademark of its
respective owner.
