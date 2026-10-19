# Package test cases

### TC-NFR-001 — The Debian package includes machine-readable copyright and license texts

Covers: NFR-06, NFR-08, NFR-09 · Phase 1.6
Config: none (package check)
Type: Manual

**Setup**
- [ ] The candidate deb for this machine's architecture is downloaded and installed
- [ ] `lintian` is available; otherwise mark its step blocked
- [ ] KDE Discover is available for the final observation; otherwise note it unavailable

**Steps**
- [ ] Run `lintian <candidate.deb>` → no copyright-file diagnostics; record any other diagnostics separately
- [ ] Read `/usr/share/doc/apptrol/copyright` → the DEP-5 Format URL, Apptrol name, source URL, `Files: *`, copyright holder and years from `LICENSE`, and full MIT text are present (NFR-06, NFR-09)
- [ ] Read `/usr/share/doc/apptrol/THIRD_PARTY_LICENSES` → bundled libraries and Go have copyright notices and license texts (NFR-08)
- [ ] Open the same deb from a folder in Discover → record the license it displays, including “Unknown” if shown; this observation does not determine pass or fail because Discover's interpretation is not guaranteed (#97)

**Result:** Pass / Fail / Blocked
**Notes:**
