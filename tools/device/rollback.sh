#!/bin/sh
# Restore only the previous NCC binary/units/config. First installation rolls back by uninstalling.
set -eu
BASE=/overlay/owner-apps/np1-control
BACKUP=$(cat "$BASE/recovery/latest")
case "$BACKUP" in "$BASE"/recovery/[0-9]*T[0-9]*Z) ;; *) echo 'Invalid recovery path' >&2; exit 1;; esac
test -d "$BACKUP"
if test -f "$BACKUP/ota-policy-initialized" && test -f "$BASE/private/ota-policy/ledger.json"; then
    sh "$(dirname "$0")/ota-policy.sh" rollback
fi
if test ! -f "$BACKUP/ncc"; then exec sh "$(dirname "$0")/uninstall.sh"; fi
systemctl stop np1-control.service np1-control-broker.service
NCC_LED_BEFORE=$(systemctl is-active np1-control-led.service || true)
systemctl stop np1-control-led.service || true
systemctl stop np1-control-modem.service || true
systemctl stop np1-control-native.service || true
NCC_AP_BEFORE=$(systemctl is-active np1-control-ap.service || true)
systemctl stop np1-control-ap.service || true
systemctl stop np1-control-firewall.service || true
cp -p "$BACKUP/ncc" "$BASE/bin/ncc"
for unit in np1-control.service np1-control-broker.service; do cp -p "$BACKUP/$unit" "/etc/systemd/system/$unit"; done
if test -f "$BACKUP/np1-control-firewall.service"; then
    cp -p "$BACKUP/np1-control-firewall.service" /etc/systemd/system/np1-control-firewall.service
    cp -p "$BACKUP/firewall.sh" "$BASE/tools/firewall.sh"
else
    rm -f /etc/systemd/system/np1-control-firewall.service
fi
if test -f "$BACKUP/np1-control-led.service"; then
    cp -p "$BACKUP/np1-control-led.service" /etc/systemd/system/np1-control-led.service
else
    python3 "$(dirname "$0")/postboot.py" remove-led
    rm -f /etc/systemd/system/np1-control-led.service
fi
if test -f "$BACKUP/config.json"; then cp -p "$BACKUP/config.json" "$BASE/config/config.json"; fi
if test -f "$BACKUP/np1-control-modem.service"; then
    cp -p "$BACKUP/np1-control-modem.service" /etc/systemd/system/np1-control-modem.service
else
    python3 "$(dirname "$0")/postboot.py" remove-modem
    rm -f /etc/systemd/system/np1-control-modem.service
fi
systemctl daemon-reload
if test -f "$BACKUP/np1-control-ap.service"; then
    cp -p "$BACKUP/np1-control-ap.service" /etc/systemd/system/np1-control-ap.service
    systemctl daemon-reload
    if test "$NCC_AP_BEFORE" = active; then systemctl start np1-control-ap.service; fi
else
    python3 "$(dirname "$0")/postboot.py" remove-ap
    rm -f /etc/systemd/system/np1-control-ap.service
    systemctl daemon-reload
fi
if test -f "$BACKUP/np1-control-native.service"; then
    cp -p "$BACKUP/np1-control-native.service" /etc/systemd/system/np1-control-native.service
    systemctl daemon-reload
    systemctl start np1-control-native.service
else
    python3 "$(dirname "$0")/postboot.py" remove-native
    rm -f /etc/systemd/system/np1-control-native.service
    systemctl daemon-reload
fi
if test -f "$BACKUP/np1-control-modem.service"; then systemctl start np1-control-modem.service; fi
if test -f "$BACKUP/np1-control-led.service" && test "$NCC_LED_BEFORE" = active; then systemctl start np1-control-led.service; fi
systemctl start np1-control-broker.service np1-control.service
sh "$(dirname "$0")/verify.sh"
