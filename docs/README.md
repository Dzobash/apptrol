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
- [Running the release tests](testing/how-to-run-release-tests.md): the manual test
  cases on a release candidate, with fixtures, an exploratory charter and a run record
- [Releasing](releasing.md): making a release and release candidates

## Reference

Looking something up.

- [Configuration reference](config.md): every setting of `config.toml`
- [Reading the logs](logging.md): where the logs are, how to search them, and every
  attribute
- [Requirements](requirements.md): every behaviour with its ID
- [Test cases](testing/cases/README.md): the format of a manual test case, and the cases
  by area
- [Configuration test cases](testing/cases/cfg.md): misplaced app settings and their diagnostics
- [Test run template](testing/runs/template.md): the record of a release's test run;
  every run is in [testing/runs/](testing/runs/)
- [Brand guide](brand.md): logo, colours, type and how to use them

## Explanation

Understanding why Apptrol works the way it does.

- [Architecture](architecture.md): packages, event flow, and how Apptrol talks to the
  controller, the audio server and the desktop
- [Testing strategy](testing/strategy.md): what is tested by hand and what
  automatically, and why
- [Decision records](adr/): every design decision with its reasons

## Project

Where Apptrol is going and how to take part.

- [Roadmap](roadmap.md): phases, what is done and what comes next, scope
- [Changelog](https://github.com/Dzobash/apptrol/blob/main/CHANGELOG.md)
- [Contributing](https://github.com/Dzobash/apptrol/blob/main/CONTRIBUTING.md)
