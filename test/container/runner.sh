#!/bin/bash
# Private implementation of run.sh; the public entry point clears the environment
# before Bash starts (QA-16, ADR 0034).
set -euo pipefail
umask 077
label=${APPTROL_AUDIT_AGENT:-unspecified}
unset APPTROL_AUDIT_AGENT
agent=unspecified
repo=$(cd -- "$(dirname -- "$0")/../.." && pwd -P)
cd "$repo"
export HOME XDG_RUNTIME_DIR CONTAINERS_CONF
HOME=$(getent passwd "$(id -u)" | cut -d: -f6)
XDG_RUNTIME_DIR="/run/user/$(id -u)"
CONTAINERS_CONF="$repo/test/container/containers.conf"
# mounts.conf and OCI hooks are separate from containers.conf; neither may add
# host paths behind the script's explicit mount inventory (QA-16, ADR 0034).
engine=(/usr/bin/podman --default-mounts-file=/dev/null --hooks-dir="$repo/test/container/empty-hooks")

# Never follow a workspace/log link into another host directory (QA-16).
for path in .tmp .audit .audit/logs .audit/runs.log .tmp/runner.lock; do
    [[ ! -L $path ]] || { echo "Refusing symlink: $path" >&2; exit 1; }
done
mkdir -p .tmp .audit/logs
[[ ! -e .audit/runs.log || -f .audit/runs.log ]]
# Serialize invocations in this checkout; their distro jobs may run side by side.
# Otherwise two invocations could each reserve the same available memory.
exec 9>.tmp/runner.lock
flock 9
available=$(awk '/^MemAvailable:/ {print $2}' /proc/meminfo)
jobs=$(( (available - 4194304) / 524288 ))
((jobs <= 16)) || jobs=16
((jobs >= 0)) || jobs=0

