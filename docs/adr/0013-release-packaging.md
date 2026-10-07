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

- 2026-10-07: Every release file also gets a build provenance attestation from GitHub
  (`actions/attest-build-provenance`), keyless, checked with `gh attestation verify`. A
  checksum shows damage, not tampering: whoever could replace a package could replace
  `checksums.txt` too. A GPG signature was considered; it needs a key kept in the
  repository's secrets, so it guards against the same threat, and its gain is that users
  already have `gpg`. It is planned before 1.0 (#94). Pre-release packages are named with
  `-` instead of `~`, which GitHub does not allow in file names; the version inside keeps
  `~` (#74).
