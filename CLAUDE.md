# Apptrol — notes for Claude

The rules for every coding agent are in [AGENTS.md](AGENTS.md); Claude Code reads them
through this import:

@AGENTS.md

## Only for Claude Code

- **Manual test runs:** `/test-run vX.Y.Z` ([.claude/commands/test-run.md](.claude/commands/test-run.md))
  walks through the cases one at a time, as the
  [how-to](docs/testing/how-to-run-release-tests.md) describes.
- **End of a session:** `/wrap-up` ([.claude/commands/wrap-up.md](.claude/commands/wrap-up.md))
  runs the shared checklist in `.agents/skills/wrap-up/SKILL.md`.
- **Sub-agents** work in worktrees, one task and one branch each, as AGENTS.md › Set up
  says (Claude Code's own worktree tools use `.claude/worktrees/`).
- **Learning output style:** where it asks for `TODO(human)`, follow AGENTS.md instead:
  the owner decides in the chat, Claude writes the code.
