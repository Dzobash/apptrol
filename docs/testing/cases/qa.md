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
