#!/bin/sh
# Retain credentials/state/recovery for the owner. No recursive deletion and no vendor reset.
set -eu
BASE=/overlay/owner-apps/np1-control
test "$(id -u)" = 0
# Stop the uplink switcher first; it leaves the stock STA mode as it is (the owner can change it in the stock settings).
systemctl stop np1-control-uplink.service || true
python3 "$(dirname "$0")/postboot.py" remove-uplink
rm -f /etc/systemd/system/np1-control-uplink.service
# Stop the guard and remove its network layer before the flag, so neither is rebuilt.
systemctl stop np1-control-ota-guard.service || true
sh "$(dirname "$0")/ota-policy.sh" guard-release || true
python3 "$(dirname "$0")/postboot.py" remove-ota-guard
rm -f /etc/systemd/system/np1-control-ota-guard.service
if test -f "$BASE/private/ota-policy/ledger.json"; then sh "$(dirname "$0")/ota-policy.sh" uninstall; fi
systemctl stop np1-control-apn.service || true
systemctl stop np1-control-plate.service || true
python3 "$(dirname "$0")/postboot.py" remove-plate
rm -f /etc/systemd/system/np1-control-plate.service
python3 "$(dirname "$0")/postboot.py" remove-apn
rm -f /etc/systemd/system/np1-control-apn.service
systemctl disable --now np1-control.service np1-control-broker.service np1-control-tailscale.service np1-control-firewall.service np1-control-led.service np1-control-modem.service || true
systemctl stop np1-control-native.service || true
systemctl stop np1-control-ap.service || true
python3 "$(dirname "$0")/postboot.py" remove-ap
rm -f /etc/systemd/system/np1-control-ap.service
python3 "$(dirname "$0")/postboot.py" remove-native
rm -f /etc/systemd/system/np1-control-native.service
rm -f "$BASE/enabled"
rm -f "$BASE/vpn-enabled"
python3 "$(dirname "$0")/postboot.py" remove-vpn
python3 "$(dirname "$0")/postboot.py" remove-led
python3 "$(dirname "$0")/postboot.py" remove-modem
python3 "$(dirname "$0")/postboot.py" remove
sh -n /etc/init.post_boot.sh
rm -f /etc/systemd/system/np1-control.service /etc/systemd/system/np1-control-broker.service /etc/systemd/system/np1-control-tailscale.service /etc/systemd/system/np1-control-firewall.service /etc/systemd/system/np1-control-led.service /etc/systemd/system/np1-control-modem.service
systemctl daemon-reload
systemctl reset-failed np1-control.service np1-control-broker.service || true
if test -f "$BASE/recovery/account-created"; then
    userdel ncc
    rm -f "$BASE/recovery/account-created"
fi
echo "Uninstalled; owner state and backups retained at $BASE. Purely stock files were not restored wholesale."
