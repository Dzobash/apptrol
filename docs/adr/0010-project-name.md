# 0010. Project name "Apptrol"

- **Status:** Accepted
- **Date:** 2026-09-28

## Context

The obvious names build on the controller's name (nanoKONTROL2). KORG and its product names
are trademarks, and names that imitate them (or their styling, e.g. "nano…" with capitals)
could lead to a forced rename after publication.

## Decision

- The project is called **Apptrol** ("app" + "control"): repo, binary, service, config
  directory and packages all use `apptrol`.
- The description says what it does and which device it is built for:
  *"Control each app's volume on Linux with a hardware MIDI controller. A per-app volume
  mixer for PipeWire, built for the Korg nanoKONTROL2."*
- The README states that the project is not affiliated with KORG.
- GitHub topics include `nanokontrol2`, `korg`, `midi`, `pipewire`, `volume-control` so
  owners of the device can find it.

## Consequences

- The name does not explain the project by itself; the description does that.
- A quick check (GitHub, AUR, Flathub, Ubuntu packages, web) found no conflicting project.
