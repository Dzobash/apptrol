---
name: wrap-up
description: End-of-session check for the Apptrol repository. Use when the owner asks whether everything is done, saved or remembered before stopping for the day (e.g. "is everything done?", "wrap up", "/wrap-up", "$wrap-up"). Do not use in the middle of a task.
---

# Wrap-up: is everything done?

The owner asks this at the end of a session. Check every point below, then give one short
report: a line per point, `✓` when it is in order, `→` with what is open. Change nothing
that needs the owner's OK without asking first (AGENTS.md › Publish): pushing, opening
or merging a PR, deleting a branch that has commits not on GitHub, creating issues.

## 1. Other agents first

- Tasks delegated to another agent that are still running (in T3 Code: `task_status`,
  `t3_thread_list`), and the branches and worktrees they use. Name each one and what it
  is waiting for; don't stop it unasked. Nothing an active agent uses is cleaned up in
  point 2, even when its PR is merged.

## 2. Code

```bash
git status --short --branch                      # uncommitted changes, unpushed commits
git fetch --prune origin
git branch -vv                                   # local branches; "gone" = deleted on GitHub (not proof of a merge)
git worktree list                                # .worktrees/, .claude/worktrees/, ~/.t3/worktrees/apptrol/
```

- Uncommitted or unpushed work: say on which branch, and ask what should happen to it.
- A branch no active agent uses: delete it, and its worktree if it is clean, only when
  `gh pr list --state merged --head <branch>` shows its PR merged and its content is in
  `main` (squash merges leave the branch's own commits outside it).
- The main checkout should end on `main`, equal to `origin/main`.

## 3. GitHub

```bash
gh pr list                                       # open PRs: draft, CI, review, merge?
gh run list --branch main --limit 3              # CI on main
gh issue list --state all --search "created:$(date +%F)"   # issues opened today
git tag --sort=-creatordate | head -3            # releases and candidates
```

- An open PR: say what it waits for (the review, CI, the owner's merge).
- A tagged candidate without a passed run record in `docs/testing/runs/` on `origin/main`:
  the test run is still open.

## 4. Memory

- **Rules for every agent** belong in `AGENTS.md`, in the repository, never only in one
  agent's own notes: Claude's memory and Codex's memories are not shared in both
  directions. A new rule from today that is not there yet: suggest the PR.
- **Facts about the owner, their machine and how you work together** go in your own
  notes (Claude: its memory directory; Codex: its memories). Save what you learned today.
- Nothing private in the repository (AGENTS.md › Boundaries).

## 5. Handover

- The state of open work goes where every agent finds it (AGENTS.md › Start): its PR, its issue, or the handed-over task. If an open item has none
  of these, suggest one (ask before opening a PR or an issue).
- Your own notes only point there: what is released, which PR or issue is next. The
  next session still looks the state up first; the note only says where to look.

## The report

```text
✓ Agents    nothing running
✓ Code      main clean, equal to origin/main; no other branches or worktrees
✓ GitHub    no open PRs; CI on main green; v0.2.2 released
✓ Memory    notes updated; no new rule missing from AGENTS.md
→ Next      0.3.0: an ADR for #86 first
```
