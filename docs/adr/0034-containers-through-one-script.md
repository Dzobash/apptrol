# 0034. Containers: rootless Podman through one script, with an audit record of every run

- **Status:** Accepted
- **Date:** 2026-10-21

**2026-10-23 decision update:** [ADR 0037](0037-builds-in-a-limited-user-scope.md)
supersedes the build-limit mechanism in point 4: Podman ignored `pids_limit` during
builds, so builds run inside a limited, delegated user scope, and the host proves
runtime limits before image code starts. The limits themselves are unchanged.

## Context

Coding agents need containers for two things:

- **Checks with a real systemd.** PR #167 (#99, SVC-13) was checked by hand in two
  throwaway containers, Ubuntu 26.04 and Fedora, with systemd as PID 1 and an ordinary
  user with a user manager. The check worked; a CI job should repeat it on every pull
  request.
- **Trying something while writing code**, e.g. CI's older PipeWire.

Until now every `docker` command needed the owner's OK, because it is not on the agents'
command lists (AGENTS.md, QA-15). That did not work well:

- **Too many prompts, and prompts that say too little.** A prompt that named only
  `/opt` did not say whether it meant the host or the container, and was rightly
  declined.
- **A sub-agent ran Docker without asking** (2026-10-20); nothing recorded what it ran.
- **Allowing `docker` is not possible.** Both command lists match how a command starts,
  so `docker run` would also allow `docker run --privileged -v /home:/host …`, and
  Docker runs as root.
- **Docker touches the host's network.** Every container adds a `veth` to `docker0`
  and rewrites firewall rules. On 2026-10-21, ten containers in ten minutes made the
  desktop report the main wired connection as disconnected and connected again, ten
  times: when a container stops, its end of the `veth` pair shows up on the host for
  a few milliseconds, and a NetworkManager profile that was not tied to one card
  started on it. A test with `--network none` showed nothing at all.
