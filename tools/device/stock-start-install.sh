#!/bin/sh
# Install only our fixed start-notification helper and owned broker unit.
# No service is started/stopped/restarted; preserve the user's current states.
set -eu
umask 077
BASE=/overlay/owner-apps/np1-control
UNIT=/etc/systemd/system/np1-control-broker.service
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
test "$(id -u)" = 0
cd "$STAGE"
sha256sum -c SHA256SUMS >/dev/null
test -d "$BASE/recovery" && test ! -L "$BASE/recovery"
test -d "$BASE/experimental" && test ! -L "$BASE/experimental"
test -f "$UNIT" && test ! -L "$UNIT"
test -f experimental/ncc-stock-start-helper && test ! -L experimental/ncc-stock-start-helper
test ! -L "$BASE/experimental/ncc-stock-start-helper"
BACKUP=$(mktemp -d "$BASE/recovery/stock-start-install-XXXXXXXX")
cp -p "$UNIT" "$BACKUP/broker.service"
if test -f "$BASE/experimental/ncc-stock-start-helper"; then
 cp -p "$BASE/experimental/ncc-stock-start-helper" "$BACKUP/helper"
else touch "$BACKUP/helper-absent"; fi
capture(){ cat /proc/sys/kernel/random/boot_id; systemctl show 'app-@*.service' app-AppManager.service 'NEventWatcher*.service' qmmf-server.service np1-control.service np1-control-broker.service np1-control-native.service np1-control-ap.service np1-control-modem.service -p Id -p MainPID -p InvocationID -p ActiveState -p SubState; }
capture > "$BACKUP/before.txt"
COMPLETED=0
finish(){
 result=$?
 trap - 0 HUP INT TERM
 if test "$COMPLETED" != 1; then
  cp -p "$BACKUP/broker.service" "$UNIT"
  if test -f "$BACKUP/helper"; then cp -p "$BACKUP/helper" "$BASE/experimental/ncc-stock-start-helper"; else rm -f "$BASE/experimental/ncc-stock-start-helper"; fi
  systemctl daemon-reload || result=1
 fi
 exit "$result"
}
trap finish 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
cp experimental/ncc-stock-start-helper "$BASE/experimental/ncc-stock-start-helper.new"
chmod 0600 "$BASE/experimental/ncc-stock-start-helper.new"
mv -f "$BASE/experimental/ncc-stock-start-helper.new" "$BASE/experimental/ncc-stock-start-helper"
NCC_UID=$(id -u ncc)
sed "s/@NCC_UID@/$NCC_UID/g" systemd/np1-control-broker.service > "$UNIT.ncc-new"
chmod 0644 "$UNIT.ncc-new"
mv -f "$UNIT.ncc-new" "$UNIT"
systemctl daemon-reload
capture > "$BACKUP/after.txt"
cmp "$BACKUP/before.txt" "$BACKUP/after.txt"
COMPLETED=1
echo 'Start helper installed; no service action performed'
