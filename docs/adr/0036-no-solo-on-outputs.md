# 0036. No solo on outputs

- **Status:** Accepted
- **Date:** 2026-10-23

Supersedes ADR 0027 point 7, and the solo part of point 9. Requirements: OUT-07,
OUT-08 (withdrawn), OUT-10, SOLO-03. Issue: #85.

## Context

ADR 0027 planned S on an output column to solo the output: every other output muted,
next to app solo and separate from it. Working out how it would behave showed that it
makes everything too complicated. Two solos, each muting a different kind of thing,
can together silence every app, with two S LEDs lit:

```text
app solo on Spotify      → VLC muted                  (app solo)
output solo on Speakers  → Headphones muted           (output solo)
Spotify plays on Headphones                           → nothing is heard
```

Output solo also does not move apps: soloing the headphones silences every app on the
speakers instead of moving it over.

## Decision

Outputs have no solo. S on an output column does nothing; a press is logged at debug.
App solo never touches outputs, as before. M is the only button that mutes an output.
Its LED is lit while the output is muted, with M or from outside Apptrol, as on an app
column; S and R stay off. A control whose device an earlier control owns shows no
mute (ADR 0035).

Nothing about solo is saved for outputs, and the state file does not change.

### Logging

| Level | Message | Attributes |
|---|---|---|
| debug | `button has no function on an output column` | `apptrol.button` |

It replaces `button has no function: not built yet on an output column` (ADR 0027
note of 2026-10-20). No new attribute.

## Consequences

- An output column looks like an empty one unless M is lit.
- Listening only on headphones means muting the other outputs with M, or choosing
  the output in the desktop's sound settings.
- OUT-08 is withdrawn; OUT-07 and OUT-10 describe the new behaviour.
- Listening on one output from the controller, by an output solo with a rule for app
  solo, or by moving apps between outputs, stays an idea for later
  ([#178](https://github.com/Dzobash/apptrol/issues/178), backlog); it would need a
  new ADR that supersedes this one.
