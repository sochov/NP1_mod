#!/bin/sh
# Notify exactly one already-running process at its official AppManager barrier.
# Never stop, start, restart, kill, or reset a stock process.
set -eu
umask 077
MODE=${1:-}
ID=${2:-}
test "$#" = 2
case "$MODE" in preflight|notify) ;; *) exit 64;; esac
case "$ID" in
 drive-rec) UNIT=app-@DriveRec.service;;
 image-recog) UNIT=app-@ImageRecog.service;;
 system) UNIT=app-@System.service;;
 remote-monitoring) UNIT=app-@RemoteMonitoring.service;;
 virtual-passenger) UNIT=app-@VirtualPassenger.service;;
 *) exit 64;;
esac
test "$(id -u)" = 0
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$STAGE"
sha256sum -c SHA256SUMS >/dev/null
test -d /run/np1-control-native && test ! -L /run/np1-control-native
exec 9>/run/np1-control-native/control.lock
flock -n 9 || exit 75
BASE=$(mktemp -d /run/np1-stock-start-trial.XXXXXX)
HELPER=$BASE/helper
cp bin/ncc-stock-start-helper "$HELPER"
chmod 0700 "$HELPER"
sha256sum "$HELPER" > "$BASE/helper-sha256.txt"
BASELINE=0
capture(){ /bin/sh tools/native-web-baseline.sh; }
finish(){
 result=$?
 trap - 0
 trap '' 1 2 15
 set +e
 capture > "$BASE/stock-after.txt" || result=1
 if test "$BASELINE" = 1; then cmp -s "$BASE/stock-before.txt" "$BASE/stock-after.txt" || result=1; fi
 if test -f "$BASE/Setting-before.dat"; then
  cp /pio_data/SYSTEM/SETTING/Setting.DAT "$BASE/Setting-after.dat" || result=1
  cmp -s "$BASE/Setting-before.dat" "$BASE/Setting-after.dat" || result=1
 fi
 rm -f "$HELPER"
 printf 'Private stock start trial records: %s\nNCC_EXIT_STATUS=%s\n' "$BASE" "$result"
 exit "$result"
}
trap finish 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 143' 15
capture > "$BASE/stock-before.txt"
BASELINE=1
if test -f /pio_data/SYSTEM/SETTING/Setting.DAT && test ! -L /pio_data/SYSTEM/SETTING/Setting.DAT; then cp /pio_data/SYSTEM/SETTING/Setting.DAT "$BASE/Setting-before.dat"; fi
PID=$(systemctl show "$UNIT" -p MainPID --value)
INV=$(systemctl show "$UNIT" -p InvocationID --value)
test "$(systemctl show "$UNIT" -p ActiveState --value)" = active
LD_LIBRARY_PATH=/usr/local/lib "$HELPER" --preflight "$ID" "$PID" "$INV" > "$BASE/preflight.txt" 2> "$BASE/preflight-error.txt"
if test "$MODE" = notify; then
 test "$(systemctl show "$UNIT" -p MainPID --value)" = "$PID"
 test "$(systemctl show "$UNIT" -p InvocationID --value)" = "$INV"
 LD_LIBRARY_PATH=/usr/local/lib NCC_STOCK_START_NOTIFY=1 "$HELPER" --notify "$ID" "$PID" "$INV" > "$BASE/notify.txt" 2> "$BASE/notify-error.txt"
 grep -Fx 'start-barrier=notified; owner=matched; threads=multiple; cleanup=matched' "$BASE/notify.txt" >/dev/null
 sleep 3
 test "$(systemctl show "$UNIT" -p MainPID --value)" = "$PID"
 test "$(systemctl show "$UNIT" -p InvocationID --value)" = "$INV"
 test "$(systemctl show "$UNIT" -p ActiveState --value)" = active
fi
