# Apptrol — Releasing

Releases are built and published automatically by
[`.github/workflows/release.yml`](https://github.com/Dzobash/apptrol/blob/main/.github/workflows/release.yml) with
[GoReleaser](https://goreleaser.com) (config: [`.goreleaser.yaml`](https://github.com/Dzobash/apptrol/blob/main/.goreleaser.yaml)).
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
| Third-party licenses | `/usr/share/doc/apptrol/THIRD_PARTY_LICENSES` (`/usr/share/licenses/apptrol/` on rpm), and in the archives |

`THIRD_PARTY_LICENSES` holds the copyright notice and license text of every library
compiled into the binary, and of Go's standard library, as their licenses require
(NFR-08). GoReleaser generates it before every build (`make third-party-licenses`, with
`go-licenses`); it is not kept in the repository, so it never goes stale. The same target
runs in CI on every pull request and fails on a library with an unknown or restricted
license, so a new dependency is checked before it reaches a release.

The service is **not** enabled automatically; each user runs
`systemctl --user enable --now apptrol` once.

## Versions

[Semantic Versioning](https://semver.org): `vMAJOR.MINOR.PATCH`.

- Before `1.0.0`, a new phase raises MINOR (`0.1.0`, `0.2.0`, …); fixes raise PATCH.
- A tag with a suffix, such as `v0.1.0-rc1`, is published as a **pre-release**.

## Making a release

1. Check that CI is green on `main` and, from Phase 1 on, that the
   [hardware checklist](testing.md#manual-hardware-checklist) passes.
2. On a branch `chore/release-v0.1.0`, in [CHANGELOG.md](https://github.com/Dzobash/apptrol/blob/main/CHANGELOG.md), add
   `## [0.1.0] - YYYY-MM-DD` below `## [Unreleased]`, so that the entries move under it
   and `[Unreleased]` stays, empty, above; update the links at the end of the file.
   Mark the phase as released in the
   [roadmap](roadmap.md), and redraw the controls picture if features changed
   (`docs/assets/photos/make-controls.py`). Open a pull request
   `chore: release v0.1.0`: `main` accepts changes only through pull requests.
3. The release must contain the code of the candidate that passed: only docs may differ.
   After the merge, check it, then tag the merge commit:
   ```bash
   git switch main && git pull
   git diff --stat v0.1.0-rc1 HEAD      # only *.md files and docs/ may appear
   git tag -a v0.1.0 -m "v0.1.0"
   git push origin v0.1.0
   ```
4. Watch **Actions → Release** on GitHub. When it finishes, the release appears under
   **Releases**, with release notes generated from the commit messages.

## Release candidates

Before a new version, publish a release candidate and test the installed package:

1. Tag `main` with a suffix; the changelog stays under `[Unreleased]`:
   ```bash
   git tag -a v0.1.0-rc1 -m "v0.1.0-rc1"
   git push origin v0.1.0-rc1
   ```
2. GitHub publishes it as a **pre-release**. Install the package, enable the service and
   complete the [hardware checklist](testing.md#manual-hardware-checklist).
3. Fix what fails, merge, and tag `v0.1.0-rc2`. When a candidate passes, make the release
   as described above, with the same code. Problems that are not new in this version,
   or that are harmless, need no new candidate: open an issue and fix them in the next
   PATCH release.

## Trying it locally first

`make snapshot` runs the whole release build without publishing anything and puts the
results in `dist/`. It needs [GoReleaser](https://goreleaser.com/install/) installed.
`make release-check` only validates `.goreleaser.yaml`.

Install a locally built package to try it:

```bash
sudo apt install ./dist/apptrol_*_amd64.deb
```

## Making the repository public

Some GitHub features only work on public repositories. When the repository goes public:

1. **Settings → General → Social preview → Edit**: upload
   `docs/assets/brand/png/apptrol-social-preview.png`.
2. **Settings → Rules → Rulesets → New branch ruleset** for `main`: require a pull request
   and passing status checks (the CI jobs) before merging (QA-02).
3. **Settings → General → Pull Requests**: enable *Allow auto-merge* (for Dependabot).
4. **Settings → Code security**: check that Dependabot alerts, security updates and private
   vulnerability reporting are on.
5. Remove "private until the first release" from the roadmap.

## If a release goes wrong

Delete the GitHub Release (web UI) and the tag, fix the problem, then tag again:

```bash
git push --delete origin v0.1.0
git tag -d v0.1.0
```

Only do this for a release nobody has downloaded yet; otherwise publish a new PATCH version.
