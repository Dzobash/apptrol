# 0040. Where agent rules live

- **Status:** Accepted
- **Date:** 2026-10-27

Decided by the owner in [#200](https://github.com/Dzobash/apptrol/issues/200).

## Context

The rules for coding agents grew one incident at a time: a rule was added where the
problem showed up, in AGENTS.md, CONTRIBUTING.md, a skill, a command or a template. Some
rules ended up in two or three places, and the copies drifted apart (when to open an
issue, when to delete a branch). AGENTS.md itself was grouped by topic, so an agent had
to read all of it to find what applies to the step it is on.

Several agents work on the repository, from more than one provider. Each keeps private
notes that the others cannot see. A rule written only there holds for one agent and is
invisible to the rest.

#200 audited the rules as a whole. Two independent audits, by models of different
providers, each without the other's findings, were merged afterwards; a finding seen by
both was marked as such.

## Decision

Every rule has one home. Every other place links to it instead of repeating it.

```
AGENTS.md          how agents work, in the order of a task
CONTRIBUTING.md    what every contributor does: issues, commits, code, docs, logs
SECURITY.md        private reporting
docs/testing.md,   checks; the manual test run
  the how-to
docs/releasing.md  candidates, release, tags
ADRs               why; dated notes when a decision changes
CLAUDE.md, skills, entry points that link, no rules of their own
  commands
settings files     enforce the command list; tests keep them in step
templates          ask for evidence, link to the rules
agents' private    no rules
  notes
```

AGENTS.md follows the life of a task, with its boundaries (privacy, files, commands)
first, as they hold at every step:

```
start → plan → set up → build → review → publish → ready → clean up → hand over, end
```

A rule an agent learns in a session goes to its home in the repository, not only into
its own notes.

## Consequences

- An agent reads the section for the step it is on; boundaries hold at every step.
- A rule changes in one place. A copy found elsewhere is replaced by a link.
- Links to AGENTS.md name its sections (e.g. AGENTS.md › Publish); moving a rule means
  updating them.
- Where two copies said different things, the difference is decided on its own, in a
  later change, not while moving the text.

## 2026-11-03 — Local review before every push

The owner chose to finish all agent review rounds locally before pushing, including
routine docs and tests and later fixes. This lets review findings be addressed before
publishing each new commit. A clean verdict names the exact commit and carries into
the PR description, avoiding a duplicate review merely because it was pushed. GitHub
PRs and CI remain. The shared workflow lives in
[AGENTS.md › Review](https://github.com/Dzobash/apptrol/blob/main/AGENTS.md#review),
[Publish](https://github.com/Dzobash/apptrol/blob/main/AGENTS.md#publish) and
[Ready](https://github.com/Dzobash/apptrol/blob/main/AGENTS.md#ready).
