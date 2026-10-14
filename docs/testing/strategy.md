# Testing strategy: what is tested by hand, and why

Apptrol is tested in two ways. **Automated tests** run on every push and check the logic:
what Apptrol decides. **Manual test cases** run before every release on a real controller
and check what only the real world can show: what you see on the LEDs, what you hear,
and how the system around Apptrol behaves. This page explains where the line between
them is. The decision is [ADR 0031](../adr/0031-manual-test-cases-as-docs.md); the
reasoning for the automated tests is [ADR 0012](../adr/0012-testing-strategy.md).

```text
                       automated (every push, CI)          manual (every release)
what it checks         decisions: events in, actions out   the result in the real world
needs                  nothing: fakes for everything       controller, speakers, a person
example                "pressing S again ends solo"        "S1 goes dark and both tones
                       (TestSOLO05_…)                       are heard again" (TC-SOLO-…)
when it fails          before merging                      before releasing
```

## What is automated

Everything that can be decided without hardware, a desktop or a person (QA-07, QA-10):

- **The mixer** (`internal/mixer`): volume from a control's position, mute and solo,
  priority of the controller, LED states, held buttons, talk-over, the screen states.
  It is pure: events in, actions out (ADR 0015), so every rule has a test named after its
  requirement, e.g. `TestSOLO05_PressingSoloAgainTurnsItOff`.
- **Configuration and saved state:** parsing, validation, every error message, reload,
  and fuzz tests that feed them random input (QA-08).
- **MIDI decoding** and the controller adapter, against a fake device: plugging in and
  out, permissions, the grace after starting (HW-08).
- **Integration tests** against real programs without hardware: a headless PipeWire
  (`make test-audio`, in CI), a private D-Bus with fake media players and a fake logind
  (`make test-desktop`, in CI), the user's systemd for launchers (`make test-launcher`,
  run before a release).

The commands are in [Testing](../testing.md#running-the-checks).

## What is manual, and why

A manual case is written when the result can only be seen, heard or felt:

| Needs | Why a fake is not enough | Examples |
|---|---|---|
| **The real controller** | The fake sends what we expect the device to send; the device may do otherwise (LED mode, a slider's last value, a USB reset) | LEDs light up; every slider reaches 0 and 127; unplug and plug in |
| **Real audio** | The audio server may apply a change differently than reported (a stream that resets its own volume, a new stream's first second) | The tone gets quieter; a muted app is silent; nothing is suddenly loud |
| **A person's judgment** | "Smooth", "at once", "too loud" are not numbers in a test | The volume follows the slider without jumps |
| **The real service and session** | systemd, logind and the desktop decide timing and rights | Start at login; *Switch user*; lock screen; restart after a crash |

Manual cases are ordered by **risk**: first what could leave audio **muted** or **too
loud** (stopping or crashing during solo or mute, `max_volume` above 100 %, an invalid
configuration, restoring after a restart), then the rest. When time before a release is
short, the order says what to test first.

An **Automated** case in the manual list only points to its Go test. It is there when a
risk is covered without hardware, so the list of risks stays complete in one place.

## How a release is tested

1. The automated tests pass (they gate every merge).
2. A release candidate is installed, and the manual cases are run with
   [fixtures](fixtures/): fixed configurations, applied by `apply-fixture.sh`, with two
   quiet test tones from mpv as the sound sources. Every run starts from the same setup.
3. One **exploratory charter** per release: a short, open session looking for what no case
   describes.
4. The results are recorded in `runs/vX.Y.Z.md` and committed.

The steps are in [Running the release tests](how-to-run-release-tests.md); the case
format is in [Test cases](cases/README.md).

## Keeping the cases current

Every new feature or change of behaviour adds or updates its cases and fixtures, in the
same pull request as its docs, roadmap entry, changelog line and ADR. A phase is not
released without cases for it (QA-14).
