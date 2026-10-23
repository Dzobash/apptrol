# 0037. Build images inside a limited, delegated user scope

- **Status:** Proposed
- **Date:** 2026-10-23
- **Supersedes:** The build-limit mechanism in [0034](0034-containers-through-one-script.md), point 4; its limits and other restrictions remain.

## Context

Review of #170 found that runtime checks did not prove limits for image builds.
On the owner's machine, an uncached build probe confirmed memory and zero-swap
limits but failed the required process limit. Setting `pids_limit = 256` in the
runner's pinned `containers.conf` did not enforce it during `podman build`.
No host values are retained here; failed attempts remain in the audit log.

The owner approved investigating a limited user cgroup and drafting this proposal.
A first experiment set the process limit on the build's leaf cgroup. It passed
one `RUN` step but failed the next: the runtime recreates the leaf between steps.
Therefore a persistent ancestor must enforce the process budget.

## Decision

Proposed for QA-16: the repository runner starts each image build with
`systemd-run --user --scope`, with `TasksMax=256`, `MemoryMax=512M`,
`MemorySwapMax=0` and `Delegate=yes`. These limits cover the build engine and its
container together. All engine invocations still originate in the repository
script; this is not permission to wrap a typed Podman command.

```text
limited user scope: 512 MiB, no swap, 256 tasks total
  ├─ supervisor: build worker and rootless Podman
  └─ build:      one RUN at a time; recreated between steps
```

The worker checks the scope's kernel limits, moves itself to a supervisor leaf,
and enables the delegated memory and pids controllers. For builds only, it selects
Podman's `cgroupfs` manager and fixes `--cgroup-parent` to the build leaf inside
that scope. The scope's limits apply even when the runtime recreates the leaf.
Podman builds retain memory/swap flags, one build job and `nice -n 10`.
Runtime containers retain ADR 0034's existing mechanism.

Before any recipe build, an uncached trusted probe with a digest-pinned Ubuntu
base image runs two separate `RUN` steps.
Each checks its exact cgroup ancestry, the scope's three kernel limits and its
nice priority. Only the probe uses the host cgroup namespace to inspect its
ancestor's virtual cgroup files; no additional host-directory mount is added.
Recipe builds explicitly use a private cgroup namespace. The probe's image is
removed after success, together with its image-ID file. A failed scope setup,
probe or build fails the audited run.
Changed build enforcement invalidates cached recipe images.

Host-side builds use an explicitly empty registry-auth file and certificate
directory. Git global/system configuration and credential prompts are disabled;
all host Git transports are disabled as well, because Git's HTTP implementation
may otherwise read the owner's `.netrc`. Dockerfiles may use public archive URLs,
but not host-side Git or SSH sources. The temporary auth and certificate paths are
removed before mounting the run folder as `/work` (QA-16).

### Logging

This developer tool follows ADR 0034's audit scheme, not application logging.

| Event | Output | Retention |
|---|---|---|
| Scope or probe fails | Failing step and diagnostic; `result=FAIL` | Full local log; committed audit line |
| Probe passes | `PASS: build scope ancestry, memory, swap, pids and nice limits` per step | Console; full log only if the run later fails |
| Recipe completes | Existing runtime checks and audit result | One audit line per selected recipe |

## Consequences

- A working systemd user manager with delegated memory and pids controllers is now
  required even for ordinary recipe builds. Missing support fails; there is no
  rootful, unlimited or Docker fallback.
- The engine shares the 512 MiB/256-task budget with its build container, leaving
  less for package installation than a container-only budget. Real distro builds
  must pass before this proposal is accepted.
- Probe builds add work to each invocation, including when the recipe image is
  cached. They cannot be cached because host delegation can change.
- This proposal does not add the CI job (#171) or required-check/package work
  (#172). The CI job will need the same user-manager prerequisite.

## Validation — 2026-10-23

The revised two-step probe passed for both distro runs. Fresh, uncached Ubuntu
26.04 and Fedora 44 package-install builds completed under the aggregate budget;
both then passed the runtime limits, ordinary user-manager and three Status checks.
The sample recipe passed with the scoped probe. `make check` passed, including
regressions for failed scope creation, failed probes and missing kernel limits.
Those exploratory runs used uncommitted source. They establish feasibility, not
acceptance evidence for the final revision. Acceptance requires both distro runs
and the sample from a committed revision, recorded with `dirty=no`, plus another
security review.

## References

- [systemd scope execution](https://www.freedesktop.org/software/systemd/man/latest/systemd-run.html)
- [systemd resource controls](https://www.freedesktop.org/software/systemd/man/latest/systemd.resource-control.html)
- [Podman build cgroup parent and namespace](https://docs.podman.io/en/stable/markdown/podman-build.1.html)
