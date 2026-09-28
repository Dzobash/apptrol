# 0001. Record architecture decisions

- **Status:** Accepted
- **Date:** 2026-09-28

## Context

Apptrol is built in phases over a long period, largely with AI assistance. Without a
record, the reasons behind choices get lost and are argued again later.

## Decision

Significant decisions are recorded as short ADRs in `docs/adr/`, numbered in order.
Requirements live separately in `docs/requirements.md` with stable IDs.

## Consequences

Every non-obvious choice needs a few lines of writing. In return, anyone (including a
future AI session) can see why the code is the way it is.
