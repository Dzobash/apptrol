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
# Host-side Git sources are unsupported: even anonymous HTTPS Git may consult
# the owner's .netrc. Disable transports as well as configuration and prompts.
# Public archive URLs remain available (QA-16, ADR 0037).
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0
export GIT_ASKPASS=/bin/false GIT_SSH_COMMAND=/bin/false GIT_ALLOW_PROTOCOL=
mkdir "$4/registry-certs"
printf '{"auths":{}}\n' > "$4/registry-auth.json"
args=(--cgroup-parent="$group/build" --memory=512m --memory-swap=512m --http-proxy=false --jobs=1
    --authfile "$4/registry-auth.json" --cert-dir "$4/registry-certs")
/usr/bin/nice -n 10 "${engine[@]}" build "${args[@]}" --no-cache \
    --cgroupns=host --build-arg "EXPECTED_SCOPE=$group" \
    --iidfile "$4/probe-image" --file "$repo/test/container/build-probe.Dockerfile" "$repo/test/container"
"${engine[@]}" image rm "$(cat "$4/probe-image")"
rm -- "$4/probe-image"
if ! "${engine[@]}" image exists "$3"; then
    /usr/bin/nice -n 10 "${engine[@]}" build "${args[@]}" --no-cache --cgroupns=private --tag "$3" --file "$1" "$2"
fi
rm -- "$4/registry-auth.json"
rmdir -- "$4/registry-certs"
