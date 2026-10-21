# Package test cases

### TC-NFR-001 — The Debian package includes descriptive text, copyright and licenses

Covers: NFR-06, NFR-08, NFR-09, NFR-10 · Phase 1.6
Config: none (package check)
Type: Manual

**Setup**
- [ ] The candidate deb for this machine's architecture is downloaded and installed
- [ ] `lintian` is available; otherwise mark its step blocked
- [ ] KDE Discover is available for the final observation; otherwise note it unavailable

**Steps**
- [ ] Run `lintian --info --display-info <candidate.deb>` → no `I:` tags and no tag about the copyright file, e.g. `no-copyright-file`, tags ending in `-dep5-copyright`, or tags starting with `copyright-`; record other tags separately (the `W:` warnings are tracked in #153)
- [ ] Read `/usr/share/doc/apptrol/copyright` → the DEP-5 Format URL, Apptrol name, source URL, `Files: *`, copyright holder and years from `LICENSE`, and full MIT text are present (NFR-06, NFR-09)
- [ ] Read `/usr/share/doc/apptrol/THIRD_PARTY_LICENSES` → bundled libraries and Go have copyright notices and license texts (NFR-08)
- [ ] Open the same deb from a folder in Discover → record the license it displays, including “Unknown” if shown; this observation does not determine pass or fail because Discover's interpretation is not guaranteed (#97)
- [ ] On that Discover page, read the summary and description → they explain per-app volume, the Korg nanoKONTROL2, mute, solo, media buttons and the background service. If package metadata is shown, the synopsis is `Per-app volume mixer for the Korg nanoKONTROL2` and the description also explains that PipeWire with pipewire-pulse is needed (NFR-10, #152)

Record the displayed text and which source it comes from: package metadata or
AppStream. Discover may show the AppStream summary ("…with a hardware MIDI
controller", from `metainfo.xml`) rather than the package synopsis.

**Result:** Pass / Fail / Blocked
**Notes:**
