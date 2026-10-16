# Apptrol — notes for Claude

The rules for every coding agent are in [AGENTS.md](AGENTS.md); Claude Code reads them
through this import:

@AGENTS.md

## Only for Claude Code

- **Manual test runs:** `/test-run vX.Y.Z` ([.claude/commands/test-run.md](.claude/commands/test-run.md))
  walks through the cases one at a time, as the
  [how-to](docs/testing/how-to-run-release-tests.md) describes.
- **Sub-agents** work in worktrees, one task and one branch each, as AGENTS.md › Several
  agents at once says. Claude Code's own worktree tools still put them under
  `.claude/worktrees/`, which git ignores too.
- **Learning output style:** where it asks for `TODO(human)`, follow AGENTS.md instead:
  the owner decides in the chat, Claude writes the code.
