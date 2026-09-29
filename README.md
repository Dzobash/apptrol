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

> **Status: first release.** Everything in Phase 1 works: sliders and knobs, mute, solo,
> LED feedback, saved positions, automatic configuration reload. It is young software, so
> please [report problems](https://github.com/Dzobash/apptrol/issues). See the
> [roadmap](docs/roadmap.md) for what comes next.

## What it does

<p align="center">
  <img src="docs/assets/photos/nanokontrol2.png" alt="A Korg nanoKONTROL2: eight columns, each with a knob, a slider and S, M and R buttons, plus transport buttons on the left" width="800"><br>
  <sub>The Korg nanoKONTROL2. Photo: jzohsuh / <a href="https://www.ifixit.com/Guide/Korg+nanoKONTROL2+Disassembly/117910">iFixit</a>, <a href="https://creativecommons.org/licenses/by-nc-sa/3.0/">CC BY-NC-SA 3.0</a>, background removed.</sub>
</p>

Put Spotify on the first slider, your browser on the second, Discord on the third and your
microphone on the last one. Moving a slider or knob changes that app's volume directly,
with no desktop mixer involved.

- Every slider and knob can control one app or one input (e.g. your microphone)
- Per app, the top of the slider can be up to 150 % (a boost) or less than 100 % (a cap)
- **M** mutes the app on that slider, **S** solos it
- The button LEDs show what is muted and soloed
- Apps that start later get the slider's volume straight away
- `apptrol list` shows the names to put in the config; `apptrol test` checks the controller
- Settings live in a simple TOML file that is reloaded when you save it
- Runs quietly in the background as a systemd user service
- Works on any desktop (KDE Plasma, GNOME, …), Wayland or X11

It works alongside an audio interface such as a GoXLR, or on its own with a normal sound card.

Planned later: media buttons, on-screen volume display, multiple layouts (e.g. *Work* and
*Gaming*) and a GUI.


## Why Apptrol exists

I use a GoXLR Mini as my main audio deck. It has faders for a few fixed channels, but not
for every app I use: music, browser, Discord, games and chat all compete for the same few
sliders. So I bought a Korg nanoKONTROL2 (eight faders, eight knobs, lit buttons) hoping to
find an open-source app that would turn each of its sliders into the volume control for one
app, on Linux.

I didn't find one. Windows has tools like this; on Linux, projects such as
[deej](https://github.com/omriharel/deej) need a self-built Arduino mixer, and generic MIDI
tools don't know about per-app volume in PipeWire. So Apptrol was built to fill that gap:
it turns an off-the-shelf MIDI controller into a per-app mixer that runs quietly next to
whatever else controls your audio.

And there's a simpler reason, too: it's satisfying to reach out and pull a real fader
down, instead of opening a mixer window and chasing a tiny slider with the mouse. Every app
gets its own place under your fingers, and you can adjust it without looking away from
what you're doing.

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

Download the package for your system from the
[Releases](https://github.com/Dzobash/apptrol/releases) page and install it:

```bash
sudo apt install ./apptrol_*_amd64.deb      # Debian, Ubuntu, Kubuntu
sudo dnf install ./apptrol-*.x86_64.rpm     # Fedora
```

## First steps

1. **Write your configuration** in `~/.config/apptrol/config.toml`. Apptrol creates it from
   the example on its first start; to begin editing before that, copy the example:
   ```bash
   mkdir -p ~/.config/apptrol
   cp /usr/share/doc/apptrol/examples/config.toml ~/.config/apptrol/
   ```
   `apptrol list` shows the names of the apps that are currently playing and of your input
   devices; `apptrol check` validates the file and shows which app is on which control.
   The [configuration reference](docs/config.md) explains every setting.
2. **Try it** in a terminal: run `apptrol`, move a slider, and watch the log. Ctrl+C stops
   it.
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

[MIT](LICENSE). The controller photo is not part of that: it is licensed under
CC BY-NC-SA 3.0, see [docs/assets/photos](docs/assets/photos/README.md).

---

Apptrol is an independent project and is not affiliated with, endorsed by or sponsored by
KORG Inc. KORG and nanoKONTROL are trademarks of KORG Inc. GoXLR is a trademark of its
respective owner.
