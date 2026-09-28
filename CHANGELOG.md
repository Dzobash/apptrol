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
