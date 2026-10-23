#!/bin/bash
# Private build worker in a delegated, limited user scope (QA-16, ADR 0037).
set -euo pipefail
repo=$(cd -- "$(dirname -- "$0")/../.." && pwd -P)
group=$(sed -n 's/^0:://p' /proc/self/cgroup)
[[ $group == */apptrol-build-*.scope ]] || { echo 'Build worker requires its dedicated scope'; exit 1; }
base="/sys/fs/cgroup$group"
[[ $(cat "$base/pids.max") == 256 && $(cat "$base/memory.max") == 536870912 && $(cat "$base/memory.swap.max") == 0 ]] || {
    echo 'FAIL: build scope kernel limits do not match the required budget' >&2; exit 1;
}
# Move the supervisor to a leaf before delegating controllers to the build leaf.
# All leaves remain subject to the scope's aggregate limits (QA-16, ADR 0037).
mkdir "$base/supervisor"
echo "$$" > "$base/supervisor/cgroup.procs"
echo '+memory +pids' > "$base/cgroup.subtree_control"
engine=(/usr/bin/podman --cgroup-manager=cgroupfs --default-mounts-file=/dev/null --hooks-dir="$repo/test/container/empty-hooks")
args=(--cgroup-parent="$group/build" --memory=512m --memory-swap=512m --http-proxy=false --jobs=1)
/usr/bin/nice -n 10 "${engine[@]}" build "${args[@]}" --no-cache \
    --cgroupns=host --build-arg "EXPECTED_SCOPE=$group" \
    --iidfile "$4/probe-image" --file "$repo/test/container/build-probe.Dockerfile" "$repo/test/container"
"${engine[@]}" image rm "$(cat "$4/probe-image")"
if ! "${engine[@]}" image exists "$3"; then
    /usr/bin/nice -n 10 "${engine[@]}" build "${args[@]}" --no-cache --cgroupns=private --tag "$3" --file "$1" "$2"
fi
