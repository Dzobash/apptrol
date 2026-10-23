#!/bin/sh
# This file is executed inside the container only (QA-16, ADR 0034).
set -eu
test -r /src/README.md
printf 'sample recipe passed\n' > /work/result.txt
cat /work/result.txt
