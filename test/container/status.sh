#!/bin/bash
# Exercise the shipped unit and reload path, not a replacement service (SVC-13,
# QA-16, ADR 0034). No controller or sound server exists in these containers.
set -euo pipefail
userctl() {
    runuser -u tester -- env XDG_RUNTIME_DIR=/run/user/1500 systemctl --user "$@"
}
diagnose() {
    journalctl --no-pager -u user@1500.service || :
    runuser -u tester -- env XDG_RUNTIME_DIR=/run/user/1500 journalctl --user --no-pager -u apptrol || :
}
trap diagnose ERR
install -m 755 /src/bin/apptrol /usr/bin/apptrol
install -m 644 /src/packaging/systemd/apptrol.service /usr/lib/systemd/user/apptrol.service
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
