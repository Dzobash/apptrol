#!/bin/bash
# Runs only inside the container: prove the assumptions before the Status test
# relies on them (QA-16, ADR 0034 point 9).
set -euo pipefail
trap 'echo "FAIL: container proof at line $LINENO" >&2' ERR
deadline=$((SECONDS+60))
# Podman returning a container ID does not mean systemd has opened its bus yet.
while ((SECONDS < deadline)); do
    if systemctl show --property=Version >/dev/null 2>&1; then break; fi
    sleep 1
done
systemctl show --property=Version >/dev/null
test "$(cat /proc/1/comm)" = systemd
/bin/sh /src/test/container/limits.sh
for unit in systemd-networkd-wait-online.service NetworkManager-wait-online.service; do
    test "$(systemctl is-enabled "$unit" || :)" = masked
done
while ((SECONDS < deadline)); do
    if runuser -u tester -- env XDG_RUNTIME_DIR=/run/user/1500 systemctl --user show-environment >/dev/null 2>&1; then
        echo 'PASS: systemd PID 1, ordinary user manager, network isolation and every limit'
        exit 0
    fi
    sleep 1
done
echo 'FAIL: ordinary user manager did not start within 60 seconds' >&2
systemctl status user@1500.service --no-pager || :
exit 1
