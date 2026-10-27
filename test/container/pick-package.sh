#!/bin/bash
# Print the one package in /src/dist that belongs to this container and this commit
# (QA-17, ADR 0034). A snapshot left there by an earlier commit must not pass for
# the current one.
#
#   pick-package.sh <deb|rpm> <architecture in the file name> <full commit>
#
# The file name carries GoReleaser's abbreviation of the commit (`snapshot.<hash>`).
# Its length is GoReleaser's choice and the host Git's `core.abbrev` is another, so
# the check accepts any abbreviation of seven or more digits that starts the full
# commit, instead of comparing two abbreviations.
set -euo pipefail
ext=$1 arch=$2 commit=$3
[[ $ext == deb || $ext == rpm ]] || { echo "FAIL: unknown package type $ext" >&2; exit 2; }
[[ $commit =~ ^[0-9a-f]{40,64}$ ]] || { echo 'FAIL: the source commit must be a full hexadecimal hash' >&2; exit 2; }
found=()
for file in /src/dist/apptrol[_-]*."$ext"; do
    [[ -f $file && ! -L $file && $file == *"$arch"* ]] || continue
    [[ ${file##*/} =~ snapshot\.([0-9a-f]{7,64})[_-] ]] || continue
    [[ $commit == "${BASH_REMATCH[1]}"* ]] && found+=("$file")
done
if ((${#found[@]} != 1)); then
    printf 'FAIL: need exactly one %s for %s from commit %s in dist/; found %d. Run make snapshot.\n' \
        "$ext" "$arch" "${commit:0:12}" "${#found[@]}" >&2
    exit 1
fi
printf '%s\n' "${found[0]}"
