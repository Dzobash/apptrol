# 0011. Deliver in phases; layouts and GUI later

- **Status:** Accepted
- **Date:** 2026-09-28

## Context

The full feature set (layouts, pick-up, GUI, media, recording, output switching, bleep) is
large. Building it at once would delay a usable tool.

## Decision

Deliver in phases (see `docs/roadmap.md`): Phase 1 is the core mixer with a single layout,
M and S. Media buttons and on-screen display follow in 1.5, layouts in 2, the GUI in 3.
Recording, output switching (R), bleep and Marker buttons stay in the backlog.

## Consequences

- A usable tool early; later phases are refined with real usage experience.
- The config format and code must leave room for later phases (see ADR 0004).