# The trap is installed before validation or Podman: rejected attempts count too.
# Raw names/errors stay in local logs, never the committed audit line (QA-16).
run_one() (
    run=$(mktemp -d "$repo/.tmp/run-XXXXXXXX")
    runid=${run##*/}
    log="$repo/.audit/logs/$runid.log"
    start=$SECONDS
    recipe=invalid
    digest=unknown
    image=unknown
    container="apptrol-$runid"
    created=0
    exec 3>&1 4>&2
    finish() {
        rc=$?
        trap - EXIT
        exec 1>&3 2>&4
        if ((created)); then
            "${engine[@]}" rm --force --ignore "$container" >>"$log" 2>&1 || rc=1
        fi
        # Recipes may create files as a subordinate UID. Delete only this run's
        # workspace in Podman's user namespace, and count cleanup failures too.
        if ((rc == 0)); then
            "${engine[@]}" unshare rm -rf -- "$run" >>"$log" 2>&1 || rc=1
        fi
        result=FAIL
        if ((rc == 0)); then result=PASS; fi
        # flock keeps a complete line together even when both distros finish at once.
        (
            flock 8
            printf '%s run=%s agent=%s branch=%s commit=%s recipe=%s hash=%s image=%s jobs=%s result=%s seconds=%s\n' \
                "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$runid" "$agent" "$(git branch --show-current)" \
                "$(git rev-parse --short HEAD)" "$recipe" "$digest" "$image" "$jobs" "$result" "$((SECONDS-start))" >&8
        ) 8>>.audit/runs.log
        cat "$log"
        if ((rc == 0)); then
            rm -f -- "$log"
        else
            echo "Failed run kept: .tmp/$runid and .audit/logs/$runid.log" >&2
        fi
        exit "$rc"
    }
    trap finish EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM
    exec >"$log" 2>&1
    [[ $label =~ ^[a-zA-Z0-9][a-zA-Z0-9.-]{0,63}$ ]] || { echo 'AGENT must be a short model or caller label'; exit 2; }
    agent=$label
    [[ $1 =~ ^[a-zA-Z0-9][a-zA-Z0-9-]*$ ]] || { echo 'RECIPE must contain only letters, digits and hyphens'; exit 2; }
    recipe=$1
    case ${2:-recipe} in
        systemd)
            systemd=1
            distro=${recipe#systemd-}
            dockerfile="test/container/$distro.Dockerfile"
            context=test/container
            ;;
        *)
            systemd=0
            context=".audit/recipes/$recipe"
            dockerfile="$context/Dockerfile"
            [[ ! -L .audit/recipes && -d $context && ! -L $context ]] || { echo 'Recipe folder is missing or linked'; exit 2; }
            [[ -f $dockerfile && -f $context/commands.sh ]] || { echo 'Recipe needs Dockerfile and commands.sh'; exit 2; }
            [[ -z $(find "$context" -type l -print -quit) ]] || { echo 'Recipe may not contain symlinks'; exit 2; }
            ;;
    esac
    [[ $repo != *','* && $repo != *$'\n'* ]] || { echo 'Checkout path cannot contain comma or newline'; exit 2; }
    ((jobs > 0)) || { echo 'Not enough available memory to leave 4 GiB free'; exit 1; }
    [[ $(id -u) != 0 ]] || { echo 'Rootless Podman is required'; exit 1; }
    [[ $("${engine[@]}" info --format '{{.Host.Security.Rootless}}') == true ]] || { echo 'Podman must be rootless'; exit 1; }
    # Hash all recipe inputs, while the systemd image depends only on its Dockerfile.
    if ((systemd)); then
        buildhash=$(sha256sum "$dockerfile" | cut -d' ' -f1)
    else
        buildhash=$(find "$context" -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum | cut -d' ' -f1)
    fi
    digest=$({ printf '%s\n' "$buildhash"; sha256sum test/container/*.sh test/container/valid.toml test/container/containers.conf; } | sha256sum | cut -d' ' -f1)
    image="localhost/apptrol-$recipe:$buildhash"
    echo "recipe=$recipe jobs=$jobs image=$image"
    echo "host $repo -> container /src read-only; host $run -> container /work writable"
    if ! "${engine[@]}" image exists "$image"; then
        /usr/bin/nice -n 10 "${engine[@]}" build --memory=512m --memory-swap=512m \
            --http-proxy=false --jobs=1 --tag "$image" --file "$dockerfile" "$context"
    fi
    args=(--name "$container" --network=none --memory=512m --memory-swap=512m --pids-limit=256
        --http-proxy=false --unsetenv-all --image-volume=ignore --user=0:0
        --env PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
        --mount "type=bind,src=$repo,dst=/src,ro=true"
        --mount "type=bind,src=$run,dst=/work,rw=true")
    if ((systemd)); then
        args+=(--systemd=always --entrypoint /sbin/init "$image" --log-target=console)
    else
        args+=(--systemd=false --entrypoint /bin/sh "$image" /src/test/container/recipe-entry.sh "$recipe")
    fi
    # Inspect before starting: inherited/default mounts must not silently widen access.
    created=1
    /usr/bin/nice -n 10 "${engine[@]}" create "${args[@]}"
    mounts=$("${engine[@]}" inspect --format '{{range .Mounts}}{{.Source}}|{{.Destination}}|{{.RW}}{{"\n"}}{{end}}' "$container" | sed '/^$/d' | sort)
    expected=$(printf '%s|/src|false\n%s|/work|true\n' "$repo" "$run" | sort)
    [[ $mounts == "$expected" ]] || { echo 'Unexpected container mounts; refusing to start'; exit 1; }
    if ((systemd)); then
        /usr/bin/nice -n 10 "${engine[@]}" start "$container"
        /usr/bin/nice -n 10 "${engine[@]}" exec "$container" /bin/bash /src/test/container/proof.sh
        /usr/bin/nice -n 10 "${engine[@]}" exec "$container" /bin/bash /src/test/container/status.sh
    else
        /usr/bin/nice -n 10 "${engine[@]}" start --attach "$container"
        [[ $("${engine[@]}" inspect --format '{{.State.ExitCode}}' "$container") == 0 ]]
    fi
)

children=()
stop_children() {
    trap - INT TERM
    if ((${#children[@]})); then
        kill -TERM "${children[@]}" 2>/dev/null || :
        wait "${children[@]}" || :
    fi
    exit 130
}
trap stop_children INT TERM
if [[ ${1:-} == @systemd ]]; then
    if ((jobs >= 2)); then
        run_one systemd-ubuntu systemd & first=$!
        run_one systemd-fedora systemd & second=$!
        children=("$first" "$second")
        failed=0
        wait "$first" || failed=1
        wait "$second" || failed=1
        exit "$failed"
    fi
    # Do not suppress errexit inside run_one by calling it as an if condition.
    run_one systemd-ubuntu systemd & first=$!
    children=("$first")
    failed=0
    wait "$first" || failed=1
    run_one systemd-fedora systemd & second=$!
    children=("$second")
    wait "$second" || failed=1
    exit "$failed"
fi
name=${1:-}
run_one "${name#recipe:}" & only=$!
children=("$only")
wait "$only"
