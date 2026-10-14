# 0031. Manual test cases as docs, with fixtures and a record of every run

- **Status:** Accepted
- **Date:** 2026-10-14

## Context

What needs the real controller, real audio or a person's eyes and ears is tested by hand
before each release (QA-12): the LEDs, moving sliders and knobs, what is heard, the
systemd service, plugging the controller out and in, several users. Until now this was
one table in [testing.md](../testing.md), the *manual hardware checklist* (H-00 to H-51),
and its results went into the release description.

That worked for three release candidates, but it has limits:

- **No fixed starting point.** Each step assumed "a configuration with an app on
  slider 1"; which one, and with which sound source, was up to the tester. A step could
  pass or fail because of the setup, not because of Apptrol.
- **One line per step** holds the action and the expectation together, so a step with
  four actions has one result for all four.
- **Results are not kept in the repository.** The release description says "checklist
  passed"; which steps were skipped, and why, is lost.
- **Not ordered by risk.** The checks that could leave audio muted or too loud are
  spread among the others.

The owner asked for a testing standard (2026-10-14): test cases in a fixed format, one
file per area, predefined configurations, a script that applies them, a how-to, a record
per release, and a rule that every feature adds its cases.

## Decision

1. **Where it lives:** `docs/testing/`, as docs-as-code (ADR 0028), grouped by
   Diátaxis:

   ```text
   docs/testing/
     strategy.md                    explanation: what is tested by hand, what automatically, why
     how-to-run-release-tests.md    how-to: a release run from start to end
     cases/README.md                reference: the case format and the list of areas
     cases/<area>.md                reference: the cases of one area (mute.md, solo.md, …)
     fixtures/<name>.toml           the configurations the cases name
     apply-fixture.sh               puts a fixture in place; --restore undoes everything
     runs/template.md               the blank record of a run
     runs/vX.Y.Z.md                 the record of each release's run
   ```

2. **The case format** (in full in [cases/README.md](../testing/cases/README.md)): an id
   `TC-<AREA>-<NNN>` and a title; `Covers:` with the requirement IDs and the phase (not a
   roadmap line: one roadmap line holds a dozen requirements, and `grep SOLO-06` should
   find the requirement, its Go test and its manual case together); `Config:` with the
   fixture; `Type:` Manual or Automated; setup, steps and result as checkboxes; each step
   one action, `→`, and what is seen or heard. *Automated* cases only point to the Go
   test, for a risk that is covered without hardware.
3. **Areas** follow the requirement groups (HW, CTRL, MUTE, SOLO, LED, STATE, CFG, LOG,
   SVC, MEDIA, LAUNCH, INPUT, …), so a case sits next to the rules it checks. Within a
   file, cases are ordered by risk: first what could leave audio muted or too loud.
4. **Fixtures** are complete configurations in the current format, using only keys from
   [config.md](../config.md). Sound comes from two mpv instances playing a quiet sine
   tone under their own names (`apptrol-test-a`, `apptrol-test-b`), without user
   scripts, so without a media player (MPRIS); a virtual microphone,
   `apptrol-test-mic`, is made the way `make test-audio` makes its inputs. Nothing from
   the tester's own system is in a fixture. Cases that need a media player name one
   (Elisa, VLC), as Spotify changes its own volume (#67). **No general fixture amplifies
   above 100 %**: a fixture is used by many cases, and a control turned up by accident
   must not get loud. Amplification (`max_volume` above 100) gets a fixture of its own,
   used only by the case that tests it, with a warning in its setup.
5. **`apply-fixture.sh` never loses the tester's setup:** the first time, it stops
   Apptrol and moves the real `config.toml` and `state.json` aside; each fixture then
   starts with an empty state; `--restore` stops the tones, removes the virtual
   microphone and puts both files back.
6. **A record per run:** `runs/vX.Y.Z.md`, copied from the template, with the version,
   date, distribution, PipeWire version, hardware, a results table (ID, result, notes),
   one exploratory charter and a summary. It is committed, so it is public: no device
   names or IDs, process numbers, user names, home paths or media titles; notes use
   placeholders (`<pid>`, `<device>`).
7. **The H-checklist is replaced**, not kept next to the cases: its steps become cases
   (each case names the step it replaces, `Was: H-03`), Phase 1 first, then Phase 1.5.
   Until both are done, the checklist stays valid.
8. **The rule** (CLAUDE.md, CONTRIBUTING.md, QA-14): every new feature or change of
   behaviour adds or updates its cases and fixtures, with its docs, roadmap, changelog
   and ADR. A phase is not released without cases for it.
9. **Runs with Claude Code:** `/test-run vX.Y.Z` (`.claude/commands/test-run.md`) walks
   through the cases one at a time and waits for the tester's result after each. During
   a run only, Claude may run `apply-fixture.sh` and read Apptrol's journal; issues for
   failures are drafted, not created without asking.

### Logging

10. This adds no code to Apptrol and so no log records. The script prints each step it
    takes (what it stopped, moved, started) and refuses to run when the backup it
    needs is missing; the cases use Apptrol's existing records as their expected
    results.

## Consequences

- A case passes or fails for the same reason on every run: the setup is part of it.
- More files to keep up: each feature PR now also touches `docs/testing/`. The rule makes
  it part of the definition of done, and a missing case is visible in review.
- Release candidates take longer to test by hand; the order by risk says what to do first
  when time is short.
- Results stay in the repository and show what was skipped and why.
- New requirement QA-14; QA-12 points to the cases once the checklist is replaced.
- The fixtures follow the configuration format: when the separate tables of ADR 0026
  arrive in 0.3.0, they change with it.
