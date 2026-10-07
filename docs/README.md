# Apptrol documentation

The pages are grouped by what you want to do, following [Diátaxis](https://diataxis.fr/):
learn, get something done, look something up, or understand why. The website uses this
page for its navigation; every page in `docs/` belongs to one group.

## Tutorials

Learning by doing, from start to finish.

- None yet. The first one is planned as **"From install to your first slider"**:
  install Apptrol, set the controller's LED mode, find an app with `apptrol list`, put it
  on slider 1, check the file and move the slider.

## How-to guides

Getting a task done.

- [Installing Apptrol](install.md): what your system needs, controller settings,
  permissions, upgrading, building from source, removing
- [Known issues](known-issues.md): problems with their causes and what to do
- [Testing](testing.md): running the checks, what CI runs, writing tests, the hardware
  checklist
- [Releasing](releasing.md): making a release and release candidates

## Reference

Looking something up.

- [Configuration reference](config.md): every setting of `config.toml`
- [Reading the logs](logging.md): where the logs are, how to search them, and every
  attribute
- [Requirements](requirements.md): every behaviour with its ID
- [Brand guide](brand.md): logo, colours, type and how to use them

## Explanation

Understanding why Apptrol works the way it does.

- [Architecture](architecture.md): packages, event flow, and how Apptrol talks to the
  controller, the audio server and the desktop
- [Decision records](adr/): every design decision with its reasons

## Project

Where Apptrol is going and how to take part.

- [Roadmap](roadmap.md): phases, what is done and what comes next, scope
- [Changelog](https://github.com/Dzobash/apptrol/blob/main/CHANGELOG.md)
- [Contributing](https://github.com/Dzobash/apptrol/blob/main/CONTRIBUTING.md)
