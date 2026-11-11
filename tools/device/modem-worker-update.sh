#!/bin/sh
# Replace only our read-only modem observer; preserve all stock/network workers.
set -eu
umask 077
BASE=/overlay/owner-apps/np1-control
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
test "$(id -u)" = 0
test -f "$BASE/bin/ncc" && test ! -L "$BASE/bin/ncc"
test -d "$BASE/recovery" && test ! -L "$BASE/recovery"
test ! -e /run/np1-control-apn/observer-paused.json
# Do not race an APN transaction that owns observer stop/resume.
if test -d "$BASE/private/apn"; then
 test ! -L "$BASE/private/apn/config.lock"
 if test -e "$BASE/private/apn/config.lock"; then
  test -f "$BASE/private/apn/config.lock"
  test "$(stat -c '%u:%a:%h' "$BASE/private/apn/config.lock")" = 0:600:1
 fi
 exec 8>"$BASE/private/apn/config.lock"
 chmod 0600 "$BASE/private/apn/config.lock"
 flock -n 8 || { echo 'APN settings are in use; modem update refused' >&2; exit 75; }
fi
systemctl is-active --quiet np1-control-modem.service
cd "$STAGE"
sha256sum -c SHA256SUMS >/dev/null
test -f bin/ncc && test ! -L bin/ncc
chmod 0755 bin/ncc
bin/ncc --mode self-test
BACKUP=$(mktemp -d "$BASE/recovery/modem-worker-XXXXXXXX")
chmod 0700 "$BACKUP"
cp -p "$BASE/bin/ncc" "$BACKUP/ncc"
UNITS='app-@DriveRec.service app-@ImageRecog.service app-@Voice.service app-@Navigation.service app-@Communication.service app-@Setting.service app-@Update.service app-@RemoteMonitoring.service app-@VirtualPassenger.service app-@InfoMtgEngine.service app-@SitAwrEng.service app-@Opening.service app-@Misc.service app-@System.service app-AppManager.service qmmf-server.service NEventWatcherAV.service NEventWatcherCom.service NEventWatcherITS.service NEventWatcherSys.service QCMAP_ConnectionManagerd.service loc_launcher.service location_hal_daemon.service pulseaudio.service NPSystemMonitor.service socketserver.service np1-control.service np1-control-broker.service np1-control-ap.service np1-control-native.service np1-control-apn.service np1-control-led.service'
capture(){ cat /proc/sys/kernel/random/boot_id; systemctl show $UNITS --no-pager -p Id -p MainPID -p InvocationID -p ActiveState -p SubState; }
capture > "$BACKUP/preserved-before.txt"
COMPLETED=0
CHANGED=0
finish(){
 result=$?
 trap - 0 HUP INT TERM
 if test "$COMPLETED" != 1 && test "$CHANGED" = 1; then
  cp -p "$BACKUP/ncc" "$BASE/bin/ncc.modem-rollback"
  mv -f "$BASE/bin/ncc.modem-rollback" "$BASE/bin/ncc"
  systemctl restart np1-control-modem.service || result=1
  echo 'Modem observer update unverified; previous owner binary restored' >&2
 fi
 exit "$result"
}
trap finish 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
cp bin/ncc "$BASE/bin/ncc.modem-new"
chmod 0755 "$BASE/bin/ncc.modem-new"
CHANGED=1
mv -f "$BASE/bin/ncc.modem-new" "$BASE/bin/ncc"
systemctl restart np1-control-modem.service
systemctl is-active --quiet np1-control-modem.service
# Fixed read-only mode waits for a new measured signal after this invocation.
/run/np1-control-modem/ncc --mode profiles-observer-check
capture > "$BACKUP/preserved-after.txt"
cmp "$BACKUP/preserved-before.txt" "$BACKUP/preserved-after.txt"
COMPLETED=1
echo 'Modem observer updated; fresh measured signal and other worker identities verified'
