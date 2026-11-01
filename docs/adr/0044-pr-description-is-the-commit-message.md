# 0044. The PR description is the commit message, with trailers for every model

- **Status:** Accepted
- **Date:** 2026-11-01

Decided by the owner on 2026-10-31.

## Context

Pull requests are squash-merged. The squash commit message used the PR title only
(`squash_merge_commit_message=BLANK`), so a commit on `main` was the title plus the
`Co-authored-by` lines GitHub collects from the branch's commits. GitHub never collects
`Reviewed-by`, so the model that reviewed a PR was missing from history: in #217,
GPT-6.1-Sol found two issues and got no credit.

The owner may switch providers later (e.g. Kimi), so the rule has to hold for any
provider, not only the two in use now.

## Decision

1. **The squash commit message is the PR title plus the description**
   (`squash_merge_commit_message=PR_BODY`). The lead switches the setting right after
   the PR that brings this ADR merges.
2. **The description ends with a trailer block**, as its last paragraph (git reads
   trailers only there): one `Co-authored-by` per model that wrote commits on the
   branch, one `Reviewed-by` per model whose round was clean on the final commit. A new
   provider needs only its model name and its noreply address:
   ```
   docs: … (#n)                                     ← PR title

   What changes and why, in a few lines.            ← description
   Fixes #n

   Co-authored-by: Claude Opus 5.5 <noreply@anthropic.com>
   Reviewed-by: GPT-6.1-Sol <noreply@openai.com>
   ```
3. **The agent that owns the PR fills in the trailers at Ready** (AGENTS.md › Ready),
   with REST PATCH, and reads them back.
4. **The template is short, the checklist is the first comment.** The description is
   history now, so the checklist moves to `.github/pull_request_checklist.md` and is
   posted, filled in, as the PR's first comment. A collapsed `<details>` block would not
   help: git would keep its raw tags.

### Options considered

- **(a) The description is the message, with trailers** — chosen. Nothing to do at
  merge time; any provider fits.
- **(b) The lead writes the message by hand when merging.** The owner merges, so this
  depends on a human remembering it every time.
- **(c) A `Reviewed-by` line in each fix commit.** Misses clean rounds that make no
  commit, and GitHub drops the line from the squash anyway.

## Consequences

- `main`'s history credits who wrote and who reviewed each change, for any provider.
- The description has to be short and correct: it is no longer a working note.
- On the first merge after the switch, the owner checks whether the template's HTML
  comments show up in `main`'s history. If they do, the template drops its comments.
- Any other line, such as Claude's "🤖 Generated with …" footer, goes above the
  trailers; after them, git would not read the block as trailers.
- If the owner does not like the result, a later ADR replaces this one with another
  way.
