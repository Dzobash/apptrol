# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Project documentation: requirements, roadmap, configuration reference, decision records.
- Example configuration and systemd user unit.
- Go project skeleton: `apptrol` command with `--version`, `--config` and placeholder
  `run`, `list` and `check` commands; Makefile for building and testing.
- Testing strategy (ADR 0012), QA requirements and manual hardware checklist (`docs/testing.md`).
- CI workflow: lint, tests with race detector and coverage gate on two Go versions,
  vulnerability scan, build and smoke test.
- Release pipeline: GoReleaser builds binaries, .deb/.rpm packages and checksums for
  amd64 and arm64 on every version tag (ADR 0013, `docs/releasing.md`).
- Dependabot for Go modules and GitHub Actions; issue and pull request templates;
  security policy.
- Branch and pull request workflow in the contributing guide.
- Logo and brand guide (`docs/brand.md`, ADR 0014): SVG mark, wordmark and lockups,
  PNG sizes from 16 to 512 px, GitHub social preview.
- Architecture for Phase 1 (`docs/architecture.md`, ADR 0015); checklist for making the
  repository public in the release guide.
- Configuration loading and validation (`internal/config`): every problem in a file is
  reported at once with its setting name, unknown settings are rejected, and friendly
  messages explain wrong value types.
- `apptrol check` validates the configuration and shows which app is on which control.
- CI runs every fuzz test for 20 seconds on each push.
- README: prerequisites, controller settings (with SysEx Controls for Linux), first
  steps and a disclaimer.
- Logging (`internal/logging`): journald output with priorities (coloured text with
  timestamps when started from a terminal), log file with size-based rotation, separate
  formats per output (text, JSON, logfmt), level and outputs changeable at runtime; falls
  back to the journal if the log file cannot be opened.
