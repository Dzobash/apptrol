#!/bin/sh
# Check kernel enforcement, not merely accepted Podman flags (QA-16, ADR 0034).
set -eu
read -r memory < /sys/fs/cgroup/memory.max
read -r swap < /sys/fs/cgroup/memory.swap.max
read -r pids < /sys/fs/cgroup/pids.max
check_limit() {
    if [ "$1" != "$2" ]; then
        printf 'FAIL: %s expected %s, got %s\n' "$3" "$2" "$1" >&2
        exit 1
    fi
}
check_limit "$memory" 536870912 memory.max
check_limit "$swap" 0 memory.swap.max
check_limit "$pids" 256 pids.max
# Remove comm (which may contain spaces), then nice is field 17 of the remainder.
read -r stat < /proc/1/stat
stat=${stat##*) }
set -- $stat
shift 16
if [ "$1" -lt 10 ]; then
    echo "FAIL: nice priority must be at least 10, got $1" >&2
    exit 1
fi
test -z "${SSH_AUTH_SOCK:-}"
test -z "${FLAGS:-}"
set -- /sys/class/net/*
test "$#" = 1
test "$1" = /sys/class/net/lo
echo 'PASS: memory, swap, pids, nice and network limits'
