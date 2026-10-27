#!/bin/bash
# Read only this job attempt's audit suffix; committed history is not CI evidence
# for this attempt (QA-16, ADR 0034). Usage: audit-file initial-lines tested-commit.
set -euo pipefail
audit=${1:-}
initial=${2:-}
commit=${3:-}

# Only validated data reaches Markdown. A missing boundary must never expose the
# historical passes already in the checkout (QA-16, ADR 0034).
heading=unknown
if [[ $commit =~ ^[0-9a-f]{7,64}$ ]]; then
    heading=${commit:0:7}
else
    audit=/dev/null
fi
if [[ ! $initial =~ ^[0-9]+$ || ! -f $audit || ! -r $audit ]]; then
    audit=/dev/null
    initial=0
fi
printf '### Systemd in containers — commit %s\n\n' "$heading"
printf '| Distro | Result | Time |\n|---|---|---|\n'

awk -v initial="$initial" -v commit="$commit" '
    NR <= initial { next }
    {
        for (key in field) delete field[key]
        for (key in count) delete count[key]
        for (i = 1; i <= NF; i++) {
            equal = index($i, "=")
            if (!equal) continue
            key = substr($i, 1, equal - 1)
            field[key] = substr($i, equal + 1)
            count[key]++
        }
        recipe = field["recipe"]
        if (recipe != "systemd-ubuntu" && recipe != "systemd-fedora") next
        source = field["commit"]
        if (source !~ /^[0-9a-f]+$/ || length(source) < 7 || index(commit, source) != 1) next
        seen[recipe]++
        if (count["recipe"] != 1 || count["commit"] != 1 || count["result"] != 1) next
        if (field["result"] == "PASS") result[recipe] = "✅ pass"
        if (field["result"] == "FAIL") result[recipe] = "❌ fail"
        if (count["seconds"] == 1 && field["seconds"] ~ /^[0-9]+$/) time[recipe] = field["seconds"] " s"
    }
    END {
        for (i = 1; i <= 2; i++) {
            recipe = i == 1 ? "systemd-ubuntu" : "systemd-fedora"
            distro = i == 1 ? "Ubuntu 26.04" : "Fedora 44"
            valid = seen[recipe] == 1 && result[recipe] != ""
            printf "| %s | %s | %s |\n", distro, valid ? result[recipe] : "no result", valid && time[recipe] != "" ? time[recipe] : "—"
        }
    }
' "$audit"
