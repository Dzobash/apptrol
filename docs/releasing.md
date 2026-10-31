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

Every file also gets a **build provenance attestation**, signed by GitHub for this
repository's release workflow and kept with the repository (*Attestations*). Anyone can
check a download with `gh attestation verify <file> --repo Dzobash/apptrol`
([install guide](install.md#checking-a-download)); nothing needs to be managed, there is no
key (NFR-05).

A pre-release's Debian and RPM versions contain `~` (`0.2.1~rc1`), so they sort before the
final release. GitHub does not allow `~` in file names, so the files are named with `-`
instead, like the tag: `apptrol_0.2.1-rc1_amd64.deb`. The version inside keeps the `~`.

The packages install:

| File | Location |
|---|---|
| Program | `/usr/bin/apptrol` |
| systemd user service | `/usr/lib/systemd/user/apptrol.service` |
| Example configuration | `/usr/share/doc/apptrol/examples/config.toml` |
| AppStream metadata (what software centers show) | `/usr/share/metainfo/io.github.dzobash.apptrol.metainfo.xml` |
| Icon | `/usr/share/icons/hicolor/scalable/apps/io.github.dzobash.apptrol.svg`, and the 16 px version in `…/16x16/apps/` |
| README, changelog | `/usr/share/doc/apptrol/` |
| Apptrol copyright and MIT license | `/usr/share/doc/apptrol/copyright` in DEP-5 format on deb; `/usr/share/licenses/apptrol/LICENSE` on rpm; `LICENSE` in archives |
| Third-party licenses | `/usr/share/doc/apptrol/THIRD_PARTY_LICENSES` (`/usr/share/licenses/apptrol/` on rpm), and in the archives |

The deb copyright file comes from
[`packaging/debian/copyright`](https://github.com/Dzobash/apptrol/blob/main/packaging/debian/copyright),
in [DEP-5 format](https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/)
(NFR-06, NFR-09). Keep its copyright notice and MIT text in sync with `LICENSE`.

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

1. Check that CI is green on `main` and that the candidate's run record in
   `docs/testing/runs/` has [passed](testing/how-to-run-release-tests.md#what-passed-means),
   with the owner's decision: all cases for a MINOR release, the smoke set and the changed
   requirements' cases for a PATCH release
   ([ADR 0032](adr/0032-patch-releases-test-what-changed.md)).
   Check that the release's milestone has no open issues left, as before the candidate
   (see [Release candidates](#release-candidates)).
2. On a branch `chore/release-v0.1.0`, in [CHANGELOG.md](https://github.com/Dzobash/apptrol/blob/main/CHANGELOG.md), add
   `## [0.1.0] - YYYY-MM-DD` below `## [Unreleased]`, so that the entries move under it
   and `[Unreleased]` stays, empty, above; update the links at the end of the file.
   Add the release at the top of `<releases>` in
   `packaging/appstream/io.github.dzobash.apptrol.metainfo.xml`
   (`<release version="0.1.0" date="YYYY-MM-DD">` with its release page as `details`), so
   software centers such as KDE Discover show it.
   Mark the phase as released in the
   [roadmap](roadmap.md), and redraw the controls picture if features changed
   (`docs/assets/photos/make-controls.py`). Open a pull request
   `chore: release v0.1.0`: `main` accepts changes only through pull requests.
3. The release must contain the code of the candidate that passed: only docs may differ,
   the `<release>` entry in the AppStream file (it describes the version, it changes
   no behaviour), and files that change nothing that is built or packaged: tests
   (`*_test.go`), `.gitignore`, and check-only targets in the `Makefile` (such as `fmt`),
   never the build or `third-party-licenses` targets. After the merge, find the release
   PR's merge commit by its SHA, check that commit, and tag that same commit, never
   `HEAD` or a branch name: another PR may have been merged since, or your checkout may
   be on another branch.
   ```text
   main:  ── A ── M (chore: release v0.1.0) ── C (another PR)
                  ▲
                  tag v0.1.0 here, by M's SHA
   ```
   ```bash
   git fetch origin
   gh pr view <n> --json mergeCommit --jq .mergeCommit.oid   # M's SHA
   git log -1 --format='%H %s' <sha>    # check: "chore: release v0.1.0 (#<n>)"
   git diff --stat v0.1.0-rc1 <sha>     # *.md, docs/, packaging/appstream/, *_test.go, .gitignore, Makefile checks
   git tag -a v0.1.0 <sha> -m "v0.1.0"
   git push origin v0.1.0
   ```
   If the commit to tag changes (e.g. a fix to the release PR), check the new SHA again
   before tagging it.
4. Watch **Actions → Release** on GitHub. When it finishes, the release appears under
   **Releases**, with release notes generated from the commit messages.
5. Close the release's milestone (`0.1.0 — <phase name>`, under **Issues → Milestones**;
   every release has one, a patch too:
   [CONTRIBUTING › Milestones](https://github.com/Dzobash/apptrol/blob/main/CONTRIBUTING.md#milestones)).
   It has no open issues left (step 1).

## Release candidates

Before a new version, publish a release candidate and test the installed package:

1. First check the release's milestone (`0.1.0 — <phase name>`) for open issues: move
   each one to another milestone or to the backlog, and write the decision in the issue.
   Then tag a commit on `main` with a suffix; the changelog stays under `[Unreleased]`.
   Name the commit by its SHA, check that one, and tag that one, not your checkout's
   `HEAD`:
   ```bash
   git fetch origin
   git rev-parse origin/main               # the commit to test: note its SHA
   git log -1 --format='%H %s' <sha>       # check: the commit you meant
   gh run list --commit <sha>              # check: CI green on it
   git tag -a v0.1.0-rc1 <sha> -m "v0.1.0-rc1"
   git push origin v0.1.0-rc1
   ```
   If `main` moved before you tag and you want the newer commit, check its SHA again.
2. GitHub publishes it as a **pre-release**. Check the download as users would
   ([install guide](install.md#checking-a-download): `sha256sum -c` and
   `gh attestation verify`), install the package, enable the service and complete the
   manual test run ([how-to](testing/how-to-run-release-tests.md)), with a run record
   named after the candidate's tag (`docs/testing/runs/v0.1.0-rc1.md`).
3. When the run has [passed](testing/how-to-run-release-tests.md#what-passed-means),
   which needs the owner's decision in the record, make the release as described above,
   with the same code. Otherwise fix what failed, merge, and tag `v0.1.0-rc2`, which
   gets its own run record.

## Trying it locally first

`make snapshot` runs the whole release build without publishing anything and puts the
results in `dist/`. It needs [GoReleaser](https://goreleaser.com/install/) installed.
`make release-check` only validates `.goreleaser.yaml`.

CI's *Packaging* job already does this on every pull request: `goreleaser check`, a
snapshot build and `lintian` on the .deb, where errors fail and warnings and informational tags are printed
([testing.md](testing.md#what-runs-in-ci)). It cannot test installing the package, so the
manual run on a candidate stays. To look at the same on your machine, when `lintian` is
available, check the snapshot deb before installing it:

```bash
make snapshot
lintian --info --display-info dist/*.deb
```

There must be no `lintian` tag about the copyright file (NFR-09), such as
`no-copyright-file`, tags ending in `-dep5-copyright`, or tags starting with
`copyright-`. No informational (`I:`) tags are expected, including description tags
(NFR-10). Record other tags separately; the `W:` warnings are tracked in #153. CI suppresses
two tags, `no-changelog` and `statically-linked-binary`, each with its reason in
`ci.yml`; a plain `lintian` run shows them too.
For an installed release candidate, run
[TC-NFR-001](testing/cases/nfr.md) and record what Discover shows for the local deb.
DEP-5 does not guarantee that Discover will display the license (#97).

Install a locally built package to try it:

```bash
sudo apt install ./dist/apptrol_*_amd64.deb
```

## Repository settings

The repository is public, and these settings are in place. Check them again after
changing the repository's settings, or when a check or Dependabot stops working:

1. **Settings → General → Social preview**:
   `docs/assets/brand/png/apptrol-social-preview.png`.
2. **Settings → Rules → Rulesets**: a branch ruleset for `main` requires a pull request
   and passing status checks (the CI jobs) before merging (QA-02).
3. **Settings → General → Pull Requests**: *Allow auto-merge* is on (for Dependabot).
4. **Settings → Code security**: Dependabot alerts, security updates and private
   vulnerability reporting are on.

## If a release goes wrong

Delete the GitHub Release (web UI) and the tag, fix the problem, then tag again:

```bash
git push --delete origin v0.1.0
git tag -d v0.1.0
```

Only do this for a release nobody has downloaded yet; otherwise publish a new PATCH version.
