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
# Whoever can edit the checkout can edit its .git/config. The audit lookups must
# not run host programs through it (fsmonitor, hooks) nor describe another
# repository. GIT_CONFIG_COUNT settings outrank .git/config (QA-16, ADR 0034).
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null GIT_OPTIONAL_LOCKS=0 GIT_PAGER=cat \
    GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_0=core.fsmonitor GIT_CONFIG_VALUE_0=false \
    GIT_CONFIG_KEY_1=core.hooksPath GIT_CONFIG_VALUE_1=/dev/null
[[ $(git rev-parse --show-toplevel) == "$repo" ]] || { echo 'Refusing: Git describes another repository' >&2; exit 1; }
export HOME XDG_RUNTIME_DIR CONTAINERS_CONF
HOME=$(getent passwd "$(id -u)" | cut -d: -f6)
XDG_RUNTIME_DIR="/run/user/$(id -u)"
CONTAINERS_CONF="$repo/test/container/containers.conf"
# mounts.conf and OCI hooks are separate from containers.conf; neither may add
# host paths behind the script's explicit mount inventory (QA-16, ADR 0034).
engine=(/usr/bin/podman --default-mounts-file=/dev/null --hooks-dir="$repo/test/container/empty-hooks")

# Never follow a workspace/log link into another host directory (QA-16).
for path in .tmp .audit .audit/logs .audit/runs.log; do
    [[ ! -L $path ]] || { echo "Refusing symlink: $path" >&2; exit 1; }
done
mkdir -p .tmp .audit/logs
[[ ! -e .audit/runs.log || -f .audit/runs.log ]]
# Worktrees share the budget lock, but never mount it (QA-16, ADR 0034).
common=$(git rev-parse --path-format=absolute --git-common-dir)
lock="$common/apptrol-containers.lock"
[[ ! -L $lock && ( ! -e $lock || -f $lock ) ]] || { echo 'Refusing unsafe shared lock'; exit 1; }
exec 9>"$lock"
flock 9
available=$(awk '/^MemAvailable:/ {print $2}' /proc/meminfo)
jobs=$(( (available - 4194304) / 524288 ))
((jobs <= 16)) || jobs=16
((jobs >= 0)) || jobs=0
source_commit=$(git rev-parse --short HEAD)
# The audit line shows the short form; the package check needs the full commit,
# because the short length depends on this checkout's core.abbrev (QA-17).
source_full=$(git rev-parse HEAD)
source_dirty=no
source_status=$(git status --porcelain --untracked-files=normal -- . ':!.audit/runs.log')
if [[ -n $source_status ]]; then
    source_dirty=yes
fi

