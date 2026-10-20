# 0013. Releases and packaging with GoReleaser

- **Status:** Accepted
- **Date:** 2026-09-28

## Context

Users should be able to install Apptrol like any other program, without Go. The release
process should be repeatable and not depend on anyone's machine.

## Decision

- Releases are built by **GoReleaser** in GitHub Actions when a `v*` tag is pushed.
- Targets: Linux **amd64** and **arm64**; `.tar.gz` archives, **`.deb`** and **`.rpm`**
  packages (via GoReleaser's built-in nFPM), and a SHA-256 checksums file.
- Binaries are built with `CGO_ENABLED=0`: static, no C toolchain needed for cross-compiling.
- Packages install the systemd **user** unit but do not enable it; a post-install message
  explains the one command each user runs.
- Version, commit and date are injected at build time (`internal/version`).
- GitHub release notes are generated from Conventional Commit messages; `CHANGELOG.md`
  stays hand-written for users.

## Consequences

- One tag produces everything; `make snapshot` reproduces it locally without publishing.
- **Phase 1 constraint:** MIDI access should be pure Go (ALSA raw MIDI or sequencer via
  the kernel interfaces) to keep `CGO_ENABLED=0`. If a C library such as `libasound` turns
  out to be necessary, cross-compiling for arm64 needs a C cross-compiler and this record
  must be revisited.
- Package repositories (a PPA, AUR, COPR) are not covered; they can be added later.

## Notes

- 2026-10-20: `make check` and a step in CI's existing Build job cross-compile
  `internal/mixer`, `internal/config`, `internal/state`, `internal/controller`,
  `internal/logattr` and `internal/version` for Windows with `CGO_ENABLED=0`
  (NFR-11, #93). This guards shared code against Linux dependencies without adding
  a CI job or a Windows runner. The config adapter keeps its inode check on Linux;
  elsewhere its watcher compares modification time and size. A replacement preserving
  both can be missed there. Windows adapters and path changes remain in #92.
- 2026-10-07: Every release file also gets a build provenance attestation from GitHub
  (`actions/attest-build-provenance`), keyless, checked with `gh attestation verify`. A
  checksum shows damage, not tampering: whoever could replace a package could replace
  `checksums.txt` too. A GPG signature was considered; it needs a key kept in the
  repository's secrets, so it guards against the same threat, and its gain is that users
  already have `gpg`. It is planned before 1.0 (#94). Pre-release packages are named with
  `-` instead of `~`, which GitHub does not allow in file names; the version inside keeps
  `~` (#74).
- 2026-10-07: The packages also install AppStream metadata
  (`/usr/share/metainfo/io.github.dzobash.apptrol.metainfo.xml`) and the app icon, so
  software centers such as KDE Discover show the name, developer, icon, links and
  releases instead of the file name and "unknown author". The id
  `io.github.dzobash.apptrol` follows the convention for projects on GitHub and is
  permanent: software centers remember the app by it; a later move would need a
  `<replaces>` entry, not a new id. The component is a `console-application` (no window).
  CI validates the file (`appstreamcli validate`); every release adds itself to
  `<releases>`.
- 2026-10-19: Debian packages install `packaging/debian/copyright` in DEP-5 format
  at `/usr/share/doc/apptrol/copyright` (NFR-09, #97). It carries Apptrol's copyright
  notice and full MIT text; bundled libraries and Go remain in `THIRD_PARTY_LICENSES`
  (NFR-08). RPM packages and archives keep `LICENSE`. Discover's license display for
  a local deb still needs observation; DEP-5 does not promise that Discover will show the license.
- 2026-10-19: CI's Packaging job runs on every pull request, without a path filter
  (the ruleset on `main` wants its checks on every pull request, and a skipped job
  counts as missing). It runs `goreleaser check`, a snapshot build and `lintian` on the
  .deb; errors fail it, warnings and informational tags are printed (NFR-05, NFR-08, NFR-09, #146). Two tags are
  suppressed because the package is built by GoReleaser, not by Debian's tools:
  `no-changelog` (we ship `CHANGELOG.md`, not `changelog.Debian.gz`) and
  `statically-linked-binary` (the static binary is intended, above). A test keeps the MIT
  text in `packaging/debian/copyright` equal to `LICENSE`.
