#!/bin/sh
# Log in-image diagnostics after the authoritative host proof (QA-16, ADR 0037).
set -eu
/bin/sh /src/test/container/limits.sh
cd /work
exec /bin/sh "/src/.audit/recipes/$1/commands.sh"
