#!/bin/bash
# Private implementation of run.sh; the public entry point clears the environment
# before Bash starts (QA-16, ADR 0034).
set -euo pipefail
umask 077
label=${APPTROL_AUDIT_AGENT:-unspecified}
# RUNS is caller data like AGENT: checked below, never in Podman's environment,
# arguments or paths (QA-16, ADR 0034, #172).
runs=${APPTROL_RUNS-1}
unset APPTROL_AUDIT_AGENT APPTROL_RUNS
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
# A plain decimal 1-1000: no sign, space or leading zero, so it is safe in
# arithmetic. Refused before the lock and before Podman; no run was attempted, so
# no audit line is written (QA-16, ADR 0034, #172).
if [[ ${1:-} == @systemd ]]; then
    [[ $runs =~ ^[1-9][0-9]{0,3}$ ]] && ((runs <= 1000)) || { echo 'RUNS must be a whole number from 1 to 1000' >&2; exit 2; }
fi
# Worktrees share the budget lock, but never mount it (QA-16, ADR 0034).
common=$(git rev-parse --path-format=absolute --git-common-dir)
lock="$common/apptrol-containers.lock"
[[ ! -L $lock && ( ! -e $lock || -f $lock ) ]] || { echo 'Refusing unsafe shared lock'; exit 1; }
exec 9>"$lock"
flock 9
available=$(awk '/^MemAvailable:/ {print $2}' /proc/meminfo)
jobs=$(( (available - 4194304) / 524288 ))
# Memory alone is not enough: per-user inotify instances are shared by every
# rootless container and the owner's desktop, and 16 systemd containers at once
# ran out of them. At most 8 at once is the owner's choice (QA-16, ADR 0034, #172).
((jobs <= 8)) || jobs=8
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
# Arguments: the run's folder from new_run, recipe, mode, and reuse=1 when an
# earlier run of this call built the image (QA-16, ADR 0034, #172).
run_one() (
    run=$1
    shift
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
        # A failed repeated run publishes its id under the stop marker's lock, so
        # the dispatcher starts nothing after it: at once when the check failed,
        # and again below if cleanup turns the result into FAIL (QA-16, #172).
        # A failed write sends no signal: the marker lives in .tmp/, and when
        # .tmp/ cannot be written the next run cannot make its own workspace
        # there either, so it fails before any container starts; the check when
        # this run is reaped still stops dispatch.
        published=0
        publish_failure() {
            ((published == 0)) && [[ -n ${stop_mark:-} ]] || return 0
            published=1
            { flock 6 && printf '%s\n' "$runid" >&6; } 6>>"$stop_mark" 2>/dev/null || :
        }
        if ((rc != 0)); then publish_failure; fi
        if ((created)); then
            "${engine[@]}" rm --force --ignore "$container" >>"$log" 2>&1 || rc=1
        fi
        # Recipes may create files as a subordinate UID. Delete only this run's
        # workspace in Podman's user namespace, and count cleanup failures too.
        if ((rc == 0)); then
            "${engine[@]}" unshare rm -rf -- "$run" >>"$log" 2>&1 || rc=1
        fi
        result=FAIL
        if ((rc == 0)); then result=PASS; else publish_failure; fi
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
    # Never cache the enforcement proof: delegation can change between calls.
    # Use the same build arguments for the probe and recipe (QA-16, ADR 0034).
    # Repeated runs build nothing: this call's first run of the distro built and
    # proved the image under the same lock, and --pull=never refuses a missing
    # one (QA-16, ADR 0034, #172).
    if [[ ${3:-} == reuse ]]; then
        echo 'image built by the first run of this call; no build here'
    else
        /usr/bin/systemd-run --user --scope --unit="apptrol-build-$runid" \
            -p Delegate=yes -p TasksMax=256 -p MemoryMax=512M -p MemorySwapMax=0 \
            /bin/bash "$repo/test/container/build.sh" "$repo/$dockerfile" "$repo/$context" "$image" "$run"
    fi
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
    # Let go of the stop marker's lock first: stopped runs write to it on the way out.
    exec 6<&-
    if ((${#children[@]})); then
        kill -TERM "${children[@]}" 2>/dev/null || :
        wait "${children[@]}" || :
    fi
    [[ -z ${stop_mark:-} ]] || rm -f -- "$stop_mark"
    exit 130
}
trap stop_children INT TERM
# The folder is made here, not inside run_one, so the caller knows each run id.
new_run() {
    mktemp -d "$repo/.tmp/run-XXXXXXXX"
}
# Both distros of one mode (systemd or install), side by side when memory allows.
run_distros() {
    local mode=$1
    if ((jobs >= 2)); then
        run=$(new_run)
        run_one "$run" "$mode-ubuntu" "$mode" & first=$!
        run=$(new_run)
        run_one "$run" "$mode-fedora" "$mode" & second=$!
        children=("$first" "$second")
        failed=0
        wait "$first" || failed=1
        wait "$second" || failed=1
        exit "$failed"
    fi
    # Do not suppress errexit inside run_one by calling it as an if condition.
    run=$(new_run)
    run_one "$run" "$mode-ubuntu" "$mode" & first=$!
    children=("$first")
    failed=0
    wait "$first" || failed=1
    run=$(new_run)
    run_one "$run" "$mode-fedora" "$mode" & second=$!
    children=("$second")
    wait "$second" || failed=1
    exit "$failed"
}

# Repeated runs collect the #172 evidence in one call: up to $jobs containers at
# once, all under the one lock, so other worktrees still wait. Each run keeps its
# own container, folder, run id and audit line. One red run resets the streak, so
# after the first failure nothing new starts; the running ones finish
# (QA-16, ADR 0034, #172).
declare -A running=() passes=() fails=()
first_failed=
# Failed runs write their ids here; empty means none yet (QA-16, #172).
stop_mark=
# Waits for any one run and counts it. Only INT and TERM are trapped, and their
# handler exits, so `wait` comes back with a run, or with none when no run is
# left: that is a dispatcher bug, never counted as a pass.
reap() {
    local pid= rc=0 recipe runid
    wait -n -p pid "${!running[@]}" || rc=$?
    if [[ -z ${pid:-} || -z ${running[${pid:-0}]:-} ]]; then
        echo "FAIL: waited for a run but none was collected (status $rc)" >&2
        exit 1
    fi
    read -r recipe runid <<< "${running[$pid]}"
    unset "running[$pid]"
    children=("${!running[@]}")
    if ((rc == 0)); then
        passes[$recipe]=$((${passes[$recipe]:-0} + 1))
    else
        fails[$recipe]=$((${fails[$recipe]:-0} + 1))
        first_failed=${first_failed:-$runid}
    fi
}
# Starts the recipes in order, at most $slots at once, none after a failure.
# Arguments: reuse or build, then the recipes.
start_runs() {
    local how=$1 recipe run
    shift
    for recipe in "$@"; do
        while ((${#running[@]} >= slots)); do reap; done
        # A failure counts from the moment its run writes the marker, not from when
        # it is reaped: with free slots nothing may be waiting on it. The check and
        # the start hold the marker's lock, which the failed run writes under, so
        # no run starts after a published failure (QA-16, #172).
        exec 6<"$stop_mark"
        flock 6
        if [[ -n $first_failed || -s $stop_mark ]]; then
            exec 6<&-
            return 0
        fi
        run=$(new_run)
        run_one "$run" "$recipe" systemd "$how" 6<&- &
        running[$!]="$recipe ${run##*/}"
        children=("${!running[@]}")
        exec 6<&-
    done
}
run_repeated() {
    local began=$SECONDS i recipe done_runs rest=()
    # With jobs=0 one slot is left, so run_one records the memory refusal.
    slots=$((jobs > 0 ? jobs : 1))
    stop_mark=$(mktemp "$repo/.tmp/stop-XXXXXXXX")
    # Each distro's first run builds and proves its image; the rest reuse it.
    start_runs build systemd-ubuntu systemd-fedora
    while ((${#running[@]})); do reap; done
    for ((i = 2; i <= runs; i++)); do
        rest+=(systemd-ubuntu systemd-fedora)
    done
    start_runs reuse "${rest[@]}"
    while ((${#running[@]})); do reap; done
    # The first published failure, if any, is the one named below.
    if [[ -s $stop_mark ]]; then
        read -r first_failed < "$stop_mark"
    fi
    rm -f -- "$stop_mark"
    echo "RUNS=$runs, up to $slots at once, wall time $((SECONDS - began)) s"
    for recipe in systemd-ubuntu systemd-fedora; do
        done_runs=$((${passes[$recipe]:-0} + ${fails[$recipe]:-0}))
        echo "$recipe: ${passes[$recipe]:-0} passed, ${fails[$recipe]:-0} failed, $((runs - done_runs)) not started"
    done
    if [[ -n $first_failed ]]; then
        echo "First failed run: $first_failed (.tmp/$first_failed, .audit/logs/$first_failed.log); no new runs were started after it" >&2
        exit 1
    fi
    exit 0
}
if [[ ${1:-} == @systemd ]]; then
    /bin/bash "$repo/test/container/build-apptrol.sh"
    # RUNS=1, the default, is the single run per distro it always was.
    if ((runs == 1)); then
        run_distros systemd
    fi
    run_repeated
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
#
# The preflight at the top checked for links before the lock, and the lock can take
# as long as a run does. So the folders are looked at again here, after the lock
# and before every removal (QA-18, review round 1).
# True when $1 (below the checkout) is a real folder with no link on its way.
real_folder_inside() {
    local want="$repo/$1" got
    got=$(cd -- "$want" 2>/dev/null && pwd -P) || return 1
    [[ $got == "$want" ]]
}
# `rm --one-file-system` does not stop at a bind mount of the same file system, so a
# folder with any mount at or below it is left alone. Status: 0 = a mount is there,
# 1 = none, 2 = could not tell; only 1 allows the removal. Mount points in
# mountinfo write a space as \040; $repo has no tab, newline or backslash here.
has_mount_inside() {
    P=$1 awk '
        { p = $5; gsub(/\\040/, " ", p) }
        p == ENVIRON["P"] || index(p, ENVIRON["P"] "/") == 1 { found = 1 }
        END { exit !found }
    ' /proc/self/mountinfo
}
remove_kept_folder() {
    [[ $1 =~ ^run-[A-Za-z0-9]{8}$ ]] || return 200
    local path="$repo/.tmp/$1" mounted=0
    real_folder_inside .tmp || return 202
    if [[ -L $path ]]; then
        # A link is removed as a link; its target is never touched.
        rm -f -- "$path"
    elif [[ -d $path ]]; then
        has_mount_inside "$path" || mounted=$?
        ((mounted == 1)) || return 203
        "${engine[@]}" unshare rm -rf --one-file-system -- "$path"
    else
        return 201
    fi
}
remove_kept_log() {
    [[ $1 =~ ^run-[A-Za-z0-9]{8}\.log$ ]] || return 200
    local path="$repo/.audit/logs/$1"
    real_folder_inside .audit/logs || return 202
    [[ -L $path || -f $path ]] || return 201
    rm -f -- "$path"
}
# The ledger is appended to only through a real .audit/runs.log: a link that appeared
# since the preflight must not send the line elsewhere (QA-18).
record_clean() {
    if [[ -L .audit || -L .audit/runs.log ]] || ! real_folder_inside .audit; then
        echo 'Not recorded: .audit or .audit/runs.log is a link' >&2
        return 1
    fi
    write_audit "$runid" @clean "$digest" none "$result" "$((SECONDS-start))"
}
clean_kept() (
    runid=$(mktemp -u clean-XXXXXXXX)
    start=$SECONDS
    result=FAIL
    digest=unknown
    # Installed before the checks below, which come after the shared preflight. The
    # preflight's own refusals (a link, another repository) write nothing, as for runs.
    trap 'record_clean || exit 1' EXIT
    [[ $label =~ ^[a-zA-Z0-9][a-zA-Z0-9.-]{0,63}$ ]] || { echo 'AGENT must be a short model or caller label' >&2; exit 2; }
    agent=$label
    digest=$(sha256sum test/container/*.sh test/container/valid.toml test/container/containers.conf | sha256sum | cut -d' ' -f1)
    [[ $repo != *[$'\\\t\n']* ]] || { echo 'Checkout path cannot contain a backslash, tab or newline' >&2; exit 2; }
    [[ $(id -u) != 0 ]] || { echo 'Rootless Podman is required' >&2; exit 1; }
    [[ $("${engine[@]}" info --format '{{.Host.Security.Rootless}}') == true ]] || { echo 'Podman must be rootless' >&2; exit 1; }
    real_folder_inside .tmp && real_folder_inside .audit/logs || { echo 'Refusing: .tmp or .audit/logs is a link, or has one on its path' >&2; exit 1; }
    echo "cleaning kept failed runs in $repo/.tmp and $repo/.audit/logs (not .audit/runs.log)"
    folders=0 logs=0 left=0 failed=0
    shopt -s nullglob
    for path in "$repo"/.tmp/*; do
        name=${path##*/}
        rc=0
        remove_kept_folder "$name" || rc=$?
        case $rc in
            0) echo "removed: .tmp/$name"; folders=$((folders + 1)) ;;
            200 | 201) echo "left alone: .tmp/$name"; left=$((left + 1)) ;;
            203) echo "FAILED: .tmp/$name has a mount in it" >&2; failed=$((failed + 1)) ;;
            *) echo "FAILED: .tmp/$name" >&2; failed=$((failed + 1)) ;;
        esac
    done
    for path in "$repo"/.audit/logs/*; do
        name=${path##*/}
        rc=0
        remove_kept_log "$name" || rc=$?
        case $rc in
            0) echo "removed: .audit/logs/$name"; logs=$((logs + 1)) ;;
            200 | 201) echo "left alone: .audit/logs/$name"; left=$((left + 1)) ;;
            *) echo "FAILED: .audit/logs/$name" >&2; failed=$((failed + 1)) ;;
        esac
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
run=$(new_run)
run_one "$run" "${name#recipe:}" & only=$!
children=("$only")
wait "$only"
