---
description: Walk through the manual release test cases for a version, one at a time
argument-hint: vX.Y.Z (e.g. v0.2.2-rc1)
---

We are doing the manual test run for $ARGUMENTS. Follow
docs/testing/how-to-run-release-tests.md. Create docs/testing/runs/$ARGUMENTS.md from
docs/testing/runs/template.md and walk me through the cases one at a time: apply the
fixture, tell me what to do, and wait for my result before continuing. Record each result
and note failures as GitHub issues (draft the text, don't create them without asking). At
the end, write the summary and list any failed or blocked cases.

Rules for this run (ADR 0031):

- **Commands.** During this run only, you may also run `docs/testing/apply-fixture.sh`
  and read Apptrol's journal (`journalctl --user -u apptrol …`, or
  `journalctl _SYSTEMD_USER_UNIT=apptrol.service …` for several users). Say in one
  sentence before each what you are doing and why; afterwards, what it showed. Everything
  else stays as AGENTS.md says.
- **One case at a time.** Apply the case's fixture only when it differs from the one in
  place. Give the setup and the steps as the case writes them, then stop and wait for my
  result. After it, read the log since the case started and say whether it matches the
  expected lines. Write the result into the run record before the next case.
- **Scope first** (ADR 0032). Before the first case, say whether $ARGUMENTS is a MINOR
  or a PATCH release. For a PATCH, list the requirement IDs of the code commits since
  the last release tag, the cases they select, and the smoke set, as in "Choosing what
  to run" of the how-to; show me the list and write it into the record's *Scope* row.
  Run only those, unless I add cases.
- **Order.** The area files in docs/testing/cases/, each from its first case: the ones
  that could leave audio muted or too loud come first. Ask before skipping a case.
  While a phase has no area files yet, use its rows of the hardware checklist in
  docs/testing.md the same way (the row's number as the ID), with the fixture that fits
  best, and say which one you chose.
- **Privacy.** The run record is committed and public: no device names or IDs, process
  numbers, user names, home paths or media titles from my system. Use placeholders
  (`<pid>`, `<device>`, `/home/you`); measured values keep their form, not their value.
  Check the file for them before suggesting a commit.
- **At the end.** Run `apply-fixture.sh --restore`, help me write the exploratory
  charter and its notes, write the summary, and list the failed and blocked cases with
  their draft issues. Ask before committing, pushing or creating issues.
