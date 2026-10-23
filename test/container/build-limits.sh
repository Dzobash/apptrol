#!/bin/sh
# Check the effective ancestor limit across repeated RUN steps: the runtime
# recreates its leaf, but the enclosing scope persists (QA-16, ADR 0037).
set -eu
group=$(sed -n 's/^0:://p' /proc/self/cgroup)
test "$group" = "$EXPECTED_SCOPE/build" || {
    echo 'FAIL: build escaped its limited scope' >&2; exit 1;
}
base="/sys/fs/cgroup$EXPECTED_SCOPE"
for pair in memory.max:536870912 memory.swap.max:0 pids.max:256; do
    file=${pair%:*}
    expected=${pair#*:}
    actual=$(cat "$base/$file")
    if [ "$actual" != "$expected" ]; then
        echo "FAIL: build scope $file does not match the required limit" >&2
        exit 1
    fi
done
read -r stat < /proc/1/stat
stat=${stat##*) }
set -- $stat
shift 16
test "$1" -ge 10
echo 'PASS: build scope ancestry, memory, swap, pids and nice limits'
