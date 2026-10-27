# Running the release tests

How to run the manual test cases on a release candidate, record the results and finish
with your own setup back in place. Why these tests are manual is in
[Testing strategy](strategy.md); the case format is in [Test cases](cases/README.md).

```text
install the candidate ─► restart Apptrol ─► copy the run template
        │
        ▼
for each case:  apply its fixture ─► do the steps ─► note Pass / Fail / Blocked
        │
        ▼
exploratory charter ─► --restore ─► summary, issues ─► owner's decision
        │
        ▼
commit runs/vX.Y.Z-rcN.md
```

## Before you start

- **Install the release candidate** and restart Apptrol in **every** user's session that
  runs it: a running Apptrol keeps the version it started with. Check the running
  version in the log: `journalctl --user -u apptrol | grep "Apptrol starting"` names
  `service.version`.
- **The controller:** a Korg nanoKONTROL2 in CC mode, buttons *Momentary*, LED mode
  *External* ([install guide](../install.md)).
- **Tools:** `mpv` (the test tones), `pactl` (the virtual microphone), `notify-send` (the
  Record launcher of the `full-layout` fixture).
- **Hearing safety:** set the system volume to about 20 % and do not wear headphones for
  the first case. The test tones are quiet sine tones, turned down to 40 % inside mpv,
  and no general fixture goes above 100 %. Only a case that tests amplification
  (`max_volume` above 100) uses a fixture that does, and says so in its setup
  ([Hearing safety](../config.md#hearing-safety)).

  ```text
  sine tone ─► mpv --volume=40 ─► stream volume (Apptrol) ─► system volume ~20 % ─► speakers
  ```

  On the first case, check your desktop's volume settings: `apptrol-test-a` shows the
  slider's value, not 40 %. If it shows 40 %, note it in the run record.
- **Make a run record:** copy [runs/template.md](runs/template.md) to a file named after
  the tag of the candidate you test, `runs/vX.Y.Z-rcN.md` (e.g. `runs/v0.2.2-rc1.md`),
  and fill in the header. One record per tested candidate: a second candidate gets its
  own record, and the first one stays as it is.

  ```text
  v0.3.0-rc1 tested ─► runs/v0.3.0-rc1.md   (a case failed, fixed)
  v0.3.0-rc2 tested ─► runs/v0.3.0-rc2.md   (passed ─► release v0.3.0)
  ```

## Choosing what to run

A MINOR release (a new phase, `0.3.0`) runs every case. A PATCH release (`0.2.2`) runs
the smoke set and the cases that check what changed
([ADR 0032](../adr/0032-patch-releases-test-what-changed.md)):

1. **List the changed requirements:** the IDs in the code commits from the last
   release's tag to the tag of the candidate you test (not `docs:`, `test:`, `ci:` or
   `chore:`). End at the candidate's tag, not at `HEAD`: commits merged after the
   candidate are not in what you test.

   ```bash
   git log --format=%s v0.2.1..v0.2.2-rc1 | grep -E '^(fix|feat|perf|refactor)' \
     | grep -oE '\b[A-Z]+-[0-9]+\b' | sort -u
   ```

2. **Find their cases:** every case whose `Covers:` (in the hardware checklist, the
   *Req.* column) names one of those IDs.
3. **Add the smoke set:** H-00, H-01, H-03 and H-08 (every control and LED, starting and
   connecting, a slider's volume, M's mute).
4. **Write the scope** in the run record: the IDs and the cases they gave. You may add
   cases (a change without an ID, a failure that worries you), with the reason; never
   leave one of the selected cases out.

```text
v0.2.2:  CFG-06 HW-08 INPUT-03 MEDIA-01 MUTE-07 SVC-11
           → H-01 H-15 H-20 H-22 H-29 H-37 H-40 H-47  + smoke H-00 H-03 H-08  = 11
```

## Running a case

1. **Apply the fixture** named on its `Config:` line, unless it is `none` or already in place:

   ```bash
   docs/testing/apply-fixture.sh two-apps
   ```

   The first time, it moves your `config.toml` and `state.json` aside; every fixture then
   starts with an empty saved state, two test tones (`apptrol-test-a` at 440 Hz,
   `apptrol-test-b` at 660 Hz) and a virtual microphone (`apptrol-test-mic`).
2. **Do the setup**, then **each step**: one action, then check what follows the `→`.
3. **Write the result** in the run record: `Pass`, `Fail` or `Blocked`
   ([what each means](#what-passed-means)), with a note for anything but a plain pass.
4. **A failure** gets an issue: what you did, what you expected, what happened, and the
   log lines (`journalctl --user -u apptrol --since "10 min ago"`), with placeholders
   for anything from your system.

Run the cases in the order of the area files: within each file, the cases that could
leave audio muted or too loud come first. Short on time, run the first case of every
area before the rest.

## The exploratory charter

Each release also gets one **exploratory session**: 30 minutes without a script, looking
for what no case describes. It is part of every scope, MINOR or PATCH, and a session of
its own after the cases: time spent off-script while running the cases does not count.
Write the charter in the run record before you start:

```text
Explore   <the area or feature>
with      <the setup, the fixture, the tools>
to find   <the kind of problem>
```

Pick the area that changed most in the release. For example, for 0.2.2:

```text
Explore   starting Apptrol while the controller and the session are not ready yet
with      two user accounts, Switch user, plugging the controller in during a login
to find   false errors in the log, or a controller nobody can use
```

Note what you tried and what you found in the run record. A problem becomes an issue; an
idea for a missing case becomes a case in the same pull request as the run record.

## Finishing

1. Put your own setup back:

   ```bash
   docs/testing/apply-fixture.sh --restore
   ```

   It stops the test tones, removes the virtual microphone and the test log file, and
   puts your `config.toml` and `state.json` back.
2. Write the **summary** in the run record: how many cases passed, failed, were blocked,
   and every failed or blocked case with its issue or reason.
3. Get the **owner's decision** and write it at the end of the record
   ([what passed means](#what-passed-means)).
4. **Check the run record for private data** before committing: no device names or IDs,
   process numbers, user names, home paths or media titles. Measured values keep their
   form (`<pid>`, `<device>`, `/home/you`).
5. Commit it in a pull request (`docs: release test run vX.Y.Z-rcN`). When the run has
   passed, the release is made as in [Releasing](../releasing.md), from the code of this
   candidate.

## What passed means

Each case gets one of three results:

```text
Pass      every step did what follows its →
Fail      a step did something else; the failure gets an issue
Blocked   the case could not be run; the note says why (e.g. no second user account)
```

A run has **passed**, and its candidate may be released, only when one of these holds:

- every case passed; or
- every case that did not pass has the owner's written decision: a **waiver** for a
  blocked case (why it is safe to release without it), or an **acceptance** for a failed
  case (its issue, and the version that will fix it).

An agent never decides alone that a blocked or failed case is fine. It drafts the
decision lines; the owner confirms them. The record ends with the decision, one line per
case that did not pass:

```text
Decision: release v0.3.0 — owner, 2026-11-02
  TC-MEDIA-001   Blocked   waived: needs Switch user; login-screen code unchanged since v0.2.2
  TC-QA-006      Blocked   waived: needs files of another user ID; make test-clean unchanged
  TC-OUT-007     Fail      accepted: LED only, cosmetic; fix in 0.3.1 (#210)
```

When every case passed, the decision is the first line alone. **Without the decision
line, the run has not passed**, even when every case did. When the owner does not
release, the decision says so, and the fix gets a new candidate with its own record:

```text
Decision: no release; fix #210, then v0.3.0-rc2 — owner, 2026-11-02
```

## With Claude Code

In a Claude Code session in this repository, type:

```text
/test-run v0.2.2-rc1
```

Claude creates the run record from the template, applies each case's fixture and shows
you the whole file, gives every change a case makes to it as lines to paste, tells you
what to do, waits for your result, reads Apptrol's log, records the result and drafts an
issue for a failure (it asks before creating it). At the end it writes the summary and
drafts the decision lines for you to confirm. The
command is [`.claude/commands/test-run.md`](https://github.com/Dzobash/apptrol/blob/main/.claude/commands/test-run.md).
