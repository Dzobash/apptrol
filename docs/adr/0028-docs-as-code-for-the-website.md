# 0028. Docs as code for the website; the roadmap as its single source

- **Status:** Accepted
- **Date:** 2026-10-07

## Context

Apptrol will get a public website, built with Hugo in a separate public repository and
hosted on Kubernetes. It should show the documentation from this repository without
copying it by hand, and a roadmap page in the style of
[crosspointreader.com/roadmap](https://crosspointreader.com/roadmap): a phase stepper at
the top, one card per phase with status, goal, items and "done when", then *Out of
roadmap*, *How this roadmap changes* and *Scope*.

Today `docs/` is written for GitHub: Markdown with relative links, a few HTML image tags,
a roadmap with checklists but a goal and a done-when line for only some phases, and no
overview that groups the pages.

The options were:

- **Docs as code** (chosen): `docs/` stays the only source; the website pulls it at build
  time and renders it.
- **Copy the docs into the site repository**: two copies that drift apart.
- **A separate roadmap data file** (YAML) for the site, next to `docs/roadmap.md`: easier
  to render as cards, but a second truth for the roadmap.

For grouping the pages:

- **A map page, `docs/README.md`, grouped by [Diátaxis](https://diataxis.fr/)** (chosen):
  no file moves, no broken links.
- **Folders per type** (`docs/how-to/`, `docs/reference/` …): clearer on disk, but every
  link and bookmark changes.

## Decision

1. **`docs/` is the website's source.** The website repository pulls it at build time;
   this repository contains no site tooling.
2. **Plain Markdown, readable on GitHub and renderable by Hugo:**
   - HTML only for images and line breaks (`<p align="center">`, `<img>`, `<br>`).
     Hugo drops raw HTML unless the site sets
     `markup.goldmark.renderer.unsafe = true`. That setting is "unsafe" only for text
     from untrusted writers; `docs/` changes only through reviewed pull requests, so
     the site sets it. Never `<script>`, `<iframe>`, `<style>` or `on…=` attributes.
   - Links inside `docs/` are relative. Links to files outside `docs/` (LICENSE,
     CHANGELOG, the example configuration, CI files) are full GitHub URLs, as those files
     are not on the website; the site needs no link rewriting.
   - Images live in `docs/assets/` and are served by the website itself, not from
     GitHub.
3. **`docs/roadmap.md` is the single roadmap source**, in the crosspoint structure:
   - an overview table (phase, name, topic, version, status), then one section per phase
     with its name as heading, its topic, version and status as subtitle, a one-line
     **Goal**, its checklist and a **Done when** line;
   - then *Soon™* (the backlog), **Out of roadmap** (dropped ideas, each with a reason and
     a link), **How this roadmap changes** and **Scope** (vision, in scope, out of
     scope).
4. **Every phase has a pop-culture name**, its topic kept as subtitle:
   0 *It's Dangerous to Go Alone! Take This.*, 1 *Turn Down for What*, 1.5 *You Get a
   Button! And You Get a Button!*, 1.6 *Show Me What You Got*, 2 *Multiverse of
   Madness*, 3 *I'm In*, backlog *Soon™*. A new phase gets one too.
5. **`docs/README.md` groups every page** as tutorial, how-to guide, reference,
   explanation or project (the roadmap fits none of the four Diátaxis types). The
   website uses it for its navigation. A page that serves two types goes where most
   readers come for it, e.g. the logging guide under reference for its attribute list.
6. The rules are in [CONTRIBUTING.md › Documentation](https://github.com/Dzobash/apptrol/blob/main/CONTRIBUTING.md#documentation);
   CLAUDE.md points to them.

## Consequences

- The website's requirements live in its own repository; it needs the Goldmark
  `unsafe` setting and reads `docs/README.md` and `docs/roadmap.md`. Hugo expects
  `_index.md` for section pages, so the site maps `README.md` files (here and in `adr/`)
  to it.
- Rendering roadmap cards from Markdown means parsing headings and the bold *Goal* and
  *Done when* lines. If that proves fragile once the site is built, a roadmap data file
  (YAML) may be needed; that is a new decision with its own ADR, and must keep one
  source of truth.
- Links to files outside `docs/` point to `main`, not to the version being read.
- There is no tutorial yet; the first planned one is "From install to your first
  slider".
- The phase plan of ADR 0011 is unchanged; it gets a dated note about the names.
