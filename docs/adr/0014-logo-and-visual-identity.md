# 0014. Logo and visual identity

- **Status:** Accepted
- **Date:** 2026-09-29

## Context

Apptrol needs a logo for the README and GitHub, and later an application icon and tray icon
(Phase 3). It must not borrow from KORG's branding (ADR 0010), must work from 16 px to
large sizes, on light and dark backgrounds, and in one colour.

Several directions were explored on a design canvas: a fader-shaped "A", three faders, a
knob, a fader-shaped "t", and combinations of knob and faders in three palettes.

## Decision

- **Mark:** a knob and two faders on a rounded tile — one simplified controller channel
  ("E3" in the exploration, derived from the author's own sketch).
- **Palette** in the spirit of Sanzō Wada's colour combinations: Indigo `#233A5E`,
  Cream `#EFE4CE`, Vermilion `#D4573B` (single accent), with Ink `#2A2A2E`, Paper
  `#FBF7EF` and Sand `#F1E7D3` for text and backgrounds.
- **Wordmark:** "apptrol" in Space Grotesk SemiBold, tracking −2 %, outlined in the SVG.
- **Small sizes:** a separate pixel-aligned 16 px drawing.
- Source files are SVG in `docs/assets/brand/`; PNGs are rendered from them.
  Usage rules are in `docs/brand.md`.

## Consequences

- The Phase 3 GUI and tray icon reuse these colours and files; Indigo, Cream and Ink carry
  text, Vermilion is an accent only (it fails text contrast on Indigo).
- Changing the mark means updating the SVGs, re-rendering the PNGs and the social preview,
  and updating `docs/brand.md`.
