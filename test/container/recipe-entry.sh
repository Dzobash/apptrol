#!/bin/sh
# A recipe must pass the same limit checks before it can do work (QA-16, ADR 0034).
set -eu
/bin/sh /src/test/container/limits.sh
cd /work
exec /bin/sh "/src/.audit/recipes/$1/commands.sh"
