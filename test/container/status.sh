#!/bin/bash
# Exercise the shipped unit and reload path, not a replacement service (SVC-13,
# QA-16, ADR 0034). No controller or sound server exists in these containers.
#
#   status.sh                    install the built binary and the unit by hand
#   status.sh package <commit>   install the released .deb or .rpm from /src/dist
#                                with apt or dnf and use what it installed (QA-17)
set -euo pipefail
mode=${1:-binary}
userctl() {
    runuser -u tester -- env XDG_RUNTIME_DIR=/run/user/1500 systemctl --user "$@"
}
diagnose() {
    # What the package manager put where, then the service's own view.
    dpkg-query -L apptrol 2>/dev/null || rpm -ql apptrol 2>/dev/null || :
    journalctl --no-pager -u user@1500.service || :
    runuser -u tester -- env XDG_RUNTIME_DIR=/run/user/1500 journalctl --user --no-pager -u apptrol || :
}
trap diagnose ERR
# The package for this container's own architecture, from this commit only: a
# snapshot left in dist/ by an earlier commit must not pass for the current one.
pick_package() {
    local ext=$1 arch=$2 commit=$3 found=() file
    for file in /src/dist/apptrol[_-]*."$ext"; do
        [[ -f $file && ! -L $file && $file == *"$arch"* && $file == *"snapshot.$commit"* ]] && found+=("$file")
    done
    if ((${#found[@]} != 1)); then
        printf 'FAIL: need exactly one %s for %s from commit %s in dist/; found %d. Run make snapshot.\n' \
            "$ext" "$arch" "$commit" "${#found[@]}" >&2
        return 1
    fi
    printf '%s\n' "${found[0]}"
}
install_package() {
    local commit=$1 pkg name
    # A leftover from an earlier step would make the package look like it worked.
    [[ ! -e /usr/bin/apptrol && ! -e /usr/lib/systemd/user/apptrol.service ]] || {
        echo 'FAIL: apptrol is already installed before the package' >&2
        return 1
    }
    # shellcheck disable=SC1091
    . /etc/os-release
    case $ID in
        ubuntu)
            pkg=$(pick_package deb "$(dpkg --print-architecture)" "$commit")
            # Copied so apt's unprivileged download user can read it. No network
            # and no repository lists exist: nothing but this file is installed.
            install -m 644 "$pkg" /tmp/apptrol.deb
            echo "package=${pkg##*/}"
            DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends /tmp/apptrol.deb
            owner() { dpkg -S "$1" | cut -d: -f1; }
            packaged_version() { dpkg-query -W -f='${Version}' apptrol; }
            ;;
        fedora)
            pkg=$(pick_package rpm "$(uname -m)" "$commit")
            install -m 644 "$pkg" /tmp/apptrol.rpm
            echo "package=${pkg##*/}"
            dnf install -y --disablerepo='*' --setopt=install_weak_deps=False /tmp/apptrol.rpm
            owner() { rpm -qf --qf '%{NAME}\n' "$1"; }
            packaged_version() { rpm -q --qf '%{VERSION}' apptrol; }
            ;;
        *)
            echo "FAIL: no package manager configured for $ID" >&2
            return 1
            ;;
    esac
    for name in /usr/bin/apptrol /usr/lib/systemd/user/apptrol.service; do
        [[ $(owner "$name") == apptrol ]] || { echo "FAIL: $name does not belong to the package" >&2; return 1; }
    done
    # The binary that runs is the one the package carries, not another build.
    [[ v$(packaged_version) == "$(/usr/bin/apptrol --version | cut -d ' ' -f2)" ]] || {
        printf 'FAIL: package version %s differs from %s\n' "$(packaged_version)" "$(/usr/bin/apptrol --version)" >&2
        return 1
    }
    echo "PASS: apptrol installed by the package manager from $(packaged_version)"
}
case $mode in
    binary)
        install -m 755 /src/bin/apptrol /usr/bin/apptrol
        install -m 644 /src/packaging/systemd/apptrol.service /usr/lib/systemd/user/apptrol.service
        ;;
    package)
        install_package "${2:?package mode needs the source commit}"
        ;;
    *)
        echo "FAIL: unknown mode $mode" >&2
        exit 2
        ;;
esac
install -d -o tester -g tester /home/tester/.config/apptrol
install -o tester -g tester -m 644 /src/test/container/valid.toml /home/tester/.config/apptrol/config.toml
version=$(/usr/bin/apptrol --version | cut -d ' ' -f2)
wait_status() {
    local expected="$version · $1" actual
    for ((i=0; i<75; i++)); do
        actual=$(userctl show --property=StatusText --value apptrol)
        if [[ $actual == "$expected" ]]; then
            echo "PASS: StatusText=$actual"
            return
        fi
        sleep 0.2
    done
    printf 'FAIL: expected StatusText=%s; got %s\n' "$expected" "$actual" >&2
    return 1
}
userctl daemon-reload
userctl start apptrol
wait_status 'controller not found'
# Invalid reloads retain valid settings; a restart exercises the waiting state.
printf '[layouts.default]\nknob3 = "missing-app"\n' > /home/tester/.config/apptrol/config.toml
userctl restart apptrol
wait_status 'waiting for a valid configuration'
cat /src/test/container/valid.toml > /home/tester/.config/apptrol/config.toml
wait_status 'controller not found'
userctl stop apptrol
