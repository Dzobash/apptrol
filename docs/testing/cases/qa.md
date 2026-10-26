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
Test: `TestQA17_InstallNeedsBothPackages`, `TestQA17_InstallUsesTheSameSandboxAsStatus` (runner only; the install itself needs real Podman)

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
