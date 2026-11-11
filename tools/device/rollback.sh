#!/bin/sh
# Restore only the previous NCC binary/units/config. First installation rolls back by uninstalling.
set -eu
BASE=/overlay/owner-apps/np1-control
BACKUP=$(cat "$BASE/recovery/latest")
case "$BACKUP" in "$BASE"/recovery/[0-9]*T[0-9]*Z) ;; *) echo 'Invalid recovery path' >&2; exit 1;; esac
test -d "$BACKUP"
if test ! -f "$BACKUP/ncc"; then exec sh "$(dirname "$0")/uninstall.sh"; fi
systemctl stop np1-control.service np1-control-broker.service
systemctl stop np1-control-firewall.service || true
cp -p "$BACKUP/ncc" "$BASE/bin/ncc"
for unit in np1-control.service np1-control-broker.service; do cp -p "$BACKUP/$unit" "/etc/systemd/system/$unit"; done
if test -f "$BACKUP/np1-control-firewall.service"; then
    cp -p "$BACKUP/np1-control-firewall.service" /etc/systemd/system/np1-control-firewall.service
    cp -p "$BACKUP/firewall.sh" "$BASE/tools/firewall.sh"
else
    rm -f /etc/systemd/system/np1-control-firewall.service
fi
if test -f "$BACKUP/config.json"; then cp -p "$BACKUP/config.json" "$BASE/config/config.json"; fi
systemctl daemon-reload
systemctl start np1-control-broker.service np1-control.service
sh "$(dirname "$0")/verify.sh"
