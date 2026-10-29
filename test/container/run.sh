#!/bin/sh
# Discard session variables before Bash can load BASH_ENV. The exceptions are an
# audit-only label and the number of repeated runs, both validated and removed
# before Podman (QA-16, ADR 0034 note, #172). RUNS stays empty when set empty, so
# the runner refuses it instead of guessing.
exec /usr/bin/env -i PATH=/usr/bin:/bin APPTROL_AUDIT_AGENT="${AGENT:-unspecified}" APPTROL_RUNS="${RUNS-1}" \
    /bin/bash --noprofile --norc "$(/usr/bin/dirname -- "$0")/runner.sh" "${1:-}"
