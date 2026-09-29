#!/bin/sh
# Runs after the .deb / .rpm package is installed or upgraded.
# The service is per user and is deliberately not enabled automatically.
cat <<'MSG'

Apptrol is installed. To start it for your user (run as your user, not root):

    systemctl --user daemon-reload
    systemctl --user enable --now apptrol

After an upgrade, restart it instead:  systemctl --user restart apptrol

Configuration: ~/.config/apptrol/config.toml (created from the example on first start)
Helpful:       apptrol list   (names to match)   apptrol test   (check the controller)

MSG
exit 0
