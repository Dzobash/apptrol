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

> **Version 0.2.0:** sliders and knobs, mute and solo, media keys, app launchers and
> microphone buttons all work. It is young software, so please
> [report problems](https://github.com/Dzobash/apptrol/issues). The
> [roadmap](docs/roadmap.md) shows what comes next.

## What it does

<p align="center">
  <img src="docs/assets/photos/nanokontrol2-controls.png" alt="The Korg nanoKONTROL2 with its controls marked: sliders, knobs, S and M since 0.1.0; the R buttons, the transport buttons as media keys, and Marker and Record as app launchers since 0.2.0; Track and Cycle are planned for 0.4.0 to switch layouts" width="800"><br>
  <sub>Photo: jzohsuh / <a href="https://www.ifixit.com/Guide/Korg+nanoKONTROL2+Disassembly/117910">iFixit</a>, <a href="https://creativecommons.org/licenses/by-nc-sa/3.0/">CC BY-NC-SA 3.0</a>, annotated.</sub>
</p>

Put Spotify on the first slider, your browser on the second, Discord on the third and your
microphone on the last one. Moving a slider or knob changes that app's volume directly,
with no desktop mixer involved.

- **Sliders and knobs** each control one app or one input, such as your microphone.
- **M** mutes, **S** solos, **R** plays or pauses the app; the LEDs show it, also when you
  change something in the desktop's volume settings.
- **◀◀ ▶▶ ■ ▶** control the music player that played last.
- **●**, the **Marker** buttons and any **R** can start an app, but not while the screen is
  locked.
- On the **microphone**, hold S to cough, or hold M to talk while the music goes down.
- Settings live in a small text file that is reloaded when you save it. Apptrol runs
  quietly in the background, on any desktop.

## Why Apptrol exists

I use a GoXLR Mini as my audio deck, but its few faders can't give every app its own:
music, browser, Discord and games all have to share them. So I bought a Korg
nanoKONTROL2, with eight faders, eight knobs and lit buttons, and looked for an
open-source app that would make each fader the volume of one app on Linux.

I didn't find one, so I made Apptrol: I designed it and made every decision, and AI coding
assistants wrote most of the code. It turns an off-the-shelf MIDI controller into a
per-app mixer that runs quietly next to whatever else controls your audio.

And there's a simpler reason, too: it's satisfying to reach out and pull a real fader
down, instead of opening a mixer window and chasing a tiny slider with the mouse. Every app
gets its own place under your fingers, and you can adjust it without looking away from
what you're doing.

## What's next

- **0.3.0:** an on-screen display when you move a slider or mute an app
- **0.4.0:** several layouts, e.g. *Work* and *Gaming*, switched with Track and Cycle
- **0.5.0:** a setup in the terminal and a tray icon

The [roadmap](docs/roadmap.md) has the details.

## Before you install

- **Your system:** any normal Linux desktop with PipeWire (or PulseAudio) and systemd. No
  extra libraries are needed.
- **The controller:** set its **LED mode to External**, so the LEDs can show mute and
  solo. Everything else works with the factory settings. `apptrol test` checks it.
- **Permissions:** your normal desktop login can use the controller. Only unusual setups,
  such as running Apptrol over SSH, need your user in the `audio` group.

The [installation guide](docs/install.md) explains each point, how to change the LED mode,
and which systems Apptrol has been tested on.

## Installation

Download the package for your system from the
[Releases](https://github.com/Dzobash/apptrol/releases) page, install it, and start
Apptrol for your user:

```bash
sudo apt install ./apptrol_*_amd64.deb      # Debian, Ubuntu, Kubuntu
sudo dnf install ./apptrol-*.x86_64.rpm     # Fedora

systemctl --user daemon-reload
systemctl --user enable --now apptrol
```

Upgrading, building from source, checking a download and removing Apptrol are in the
[installation guide](docs/install.md).

## First steps

1. **Find the names** of the apps that are playing: `apptrol list`.
2. **Assign them** in `~/.config/apptrol/config.toml`, which Apptrol creates on its first
   start. The [configuration reference](docs/config.md) explains every setting.
3. **Check the file** with `apptrol check`; Apptrol picks up the changes when you save.

If a control does nothing, [Reading Apptrol's logs](docs/logging.md) shows how to find out
why.

## Known issues

- **Spotify resets its volume when the track changes** ([#67](https://github.com/Dzobash/apptrol/issues/67)).
  Touch its slider to set it back.
- **R pauses only one browser tab**, the one whose player you used last ([#68](https://github.com/Dzobash/apptrol/issues/68)).
- **In 0.2.0, a restart with an invalid configuration loses the saved slider positions and
  mutes** ([#77](https://github.com/Dzobash/apptrol/issues/77)). Run `apptrol check`
  before restarting.
- **In 0.2.0, after *Switch user* the controller still acts on the first user's apps**
  ([#78](https://github.com/Dzobash/apptrol/issues/78)).

All known issues, with their causes and workarounds: [Known issues](docs/known-issues.md).
Found another one? Please [open an issue](https://github.com/Dzobash/apptrol/issues).

## Documentation

All pages, grouped by what you want to do: [documentation overview](docs/README.md).

- [Installation guide](docs/install.md) and [known issues](docs/known-issues.md)
- [Configuration reference](docs/config.md) and [example config](examples/config.toml)
- [Reading the logs](docs/logging.md)
- [Roadmap](docs/roadmap.md), [requirements](docs/requirements.md) and
  [architecture](docs/architecture.md)
- [Decision records](docs/adr/), [testing](docs/testing.md) and
  [releasing](docs/releasing.md)
- [Brand guide](docs/brand.md) (logo, colours, type)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Everyone taking part follows the
[code of conduct](CODE_OF_CONDUCT.md).

## About this project

Apptrol is a hobby project developed with substantial help from AI coding assistants
("vibe coded"). Design decisions are made and reviewed by a human and documented in the
[decision records](docs/adr/); the code is covered by automated tests and checks, but it
has not been audited.

## License and disclaimer

[MIT](LICENSE). Apptrol is provided **as is, without warranty of any kind**, and you use it
at your own risk; the authors are not responsible for problems or damage it may cause,
including changes to your audio setup or to your controller's settings. Commands you set
up for launcher buttons run with your rights and are your responsibility (see
[Blocked commands](docs/config.md#blocked-commands)).

The controller photo is licensed under CC BY-NC-SA 3.0, see
[docs/assets/photos](docs/assets/photos/README.md). The libraries compiled into Apptrol
keep their own licenses (MIT, BSD, Apache-2.0); the packages and archives contain them in
`THIRD_PARTY_LICENSES`.

---

Apptrol is an independent project and is not affiliated with, endorsed by or sponsored by
KORG Inc. KORG and nanoKONTROL are trademarks of KORG Inc. GoXLR is a trademark of its
respective owner.