# One function writes every audit line, so a run and a cleanup cannot differ in
# format (QA-16, QA-18, ADR 0034). Arguments: run id, recipe, hash, image, result,
# seconds; the rest comes from the invocation (agent, branch, commit, jobs).
write_audit() {
    # flock keeps a complete line together even when both distros finish at once.
    (
        flock 8
        printf '%s run=%s agent=%s branch=%s commit=%s dirty=%s recipe=%s hash=%s image=%s jobs=%s result=%s seconds=%s\n' \
            "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1" "$agent" "$(git branch --show-current)" \
            "$source_commit" "$source_dirty" "$2" "$3" "$4" "$jobs" "$5" "$6" >&8
    ) 8>>.audit/runs.log
}

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
        write_audit "$runid" "$recipe" "$digest" "$image" "$result" "$((SECONDS-start))"
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
    # install runs the released packages in the same image and with the same
    # flags as systemd; only the check inside differs (QA-17, ADR 0034).
    install=0
    case ${2:-recipe} in
        systemd|install)
            systemd=1
            if [[ $2 == install ]]; then install=1; fi
            distro=${recipe#"$2"-}
            dockerfile="test/container/$distro.Dockerfile"
            context=test/container
            imagename=systemd-$distro
            ;;
        *)
            systemd=0
            imagename=$recipe
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
    # Changing build enforcement must invalidate images built before that proof.
    buildhash=$({ printf '%s\n' "$buildhash"; sha256sum test/container/build.sh test/container/build-limits.sh test/container/build-probe.Dockerfile test/container/containers.conf; } | sha256sum | cut -d' ' -f1)
    digest=$({ printf '%s\n' "$buildhash"; sha256sum test/container/*.sh test/container/valid.toml test/container/containers.conf; } | sha256sum | cut -d' ' -f1)
    image="localhost/apptrol-$imagename:$buildhash"
    echo "recipe=$recipe jobs=$jobs image=$image"
    echo "host $repo -> container /src read-only; host $run -> container /work writable"
    # Never cache the enforcement proof: delegation can change between runs.
    # Use the same build arguments for the probe and recipe (QA-16, ADR 0034).
    /usr/bin/systemd-run --user --scope --unit="apptrol-build-$runid" \
        -p Delegate=yes -p TasksMax=256 -p MemoryMax=512M -p MemorySwapMax=0 \
        /bin/bash "$repo/test/container/build.sh" "$repo/$dockerfile" "$repo/$context" "$image" "$run"
    args=(--name "$container" --pull=never --network=none --memory=512m --memory-swap=512m --pids-limit=256
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
    # OCI init waits for start: prove kernel state before trusting any image code
    # (including its shell). Missing host evidence fails closed (QA-16, ADR 0037).
    /usr/bin/nice -n 10 "${engine[@]}" init "$container"
    pid=$("${engine[@]}" inspect --format '{{.State.Pid}}' "$container")
    [[ $pid =~ ^[1-9][0-9]*$ ]] || { echo 'FAIL: initialized container has no host PID'; exit 1; }
    group=$(sed -n 's/^0:://p' "/proc/$pid/cgroup")
    [[ $group == /* && $group != / && $group != *'..'* && $group != *$'\n'* ]] || { echo 'FAIL: invalid container cgroup'; exit 1; }
    base="/sys/fs/cgroup$group"
    [[ $(cat "$base/memory.max") == 536870912 && $(cat "$base/memory.swap.max") == 0 && $(cat "$base/pids.max") == 256 ]] || {
        echo 'FAIL: host-read runtime cgroup limits'; exit 1;
    }
    read -r stat < "/proc/$pid/stat"
    stat=${stat##*) }
    read -ra fields <<< "$stat"
    [[ ${fields[16]:-missing} == 10 ]] || { echo 'FAIL: host-read runtime nice priority'; exit 1; }
    network=$(awk -F: 'NR > 2 {gsub(/[[:space:]]/, "", $1); print $1}' "/proc/$pid/net/dev")
    [[ $network == lo ]] || { echo 'FAIL: host-read runtime network'; exit 1; }
    # A recipe must not be able to undo this proof after start (QA-16, ADR 0037).
    if ((!systemd)); then
        cgroup_mount=$(awk '$5 == "/sys/fs/cgroup" && / - cgroup2 / {print $6}' "/proc/$pid/mountinfo")
        [[ ,$cgroup_mount, == *,ro,* && $cgroup_mount != *$'\n'* ]] || {
            echo 'FAIL: recipe cgroup mount is not proven read-only'; exit 1;
        }
    fi
    echo 'PASS: host-read runtime memory, swap, pids, nice and network limits'
    if ((systemd)); then
        /usr/bin/nice -n 10 "${engine[@]}" start "$container"
        /usr/bin/nice -n 10 "${engine[@]}" exec "$container" /bin/bash /src/test/container/proof.sh
        if ((install)); then
            # The commit is data from Git, checked here: the packages must come from
            # this checkout, not from an older snapshot left in dist/ (QA-17).
            [[ $source_full =~ ^[0-9a-f]{40,64}$ ]] || { echo 'FAIL: unexpected source commit'; exit 1; }
            /usr/bin/nice -n 10 "${engine[@]}" exec "$container" /bin/bash /src/test/container/status.sh package "$source_full"
        else
            /usr/bin/nice -n 10 "${engine[@]}" exec "$container" /bin/bash /src/test/container/status.sh
        fi
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
# Both distros of one mode (systemd or install), side by side when memory allows.
run_distros() {
    local mode=$1
    if ((jobs >= 2)); then
        run_one "$mode-ubuntu" "$mode" & first=$!
        run_one "$mode-fedora" "$mode" & second=$!
        children=("$first" "$second")
        failed=0
        wait "$first" || failed=1
        wait "$second" || failed=1
        exit "$failed"
    fi
    # Do not suppress errexit inside run_one by calling it as an if condition.
    run_one "$mode-ubuntu" "$mode" & first=$!
    children=("$first")
    failed=0
    wait "$first" || failed=1
    run_one "$mode-fedora" "$mode" & second=$!
    children=("$second")
    wait "$second" || failed=1
    exit "$failed"
}
if [[ ${1:-} == @systemd ]]; then
    /bin/bash "$repo/test/container/build-apptrol.sh"
    run_distros systemd
fi
# A regular file, not a folder or a link, with the extension $1 in dist/.
has_package() {
    local file
    for file in dist/*."$1"; do
        if [[ -f $file && ! -L $file ]]; then
            return 0
        fi
    done
    return 1
}
if [[ ${1:-} == @install ]]; then
    # The packages are built by `make snapshot`, never here: GoReleaser would run
    # unsandboxed on the host. A missing package is the caller's mistake, so it is
    # refused before any container starts (QA-17, ADR 0034).
    if [[ -L dist || ! -d dist ]] || ! has_package deb || ! has_package rpm; then
        echo 'Refusing: dist/ needs the .deb and .rpm of this commit; run make snapshot first' >&2
        exit 1
    fi
    run_distros install
fi
# Remove the folders and logs that failed runs keep (QA-18, ADR 0034). It starts no
# container and mounts nothing: `podman unshare` only lets this host process act as
# the user IDs a recipe may have written files as. Only exact names the script
# creates itself, directly inside this checkout's .tmp/ and .audit/logs/, are
# touched; each helper builds the path from a name it checks again, so no caller
# can hand it another path. The shared lock above is held for a whole run, so a run
# in progress is never cleaned under its feet.
remove_kept_folder() {
    [[ $1 =~ ^run-[A-Za-z0-9]{8}$ ]] || return 200
    local path="$repo/.tmp/$1"
    if [[ -L $path ]]; then
        # A link is removed as a link; its target is never touched.
        rm -f -- "$path"
    elif [[ -d $path ]]; then
        "${engine[@]}" unshare rm -rf --one-file-system -- "$path"
    else
        return 201
    fi
}
remove_kept_log() {
    [[ $1 =~ ^run-[A-Za-z0-9]{8}\.log$ ]] || return 200
    local path="$repo/.audit/logs/$1"
    [[ -L $path || -f $path ]] || return 201
    rm -f -- "$path"
}
clean_kept() (
    runid=$(mktemp -u clean-XXXXXXXX)
    start=$SECONDS
    result=FAIL
    digest=unknown
    # Installed before any check: a refused cleanup is recorded too, like a run.
    trap 'write_audit "$runid" @clean "$digest" none "$result" "$((SECONDS-start))"' EXIT
    [[ $label =~ ^[a-zA-Z0-9][a-zA-Z0-9.-]{0,63}$ ]] || { echo 'AGENT must be a short model or caller label' >&2; exit 2; }
    agent=$label
    digest=$(sha256sum test/container/*.sh test/container/valid.toml test/container/containers.conf | sha256sum | cut -d' ' -f1)
    [[ $(id -u) != 0 ]] || { echo 'Rootless Podman is required' >&2; exit 1; }
    [[ $("${engine[@]}" info --format '{{.Host.Security.Rootless}}') == true ]] || { echo 'Podman must be rootless' >&2; exit 1; }
    echo "cleaning kept failed runs in $repo/.tmp and $repo/.audit/logs (not .audit/runs.log)"
    folders=0 logs=0 left=0 failed=0
    shopt -s nullglob
    for path in "$repo"/.tmp/*; do
        name=${path##*/}
        if remove_kept_folder "$name"; then
            echo "removed: .tmp/$name"
            folders=$((folders + 1))
        elif (($? == 200 || $? == 201)); then
            echo "left alone: .tmp/$name"
            left=$((left + 1))
        else
            echo "FAILED: .tmp/$name" >&2
            failed=$((failed + 1))
        fi
    done
    for path in "$repo"/.audit/logs/*; do
        name=${path##*/}
        if remove_kept_log "$name"; then
            echo "removed: .audit/logs/$name"
            logs=$((logs + 1))
        elif (($? == 200 || $? == 201)); then
            echo "left alone: .audit/logs/$name"
            left=$((left + 1))
        else
            echo "FAILED: .audit/logs/$name" >&2
            failed=$((failed + 1))
        fi
    done
    echo "removed $folders run folders and $logs logs; left alone $left; failed $failed"
    ((failed == 0)) || exit 1
    result=PASS
)
if [[ ${1:-} == @clean ]]; then
    clean_kept
    exit 0
fi
name=${1:-}
run_one "${name#recipe:}" & only=$!
children=("$only")
wait "$only"
