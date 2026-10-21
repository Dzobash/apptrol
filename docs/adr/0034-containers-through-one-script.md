# 0034. Containers: rootless Podman through one script, with an audit record of every run

- **Status:** Accepted
- **Date:** 2026-10-21

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
