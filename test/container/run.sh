#!/bin/sh
# Discard session variables before Bash can load BASH_ENV. The one exception is
# an audit-only label, validated and removed before Podman (QA-16, ADR 0034 note).
exec /usr/bin/env -i PATH=/usr/bin:/bin APPTROL_AUDIT_AGENT="${AGENT:-unspecified}" /bin/bash --noprofile --norc \
    "$(/usr/bin/dirname -- "$0")/runner.sh" "${1:-}"
