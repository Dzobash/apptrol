# Apptrol — Releasing

Releases are built and published automatically by
[`.github/workflows/release.yml`](../.github/workflows/release.yml) with
[GoReleaser](https://goreleaser.com) (config: [`.goreleaser.yaml`](../.goreleaser.yaml)).
Pushing a version tag is all it takes.

## What a release contains

| File | For |
|---|---|
| `apptrol_<version>_amd64.deb`, `…_arm64.deb` | Debian, Ubuntu, Kubuntu, Mint, … |
| `apptrol-<version>-1.x86_64.rpm`, `…aarch64.rpm` | Fedora, openSUSE, … |
| `apptrol_<version>_linux_amd64.tar.gz`, `…arm64.tar.gz` | Any distribution (binary plus docs) |
| `checksums.txt` | SHA-256 of every file |

The packages install:

| File | Location |
|---|---|
| Program | `/usr/bin/apptrol` |
| systemd user service | `/usr/lib/systemd/user/apptrol.service` |
| Example configuration | `/usr/share/doc/apptrol/examples/config.toml` |
| README, changelog, license | `/usr/share/doc/apptrol/` (license under `/usr/share/licenses/apptrol/` on rpm) |

The service is **not** enabled automatically; each user runs
`systemctl --user enable --now apptrol` once.

## Versions

[Semantic Versioning](https://semver.org): `vMAJOR.MINOR.PATCH`.

- Before `1.0.0`, a new phase raises MINOR (`0.1.0`, `0.2.0`, …); fixes raise PATCH.
- A tag with a suffix, such as `v0.1.0-rc1`, is published as a **pre-release**.

## Making a release

1. Check that CI is green on `main` and, from Phase 1 on, that the
   [hardware checklist](testing.md#manual-hardware-checklist) passes.
2. In [CHANGELOG.md](../CHANGELOG.md), rename `## [Unreleased]` to
   `## [0.1.0] - YYYY-MM-DD` and add a new empty `## [Unreleased]` above it. Commit:
   ```bash
   git commit -am "chore: release v0.1.0"
   git push
   ```
3. Create and push the tag:
   ```bash
   git tag -a v0.1.0 -m "v0.1.0"
   git push origin v0.1.0
   ```
4. Watch **Actions → Release** on GitHub. When it finishes, the release appears under
   **Releases**, with release notes generated from the commit messages.

## Trying it locally first

`make snapshot` runs the whole release build without publishing anything and puts the
results in `dist/`. It needs [GoReleaser](https://goreleaser.com/install/) installed.
`make release-check` only validates `.goreleaser.yaml`.

Install a locally built package to try it:

```bash
sudo apt install ./dist/apptrol_*_amd64.deb
```

## If a release goes wrong

Delete the GitHub Release (web UI) and the tag, fix the problem, then tag again:

```bash
git push --delete origin v0.1.0
git tag -d v0.1.0
```

Only do this for a release nobody has downloaded yet; otherwise publish a new PATCH version.
