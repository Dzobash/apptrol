# Container runner test cases

These developer checks exercise the runner's restrictions without changing the
installed service. Real systemd checks use the fixture in `test/container/valid.toml`.

### TC-QA-001 — Recipe flags and environment cannot widen container access

Covers: QA-16 · Phase 1.6
Config: none (developer tool check)
Type: Automated
Test: `TestQA16_RecipeMountsEnvironmentAndFlags`, `TestQA16_BadRecipesRefused`, `TestQA16_SymlinkRecipeRefused`

### TC-QA-002 — Both agents block typed container commands

Covers: QA-15, QA-16 · Phase 1.6
Config: none (developer tool check)
Type: Automated
Test: `TestQA16_ContainerCommandsBlocked`, `TestQA15_DenyParity`

### TC-QA-003 — Rootless systemd reports each Status transition

Covers: QA-16, SVC-13 · Phase 1.6
Config: `test/container/valid.toml` (container-only fixture)
Type: Manual

**Setup**
- [ ] Rootless Podman is installed, with delegated memory and pids cgroup controllers

**Steps**
- [ ] Run `make test-systemd` → both distros pass the limits, user-manager and three Status checks
- [ ] Read `.audit/runs.log` → each distro has its own PASS record and recipe hash
- [ ] Both uncached build-probe steps report scope ancestry and memory/swap/pids/nice limits
- [ ] Run `make container-run RECIPE=sample` → the sample recipe passes and adds one record
- [ ] Read `.tmp/` and `.audit/logs/` → successful runs have no leftover workspace or full log

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-QA-004 — The packages install with apt and dnf and start with the Status line

Covers: QA-16, QA-17, SVC-13 · Phase 1.6
Config: `test/container/valid.toml` (container-only fixture)
Type: Manual
Test: `TestQA17_InstallNeedsBothPackages`, `TestQA17_InstallUsesTheSameSandboxAsStatus`, `TestQA17_InstallPassesTheFullCommit`, `TestQA17_PickPackage` (runner and package choice only; the install itself needs real Podman)

**Setup**
- [ ] Rootless Podman is installed, with delegated memory and pids cgroup controllers
- [ ] GoReleaser is installed (`make snapshot` needs it)

**Steps**
- [ ] Run `make test-install` without `dist/` → refused with "run make snapshot", no container starts, no audit line
- [ ] Run `make snapshot` → `dist/` holds a `.deb` and an `.rpm` for this commit
- [ ] Run `make test-install` → Ubuntu installs the `.deb` with apt, Fedora the `.rpm` with dnf; each prints the package file name, the postinstall message and `PASS: apptrol installed by the package manager from <version>`
- [ ] Both distros then report the three Status lines (`controller not found`, `waiting for a valid configuration`, `controller not found`) with the packaged version in front
- [ ] Read `.audit/runs.log` → `recipe=install-ubuntu` and `recipe=install-fedora`, each PASS, none under `systemd-*`
- [ ] Make a commit (or `git commit --allow-empty`), do not run `make snapshot`, run `make test-install` → both fail with "need exactly one … from commit … in dist/"

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-QA-005 — CI summarizes only this attempt's systemd results

Covers: QA-16 · Phase 1.6
Config: none (CI summary check)
Type: Manual
Test: `TestQA16_SystemdSummaryAudit`, `TestQA16_SystemdSummaryWorkflow` (synthetic audits and workflow wiring; the rendered summary needs CI)

**Setup**
- [ ] Open a PR's CI run and its `Systemd in containers` job

**Steps**
- [ ] Read the job summary → the tested merge commit is named, with one Ubuntu 26.04 row and one Fedora 44 row
- [ ] Compare each row with the new `systemd-*` records in the audit artifact → PASS/FAIL and seconds agree; committed history and `install-*` records are excluded
- [ ] For a failed attempt with a distro FAIL record, check that the summary says `❌ fail` and the job stays failed
- [ ] For an attempt missing a distro record, check that its row says `no result` and `—`; a setup failure before both records leaves both rows without results
- [ ] If a cancellation leaves the runner able to finish the summary, check that it reports only recorded results; a force-stopped runner may have no summary
- [ ] Confirm the artifact still contains the full audit and the summary has no streak counter

**Result:** Pass / Fail / Blocked
**Notes:** Record which failure/cancellation scenarios were available; do not claim unobserved CI paths passed.

### TC-QA-006 — `make test-clean` removes kept failed runs, including files of other user IDs