- **Nobody counts flaky runs.** A red CI job is run again by hand, and only its last
  attempt is shown (#166).

## Decision

1. **Podman as the user, never Docker.** Agents run containers with rootless Podman:
   no daemon, no root, images in the user's home folder. Its network runs in user
   space (`pasta`): no bridge, no `veth` on the host, nothing for NetworkManager to
   see. Never `sudo podman` (rootful Podman makes a bridge, as Docker does). The same
   Dockerfiles work unchanged (`podman build -f …`).

   ```
   Docker          root daemon → docker0 bridge → veth on the host → NetworkManager reacts
   Podman, user    no daemon   → pasta, in user space              → host network untouched
   ```

2. **Every container run goes through one script, and only the script is allowed.**
   Agents run `make test-systemd` (the systemd check) or `make container-run
   RECIPE=<name>` (anything else). Both run without asking. With the script, typed
   `docker`, `podman` and `buildah` become blocked for agents in both command lists
   (Claude Code `deny`, Codex `forbidden`). Needing a flag the script does not allow (a
   device, `--privileged`) means a pull request that changes the script: limits change
   in review, in git.

   ```
   agent:   make container-run RECIPE=<name>          allowed
              └─ script: podman build / run / rm      fixed flags, the agent adds none
   agent:   podman run …   docker run …               blocked: refused, the owner is not asked
   agent:   sudo podman …  bash -c "podman …"         not blocked: the owner is asked → say no
   ```

   **The block stops only the literal command.** Both lists match the first words of a
   command (the same reason `docker run` cannot be allowed safely), so a command that
   starts with another word (`sudo`, `env`, `nice`, `bash -c`, `/usr/bin/podman`) is
   not blocked. It is not allowed either, so the owner is asked; a yes would run a
   container outside the script, with no record and none of point 3's rules. Such a
   request is answered no. The real guard is that the script alone writes Podman's
   arguments:

   - The script takes only the recipe name. `RECIPE` is a plain name (letters, digits,
     `-`) of an existing recipe folder; anything else is refused.
   - It ignores its environment and any further arguments, and writes every Podman
     argument itself. `make` passes command-line variables on to the script's
     environment (`make container-run RECIPE=x FLAGS=--privileged` sets `FLAGS`), so
     a test runs exactly that and checks that `FLAGS` never reaches the Podman command.
   - `commands.sh` runs **inside** the container, never on the host.

3. **The container sees only the repository.** The script mounts the checkout
   read-only, plus one writable folder inside it, and refuses any other host path, so
   `~/.ssh`, the rest of the home folder and `/run/user/…` (where the SSH agent's
   socket lives) do not exist inside.

   ```
   host <repo>/              → container /src     read-only
   host <repo>/.tmp/<run>/   → container /work    writable, one folder per run
   any other host path       → refused by the script
   ```

   `.tmp/` is ignored by git and, like every folder starting with a dot, skipped by Go's
   tools and `make check`. One folder per run keeps parallel runs apart; it is kept
   after a failed run, to look at, and deleted after a passed one. No
   environment variable from the session is passed in (`SSH_AUTH_SOCK` would let a
   container use the user's keys without seeing them). A test keeps both rules in the
   script. Anything the owner is asked about names both sides:
   `host <repo>/bin/apptrol → container /usr/bin/apptrol (read-only)`.

4. **Limits per container, fixed in the script.** Memory is the scarce resource; CPU
   can wait.

   ```
   memory       --memory=512m --memory-swap=512m   no swap: a container that grows is
                                                   stopped, the desktop is not pushed into swap
   processes    --pids-limit=256
   CPU          no cap; nice -n 10                 CPU can wait; nice is only a priority
                                                   (whether a cpu controller exists: point 9)
   network      --network none for the run         the build may download packages
   parallel     JOBS=16 by default                 before starting, the script keeps 4 GB of
                                                   memory free and starts fewer if needed
   ```

5. **An audit record of every run** in `.audit/`, split into the recipe and the runs.
   Eighty runs of one recipe store the recipe once and write eighty lines.

   ```
   .audit/
     recipes/<short hash>/Dockerfile, commands.sh   once per new recipe     committed
     runs.log                                       one line per run        committed
     logs/<run id>.log                              full output of a failed run   local only (.gitignore)
   ```

   A `runs.log` line, as the script writes it:

   ```text
   2026-10-21T18:49:17+02:00 run=0042 agent=claude-opus-5.5 branch=feat/x commit=a1b2c3d recipe=7f3e9a1 image=ubuntu jobs=16 result=PASS seconds=14
   ```

   Recipes and `runs.log` hold no paths, device names or log output, so they can be
   committed. Full logs can hold home paths or host names (privacy rule, AGENTS.md), so
   they stay on the machine. `make test-systemd`'s recipes are the files in
   `test/container/`. CI uploads its `.audit/` as an artifact; it cannot commit.

6. **The systemd check in CI.** A job `Systemd in containers` on `ubuntu-latest` runs
   `make test-systemd` for Ubuntu and Fedora (the same command as on the owner's
   machine) and compares `systemctl --user show -p StatusText apptrol` with the
   expected text: `<version> · controller not found`, then `waiting for a valid
   configuration` after a broken config, then `controller not found` again after the
   fix (SVC-13). A container has no controller, so its last line stays `controller not
   found`; `controller connected · N controls assigned` is the line in the hardware
   case TC-SVC-001, and it is not copied here. The job prepares rootless Podman on the runner
   itself (user namespaces, the runner user's subuid and subgid ranges) before it calls
   `make test-systemd`.

   It is **not** one of the required checks at first. It becomes one when `runs.log`
   shows **300 green runs in a row for each distro's recipe** on the owner's machine
   (the failure rate is then below 1 % with 95 % confidence; rule of three) **and 10
   green CI runs for each distro** from normal pull requests. The distros run side by
   side; each is counted on its own, because each has its own systemd and one can be
   flaky while another is not. A distro added later runs outside the required job until
   it has its own 300 and its own 10.

   ```
   Ubuntu 26.04   ✓✓✓✓✓✓ … 300 in a row   → counts toward required
   Fedora         ✓✓✓✗✓✓ …                → its count starts again; the red run gets an issue
   ```

   One unexplained red run resets that distro's count and gets an issue. Timeouts in
   the script are generous (60 s for systemd to start, 15 s for a Status line to
   change), so a slow container under load is not counted as a flaky test.

7. **Files.** `test/container/` holds one Dockerfile per distro and the one script,
   used locally and in CI. `docs/testing/` stays the manual cases; `packaging/` stays
   what is shipped.

8. **What containers cannot check** stays manual: the controller (no MIDI device), a
   second user in front of the computer (ADR 0029), sound (no PipeWire), and the short
   `stopping` state. TC-SVC-001 stays a hardware case. Later the same containers can
   take installing the `.deb` and `.rpm` with `apt` and `dnf`: part of the install half
   of #116 (which also has `zypper` and a build from source). Separately, `make
   test-launcher` could use their user systemd. The rest of #116 stays in VMs: `make test-audio`
   with PipeWire and with plain PulseAudio (the containers have no sound server), and
   the desktops (GNOME, on-screen display).

9. **To prove in the script's pull request.** The hand check in PR #167 ran in Docker
   with `--privileged`; nobody has run it in rootless Podman yet. The script's pull
   request shows it works, or brings the question back to the owner as a new ADR (see
   *Considered and left for later*). Known points to check:

   - systemd's login service may not start inside a container, so the test user's
     systemd (`systemctl --user`) may need to be started by the image itself.
   - With `--network none`, units that wait for a network (e.g.
     `systemd-networkd-wait-online`) are masked in the image, or every run waits out
     its timeout.
   - Each limit in point 4 is applied or the run fails; a limit is never dropped
     quietly. `--memory=512m --memory-swap=512m` was accepted on the owner's machine
     (2026-10-21, image `ubuntu:24.04`, command `true`); a CI runner may lack the
     `memory` controller.
   - The missing cpu controller is this machine's systemd setting, not a property of
     Podman; `nice` sets a priority, not a cap.

10. **Logging.** The script is a developer tool, not part of Apptrol: ADR 0016 does not
    apply. Its record is `runs.log` (point 5); it prints each step with the host path,
    the container path and the image, so a failed run can be read from its log alone.

### Considered and left for later

- **Allowing `docker` or `podman` with arguments in the command lists.** Not possible
  safely (point 2).
- **Docker with `--privileged`.** Needed for systemd in a Docker container (the narrower
  form exited silently). It is close to root on the host and it puts a bridge on the
  host network. The script stays on rootless Podman. If that cannot run a check, the
  decision is revisited in a new ADR.
- **Docker Sandboxes**: the whole agent session in a microVM with its own Docker. Strong
  isolation, but it solves another problem (the agent, not the test), Docker lists only
  macOS and Windows as of 2026-10, and `make test-audio`, the controller and the test
  runs need the host. Worth a look later for agents doing risky work.

## Consequences

### Implementation clarification — 2026-10-23

The owner clarified two details while implementing #170:

- `JOBS=16` is a concurrency ceiling, not a repetition count. Each invocation runs
  each selected recipe once; available memory reduces concurrency while reserving
  4 GiB for the host.
- `AGENT` is the one allowed caller-supplied environment value, solely for audit
  attribution, for example `make test-systemd AGENT=gpt-6-astra`. It names the model
  or caller starting the run, not a model inside the container. The runner accepts
  only a 1–64 character label beginning with a letter or digit, followed by letters,
  digits, dots or hyphens. It removes the value before invoking Podman and never
  uses it to construct commands, flags or paths. An omitted label is `unspecified`;
  invalid labels fail. All other caller environment values remain ignored. This
  label is caller-declared attribution, not authenticated identity.

### Review clarification — 2026-10-23

- Recipe folders use names (`.audit/recipes/<name>/`), as specified by #170.
  Audit lines use `recipe=<name>` plus a separate `hash=` field. Early lines without
  hashes are retained as history, not evidence for the consecutive-pass gate.
- Worktrees share a host-only lock in Git's common directory before calculating
  available memory. Separate clones and unrelated programs are outside this lock.
- Make pins `SHELL` and `.SHELLFLAGS`; the systemd target builds through a fixed
  helper after clearing the caller environment, rather than Make's overridable
  `build` recipe. `FLAGS` remains ignored. Using `-f`, `MAKEFILES`, `--eval`,
  Make-language expressions in variable assignments or another target to bypass
  the runner remains forbidden by policy. Command-prefix rules cannot enforce
  all Make arguments; broader argument-aware approval is follow-up work.
- Audit lines capture the invocation's source commit and `dirty=yes|no`, including
  untracked non-ignored files but excluding the generated `.audit/runs.log` itself.
  Acceptance evidence must identify committed source with `dirty=no`.
- [ADR 0037](0037-builds-in-a-limited-user-scope.md) records the build
  limit failure and the investigated user-scope mechanism. It does not relax any
  resource limit.
- Git lookups for the audit line run with `core.fsmonitor` and `core.hooksPath`
  overridden and without global or system config, and the runner refuses a run when
  Git's top level is not the checkout. Parsing the Makefile no longer runs Git
  (`LDFLAGS` expands only in `build`). This closes the automatic paths, not every
  one: a `.git/config` filter driver can still run a program during `git status`.
  Whoever can write `.git/config` can already run code through `git commit` hooks
  and `make check`, so the checkout's Git data is not a security boundary.

### CI clarification — 2026-10-25

Issue #171 runs `make test-systemd AGENT=ci` on each pull request, using the
unchanged runner for Ubuntu 26.04 and Fedora 44. `ci` identifies the caller in
audit records, not a model, and is removed before Podman runs. The job uploads
`.audit/`, including hidden files and failed-run logs, for 14 days even on failure;
it cannot commit evidence. It remains outside the required checks until #172's
per-distro evidence gate is met. Package installation tests remain #172 as well.

On the disposable GitHub-hosted VM only, privileged setup installs Podman and its
rootless dependencies, preserves usable subordinate UID/GID ranges or allocates
non-overlapping ranges, and enables unprivileged user namespaces. Where present,
`kernel.apparmor_restrict_unprivileged_userns=0` permits those namespaces for this
boot; AppArmor itself remains enabled. This VM-wide setting trades the namespace
restriction for simple, reproducible CI preparation. A dedicated AppArmor profile
was considered but would add policy maintenance for the build/runtime helpers.
`kernel.unprivileged_userns_clone=1` is set where available, and a zero
`user.max_user_namespaces` is raised to 28633. These are runner settings, never
changes to the owner's host or relaxations of the script's limits and mounts.
Setup and read-only failure diagnostics use `sudo`; every engine invocation remains
in the repository script as the ordinary runner user. Checkout credentials are not
persisted.

The selected Go toolchain is exposed at `/usr/local/go` for the runner's fixed,
cleared environment. User-manager delegation is described in
[ADR 0037](0037-builds-in-a-limited-user-scope.md#ci-user-manager--2026-10-25).

### CI policy clarification — 2026-10-26

The first CI runs passed Ubuntu and both distros' build and runtime limit proofs,
but Fedora's ordinary user manager failed during PAM account lookup. The host
kernel reported `profile="unix-chkpwd"` denying `capname="dac_override"` for the
container's PAM helper. This matches an
[upstream AppArmor profile report](https://github.com/roddhjav/apparmor.d/issues/958).

The owner approved adding `capability dac_override,` to the disposable runner's
`/etc/apparmor.d/local/unix-chkpwd` and reloading only the `unix-chkpwd` profile.
Setup first checks that the packaged profile includes that local file and then
verifies the reloaded profile remains enforcing. An incompatible profile layout
fails setup rather than silently skipping the rule.

This permits the helper to use a capability it already holds in its user
namespace to read container account data. It applies to all processes using that
profile on this VM for the rest of the job, including host invocations, not only
the Fedora container. The profile's other restrictions and AppArmor enforcement
remain active. The owner's host is untouched; the container script gains no
flags, privileges or mounts, and all resource proofs still run. Disabling the
profile or changing Fedora's account-file permissions was not selected.

Audit artifact names include the workflow attempt number, so reruns do not reuse
an earlier upload's name; the upload does not request overwriting old evidence.
GitHub controls artifact retention and accessibility across reruns, so naming
alone is not a preservation guarantee. On pull requests, the audit's commit is
the tested merge commit and `branch=` is empty for the detached checkout; the
associated workflow run identifies the PR and its head. The #172 evidence gate
must use that run association rather than interpreting the merge SHA as a PR head.

### Package installs — 2026-10-26

Point 8 planned installing the `.deb` and `.rpm` in the same containers; `make
test-install` (QA-17, #172) does it. This note records what it decides, and what it
leaves as it was.

```text
make snapshot (you)  →  dist/*.deb, dist/*.rpm        read through the read-only checkout
make test-install    →  Ubuntu: apt-get install      Fedora: dnf install
                        then the Status checks, run on the installed /usr/bin/apptrol
```

- **No change to any guarantee.** The runner adds a third mode next to the Status check
  and the recipes. The Podman arguments, mounts (checkout read-only, `.tmp/<run>/`
  writable), limits, host-side proofs, `--network none` and the audit record are the
  same code path. It adds no flag, mount or environment value. The image is the Status
  check's own, so nothing new is built.
- **The runner never builds packages.** GoReleaser downloads and runs code of its own;
  run by the script it would run unsandboxed on the host, and in a container it would
  need a writable `dist/` and the network, which are loosened guarantees. The owner or
  CI runs `make snapshot`; the script only reads `dist/` and refuses to start without
  the packages. Building the packages in a container would be a new ADR.
- **Root in the container is acceptable.** `apt` and `dnf` run the package's maintainer
  script as root, but root in a rootless container is the user's own unprivileged ID on
  the host. The network is off, the checkout is read-only and the limits are proven from
  the host before the image starts. The script is the repository's own
  `packaging/scripts/postinstall.sh`.
- **A stale package fails.** `pick-package.sh` picks the one package for the
  container's architecture whose file name has `snapshot.<hash>`, with a hash of seven
  or more digits that starts the checked-out commit. The host passes the full commit
  as data, validated as hexadecimal; it does not compare two abbreviations, because
  Git's short length depends on the checkout's `core.abbrev` and GoReleaser's on its
  own rule (found in the first review round). A snapshot left in `dist/` by an earlier
  commit cannot pass for the current one.
- **No network means no dependency resolution.** Distribution repositories are not
  reachable, so required dependencies are not resolved and Recommends are not installed.
  The packages declare no required dependencies. If one is added the check fails, and
  the install belongs in #116's VM tests, which also keep upgrade, removal and `zypper`.
- **Own evidence.** The audit names are `install-ubuntu` and `install-fedora`, apart
  from `systemd-*`. Point 6 says a check added later runs outside the required job
  until it has its own 300 local and 10 CI green runs per distro; this one does, and the
  Status check's evidence is not reset by it.

### CI summary clarification — 2026-10-27

The Systemd in containers job writes one result/time table per workflow attempt
(QA-16, #196), using a small shell helper outside `test/container/`. Keeping the
parser outside the container scripts preserves their evidence hashes. Tests run
the helper with synthetic audit lines, without containers.

```text
committed audit → capture line count → current run appends → summarize suffix
       └──────────────── full audit retained in artifact ────────────────┘
```

Capturing the initial line count immediately after checkout excludes historical
records even when they name the same commit as a rerun. Filtering only by commit
would not do that; emptying the log would discard history from the artifact.
The helper also checks the tested commit (accepting the audit's hexadecimal
abbreviation) and exact `systemd-ubuntu`/`systemd-fedora` recipe names. The heading
shows the first seven digits of the tested checkout commit, which is the merge
commit for a PR, not its head.

Rows show `✅ pass`, `❌ fail` or `no result`; missing time is `—`. Missing or
invalid results and duplicate distro records cannot claim a pass. The summary
step runs with `if: always()` immediately after the test, before diagnostics and
upload. Setup/build failures can leave no records, and cancellation can leave
partial records. The overall job outcome is not a substitute for a distro audit
record. Runner loss, force cancellation and job timeouts can prevent even an
`always()` step from completing. Test failures remain job failures.

The artifact still includes the complete audit history. No streak is computed,
no required-check gate changes, and package-install summaries remain a follow-up
once their CI job exists. The container script, flags, mounts, limits and audit
format are unchanged.

### Clean-up of kept failed runs — 2026-10-27

Point 3 keeps a failed run's folder, and point 5 its log, to look at. Nothing removed
them later (#195). `make test-clean` (QA-18) does, through the same script. It changes
no earlier decision and adds no guarantee-widening flag, mount or environment value.

```text
make test-clean AGENT=<model>
  └─ script, @clean mode: same checks and lock as a run, then
       .tmp/run-<8>/              → podman unshare rm -rf --one-file-system
       .audit/logs/run-<8>.log    → rm -f
       .audit/runs.log            → kept; one line added (recipe=@clean)
```

- **No container.** Only `podman info` (rootless check) and `podman unshare rm` run, the
  call the script already makes after a passed run. `unshare` lets the host process act as
  the subordinate user IDs a recipe may have written files as; it mounts nothing, so the
  mount inventory of point 3 is unchanged.
- **Scope.** Exact names the script creates (`run-` plus 8 letters or digits, `.log` for
  logs), directly inside this checkout's `.tmp/` and `.audit/logs/`; each removal helper
  builds its path from a name it checks again. Other names are left alone and printed.
  Links are never followed: `.tmp/`, `.audit/` and `.audit/logs/` as links are refused as
  for every run (before anything is written, so such a refusal is not recorded), and a
  link named like a run is removed as a link only. The lock can be waited for as long as
  a run lasts, so after it, and again before every removal, the folders are checked to
  be real (no link on their path); the audit line is appended only through a real
  `.audit/runs.log`.
- **Mounts.** `rm --one-file-system` does not stop at a bind mount of the same file
  system (found in review round 1), so a folder with a mount at or below it, read from
  the host's own `/proc/self/mountinfo`, is not removed; being unable to read it also
  refuses. The other folders are still removed, and the run ends with status 1.
- **All kept runs, no age limit** (owner's decision, 2026-10-27): the message of a failed
  run says it is kept for you; you run the clean-up when you are done looking. A run in
  progress is safe because the script's shared lock is held for a whole run and the
  clean-up waits for it.
- **One line in the ledger, like a run** (owner's decision): `recipe=@clean`,
  `image=none`, the `AGENT=` label, `result=PASS` or `FAIL`. `@` cannot occur in a recipe
  name, so the line never mixes with a recipe's evidence, and the per-distro counts of
  point 6 are unchanged. Names and counts of removed items are printed, not stored.
  One failed removal does not stop the others; the exit status is 1 and the line says
  `FAIL`.

### Repeated runs — 2026-10-28

Point 6 needs 300 green runs in a row per distro, and #172 and point 4 counted on doing
them 16 at once, in about 10 minutes. What PR #182 built runs each distro once per
`make test-systemd` call, and the shared lock is held for the whole call, so calls
cannot overlap: 300 runs took about 300 calls one after another, roughly 1.5–2 hours,
with 14 of the 16 slots unused. The owner decided (2026-10-28) that one call must do
the 300. `make test-systemd RUNS=<n>` (QA-16, #172) does.

```text
before   call 1 ─ lock ─ ubuntu ┐            call 2 ─ lock ─ ubuntu ┐       … × 300
                         fedora ┘ unlock                     fedora ┘ unlock

RUNS=300 call ─ lock ─ build binary once
                       ubuntu #1, fedora #1     each builds and proves its image
                       then up to <jobs> at once: ubuntu #2 … #300, fedora #2 … #300
                       summary ─ unlock
```

- **Still one lock.** The lock is taken once for the whole call, so another worktree's
  run or `make test-clean` still waits until every run of the call has finished. The
  slots come from the same memory reading (4 GiB kept free), now at most 8 (below).
- **Every run is still a run.** Its own container, `.tmp/<run>/` folder, run id and
  `runs.log` line, in the same format; the same mounts, limits, host-side proofs and
  `--network none`. A failed run keeps its folder and log.
- **Built once per call.** The binary is built once. Each distro's first run builds and
  proves its image as before; the later runs of that call build nothing and reuse it
  (`--pull=never` refuses a missing image). ADR 0037 holds as written: the uncached
  probe runs in every call and before every build; a reused run builds nothing, so it
  has nothing to probe. Each run's runtime limits are still proven from the host.
- **Stop at the first red run.** One red run resets the distro's count (point 6), so
  continuing would prove nothing: after the first failure no new run starts, the running
  ones finish, the call exits 1 and names the failed run id. A summary gives passed,
  failed and not started per distro, and the wall time.
- **RUNS is data, like AGENT.** This amends the 2026-10-23 clarification: `AGENT` is no
  longer the only caller value. `run.sh` passes `RUNS` through `env -i` as data; the
  runner accepts only a plain decimal 1–1000 (no sign, space or leading zero), checks
  it before the lock, removes it from the environment, and never puts it in Podman's
  arguments, a path or shell source. A refused value writes no audit line: no run was
  attempted. `RUNS=1`, the default, is the old path unchanged; `make test-install`,
  `make test-clean` and `make container-run` ignore it. The Makefile freezes `RUNS` as
  literal data (`$(value RUNS)`), so a Make expression in it never runs (review round 1).
- **At most 8 at once, not 16.** This amends point 4's ceiling of 16. The first real
  `RUNS=16` call, 16 systemd containers at once, had 3 of 18 runs fail; systemd in the
  containers logged:

  ```text
  systemd[<pid>]: Failed to create control group inotify object: Too many open files
  systemd[<pid>]: Failed to allocate manager object: Too many open files
  ```

  Inotify instances are a per-user budget (`fs.inotify.max_user_instances`), shared by
  every rootless container and the desktop session; 16 at once exceeded it on the
  owner's machine. Memory alone does not tell how many fit. The cause is known; it is
  not an Apptrol or test flaw, and the 18 lines stay in `runs.log`.

  ```text
  the user's inotify instances (fs.inotify.max_user_instances)
    ├─ the desktop session
    └─ every rootless container's systemd and user manager
         16 at once → budget exceeded, runs fail
  ```

  At most 8 at once, for all container runs, is the owner's choice (2026-10-28). The
  memory reading still applies and can only lower it.
- **Left for later.** Adapting the ceiling to the free inotify instances was
  considered and left for later.

### Effects

- Agents run containers without prompts, and the owner can see afterwards what ran
  (recipe), when, by which agent, on which commit, and whether it passed.
- With the script's pull request, a typed `docker`, `podman` or `buildah` is refused
  for agents, even after an OK in the chat. A wrapped one (`sudo podman …`) is asked
  about and answered no (point 2). The owner can still use all three. A new need is a change to the
  script.
- Containers no longer touch the host's network setup or the desktop's network
  notifications.
- The flakiness of the new CI job is measured, not guessed. The same `runs.log` could
  later count the reruns of the Test job (#166).
- Podman must be installed on the developer's machine (`apt install podman`; it does
  not touch an installed Docker). Rootless Podman needs the user's subuid and subgid
  ranges, which Ubuntu and Fedora create for new desktop users; a CI runner's user
  gets them from the job (point 6).
- Delivered in small pull requests: this decision and the AGENTS.md rule; then
  `test/container/`, the script, `make test-systemd` and `make container-run`, the
  command lists and their test (QA-15, QA-16); then the CI job; later, making it
  required and adding package installs.
