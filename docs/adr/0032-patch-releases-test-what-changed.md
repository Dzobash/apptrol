# 0032. Patch releases test what changed, plus a smoke set

- **Status:** Accepted
- **Date:** 2026-10-16

## Context

QA-12 asks for the whole manual hardware checklist before **every** release: 52 steps,
H-00 to H-51, plus an exploratory session and a run record (ADR 0031). For a new phase
(MINOR) that is right: much has changed, and the checklist is how a phase is accepted.

For a fix release (PATCH) it is not. 0.2.2 changes four small things (#110, #118, #119,
#120), each with an automated test that failed without the fix (QA-11). Testing all 52
steps by hand for that takes about half a day at the controller, and a cost that high
makes fix releases wait: #118 is a microphone that can stay live while Apptrol shows it
muted, and every day without a release leaves it in the version users have.

The requirement IDs needed to choose fewer steps are already there: every commit names
the IDs it changes (`fix: … (MUTE-07, INPUT-03)`), and every checklist step names the IDs
it checks (its *Req.* column).

The owner decided on 2026-10-16: smaller tests for fix releases, the changes tested in
full, plus smoke tests for what matters most.

## Decision

1. **A MINOR release** (a new phase) runs the whole checklist, as before.
2. **A PATCH release** runs:
   - **the smoke set:** H-00 (every control and LED), H-01 (starts and connects),
     H-03 (a slider sets an app's volume), H-08 (M mutes and unmutes). Together these
     show that Apptrol starts, hears the controller, changes audio and lights the LEDs.
   - **every step that checks a changed requirement:** the IDs of the code commits
     since the last release, matched against the *Req.* column:

     ```bash
     git log --format=%s v0.2.1..HEAD | grep -E '^(fix|feat|perf|refactor)' \
       | grep -oE '\b[A-Z]+-[0-9]+\b' | sort -u
     ```

     `docs:`, `test:`, `ci:` and `chore:` commits change no behaviour and add no steps.
   - **the exploratory session** (ADR 0031), aimed at what changed.
3. **The tester may widen it, never narrow it.** A code change without a requirement ID,
   or one that touches much of `internal/mixer`, adds the steps of the area it touches; a
   failed step may call for the whole checklist. The run record says what was added and
   why. A changed requirement without any step is covered by its automated test (QA-11);
   the run record names it.
4. **The run record says the scope:** *Full*, or *Patch* with the IDs and the steps they
   gave, so a reader sees what was not tested.
5. **When the checklist becomes test cases** (ADR 0031, point 7), the same rule applies
   to the cases: the smoke set is the cases that replace H-00, H-01, H-03 and H-08, and
   `Covers:` takes the place of the *Req.* column.

For 0.2.2 this gives 11 steps instead of 52:

```text
code commits since v0.2.1 ─► CFG-06 HW-08 INPUT-03 MEDIA-01 MUTE-07 SVC-11
                                  │ matched against "Req."
                                  ▼
changed:  H-01 H-15 H-20 H-22 H-29 H-37 H-40 H-47
smoke:    H-00 H-03 H-08 (H-01 is in both)
```

### Logging

6. This changes no code and adds no log records.

## Consequences

- A fix release takes about an hour at the controller instead of half a day, so fixes
  ship sooner.
- A bug in an area no commit touched can reach a patch release unnoticed. The risk is
  small while patch releases change little code, and the automated tests still cover
  every MUST requirement that can be tested without hardware (QA-07).
- The quality of a patch run depends on the commit messages naming their requirement IDs,
  which CLAUDE.md and CONTRIBUTING already ask for.
- QA-12 changes: the whole checklist for MINOR releases, this selection for PATCH.

## Notes

- 2026-10-16: A release may differ from the candidate that passed in files that change
  nothing that is built or packaged: tests (`*_test.go`), `.gitignore`, and check-only
  `Makefile` targets such as `fmt` ([releasing.md](../releasing.md), step 3). Before, only
  docs and the AppStream `<release>` entry were allowed. v0.2.2 was the first: since
  v0.2.2-rc1 only such files had changed (#128, #131), so it was released without an rc2.
- 2026-10-27: The scope command in point 2 ends at the **tag of the tested candidate**,
  not at `HEAD` (`v0.2.1..v0.2.2-rc1`): commits merged after the candidate are not in
  what is tested ([#200](https://github.com/Dzobash/apptrol/issues/200),
  [how-to](../testing/how-to-run-release-tests.md#choosing-what-to-run)).