Covers: QA-16, QA-18 · Phase 1.6
Config: none (developer tool check)
Type: Manual
Test: `TestQA18_CleanRemovesOnlyKeptRunsAndLogs`, `TestQA18_CleanWithNothingKeptStillRecords`, `TestQA18_CleanNeverFollowsLinks`, `TestQA18_CleanRefusesLinkedFolders`, `TestQA18_CleanOneFailureDoesNotStopTheRest`, `TestQA18_CleanAuditLabelIsData`, `TestQA18_CleanWaitsForARunInProgress`, `TestQA18_CleanLooksAgainAfterTheLock`, `TestQA18_CleanLeavesFoldersWithMounts` (names, links, lock, mounts, audit and the Podman calls against a fake Podman; the sub-UID files need real Podman)

**Setup**
- [ ] Rootless Podman is installed, as for TC-QA-003
- [ ] A throwaway recipe `.audit/recipes/clean-proof/` (do not commit it) with this `Dockerfile` and `commands.sh`:

```text
Dockerfile:   FROM docker.io/library/ubuntu:26.04
commands.sh:  #!/bin/sh
              set -eu
              mkdir /work/owned-by-other-user
              echo x > /work/owned-by-other-user/file
              chown -R 1000 /work/owned-by-other-user
              chmod 700 /work/owned-by-other-user
              exit 1
```

**Steps**
- [ ] Run `make container-run RECIPE=clean-proof AGENT=<model>` → it fails and prints `Failed run kept: .tmp/run-<id> and .audit/logs/run-<id>.log`
- [ ] `ls -l .tmp/run-<id>/` → `owned-by-other-user` belongs to a high user ID (a subordinate ID), and your own user cannot delete its contents
- [ ] Create `.tmp/keep-me` and `.audit/logs/notes.log` (any content) → they are not run names
- [ ] Run `make test-clean AGENT=<model>` → it prints `removed: .tmp/run-<id>` and `removed: .audit/logs/run-<id>.log`, then `left alone` for the two other names, and ends with `failed 0`
- [ ] `ls .tmp .audit/logs` → only `keep-me` and `notes.log` remain
- [ ] Read `.audit/runs.log` → the failed run's line is still there, followed by one new line with `recipe=@clean`, `image=none`, your `agent=` label and `result=PASS`
- [ ] Run `make test-clean` again with nothing kept → it passes and adds one more `@clean` line
- [ ] Remove the two test names and the throwaway recipe by hand

**Result:** Pass / Fail / Blocked
**Notes:**

### TC-QA-007 — `make test-systemd RUNS=<n>` repeats the check in one call

Covers: QA-16 · Phase 1.6
Config: `test/container/valid.toml` (container-only fixture)
Type: Manual
Test: `TestQA16_RunsDefaultIsOneRunPerDistro`, `TestQA16_RunsRepeatsEachDistro`, `TestQA16_RunsCeilingIsEight`, `TestQA16_RunsStopAfterTheFirstFailure`, `TestQA16_BadRunsRefused` (count, slots, the ceiling of 8, stop after a failure, refused values and Podman's arguments against a fake Podman; real timing needs real Podman)

**Setup**
- [ ] Rootless Podman is installed, as for TC-QA-003
- [ ] No uncommitted changes (`git status --short` is empty), so the lines say `dirty=no`

**Steps**
- [ ] Run `make test-systemd RUNS=01 AGENT=<model>` → refused with "RUNS must be a whole number from 1 to 1000", no container starts, no audit line
- [ ] Run `make test-systemd RUNS=16 AGENT=<model>` → the binary is built once, each distro's first run builds and proves its image, the later runs print "no build here"
- [ ] The summary says `up to 8 at once` (fewer only when memory is short), `systemd-ubuntu: 16 passed, 0 failed, 0 not started`, the same for Fedora, and a wall time
- [ ] Read `.audit/runs.log` → 16 new PASS lines per distro, each with its own `run=`, all `dirty=no`
- [ ] Read `.tmp/` and `.audit/logs/` → no leftovers from the passed runs
- [ ] Start another repeated call and press Ctrl-C while checks are running, then press it again while stopping → no further runs are dispatched; started runs finish, clean up and each append one complete PASS/FAIL line; their per-distro summary is printed before exit. The runner returns 130 (Make may wrap it). Repeat with default `RUNS=1`, install and a recipe. Waiting has no finite upper bound; see ADR 0034's cancellation note. Automated gate coverage: `TestQA16_CancellationDrainsTransactions`.

**Result:** Pass / Fail / Blocked
**Notes:** Record the wall time and the `jobs=` value, to estimate a RUNS=300 call (docs/testing.md › Collecting the #172 evidence).
