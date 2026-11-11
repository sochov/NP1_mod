#!/bin/sh
# Retain credentials/state/recovery for the owner. No recursive deletion and no vendor reset.
set -eu
BASE=/overlay/owner-apps/np1-control
test "$(id -u)" = 0
systemctl disable --now np1-control.service np1-control-broker.service np1-control-tailscale.service np1-control-firewall.service np1-control-led.service || true
rm -f "$BASE/enabled"
rm -f "$BASE/vpn-enabled"
python3 "$(dirname "$0")/postboot.py" remove-vpn
python3 "$(dirname "$0")/postboot.py" remove-led
python3 "$(dirname "$0")/postboot.py" remove
sh -n /etc/init.post_boot.sh
rm -f /etc/systemd/system/np1-control.service /etc/systemd/system/np1-control-broker.service /etc/systemd/system/np1-control-tailscale.service /etc/systemd/system/np1-control-firewall.service /etc/systemd/system/np1-control-led.service
systemctl daemon-reload
systemctl reset-failed np1-control.service np1-control-broker.service || true
if test -f "$BASE/recovery/account-created"; then
    userdel ncc
    rm -f "$BASE/recovery/account-created"
fi
echo "Uninstalled; owner state and backups retained at $BASE. Purely stock files were not restored wholesale."
