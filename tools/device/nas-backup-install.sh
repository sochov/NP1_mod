#!/bin/sh
# Install only the owner's NAS state directory and updated broker sandbox.
# Keep all running/stopped services unchanged; activate on the Web-only update.
set -eu
umask 077
BASE=/overlay/owner-apps/np1-control
UNIT=/etc/systemd/system/np1-control-broker.service
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
test "$(id -u)" = 0
cd "$STAGE"
sha256sum -c SHA256SUMS >/dev/null
for path in "$BASE" "$BASE/private" "$BASE/recovery"; do test -d "$path" && test ! -L "$path"; done
test -f "$UNIT" && test ! -L "$UNIT"
test ! -L "$BASE/private/nas"
BACKUP=$(mktemp -d "$BASE/recovery/nas-install-XXXXXXXX")
cp -p "$UNIT" "$BACKUP/broker.service"
capture(){ cat /proc/sys/kernel/random/boot_id; systemctl show 'app-@*.service' app-AppManager.service 'NEventWatcher*.service' qmmf-server.service np1-control.service np1-control-broker.service np1-control-native.service np1-control-ap.service np1-control-modem.service -p Id -p MainPID -p InvocationID -p ActiveState -p SubState; }
capture > "$BACKUP/before.txt"
COMPLETED=0
finish(){
 result=$?
 trap - 0 HUP INT TERM
 if test "$COMPLETED" != 1; then
  cp -p "$BACKUP/broker.service" "$UNIT"
  systemctl daemon-reload || result=1
 fi
 exit "$result"
}
trap finish 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
if test -e "$BASE/private/nas"; then
 test -d "$BASE/private/nas"
 test "$(stat -c '%u:%a' "$BASE/private/nas")" = 0:700
else mkdir -m 0700 "$BASE/private/nas"; fi
NCC_UID=$(id -u ncc)
sed "s/@NCC_UID@/$NCC_UID/g" systemd/np1-control-broker.service > "$UNIT.ncc-new"
chmod 0644 "$UNIT.ncc-new"
mv -f "$UNIT.ncc-new" "$UNIT"
systemctl daemon-reload
capture > "$BACKUP/after.txt"
cmp "$BACKUP/before.txt" "$BACKUP/after.txt"
COMPLETED=1
echo 'NAS private directory and broker sandbox installed; no service action performed'
