#!/bin/sh
# Runs after the .deb / .rpm package is installed or upgraded.
# The service is per user and is deliberately not enabled automatically.
cat <<'MSG'

Apptrol is installed. To start it for your user (run as your user, not root):

    systemctl --user daemon-reload
    systemctl --user enable --now apptrol

Configuration: ~/.config/apptrol/config.toml
Example:       /usr/share/doc/apptrol/examples/config.toml

MSG
exit 0
