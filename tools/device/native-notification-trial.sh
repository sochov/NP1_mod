#!/bin/sh
# One reversible candidate at a time. Stable setting/Alexa workers stay running.
set -eu
umask 077
MODE=${1:-}
ID=${2:-}
NEXT=${3:-}
case "$ID" in front-car-start) INDEX=79;; signal-change) INDEX=80;; '') INDEX=0;; *) exit 64;; esac
case "$MODE:$#" in preflight:1) ;; roundtrip:3) test "$INDEX" != 0; case "$NEXT" in [0-2]) ;; *) exit 64;; esac;; *) echo 'Usage: native-notification-trial.sh preflight | roundtrip <fixed-id> <0..2>' >&2; exit 64;; esac
test "$(id -u)" = 0
test -d /run/np1-control-native && test ! -L /run/np1-control-native
exec 9>/run/np1-control-native/control.lock
flock -n 9 || { echo 'Another native control is active' >&2; exit 75; }
PERSISTENT=/overlay/owner-apps/np1-control/experimental/ncc-native-setting-notification-helper
test -f "$PERSISTENT" && test ! -L "$PERSISTENT"
BASE=$(mktemp -d /run/np1-native-notification-trial.XXXXXX)
HELPER=$BASE/helper
cp "$PERSISTENT" "$HELPER"
chmod 0700 "$HELPER"
sha256sum "$HELPER" > "$BASE/helper-sha256.txt"
capture_core(){ for unit in app-@Communication app-@DriveRec app-@ImageRecog app-@Voice qmmf-server NEventWatcherCom app-@Setting app-@Update; do printf 'Unit=%s.service\n' "$unit"; systemctl show "$unit.service" -p MainPID -p ActiveState -p SubState; done; }
capture_stock(){ capture_core; for unit in app-@Navigation app-@VirtualPassenger; do printf 'Unit=%s.service\n' "$unit"; systemctl show "$unit.service" -p MainPID -p ActiveState -p SubState; done; }
capture_saved(){ source=/pio_data/SYSTEM/SETTING/Setting.DAT; test -f "$source" && test ! -L "$source"; cp "$source" "$1"; chmod 0600 "$1"; test "$(wc -c < "$1")" = 704; }
value(){ awk -F= -v key="$INDEX" '$1 ~ /^[0-9]+$/ && ($1+0)==key && $2 ~ /^[0-2]$/ {print $2}' "$1"; }
saved(){ awk -F= -v key="$INDEX" '$1 ~ /^saved-[0-9]+$/ {name=$1;sub(/^saved-/,"",name);if((name+0)==key && $2 ~ /^[0-2]$/)print $2}' "$1"; }
invoke(){ LD_LIBRARY_PATH=/usr/local/lib NCC_NATIVE_TRIAL=1 "$HELPER" "$@"; }
BEFORE=
ATTEMPTED=0
BASELINE=0
finish(){
 result=$?
 trap - 0
 trap '' 1 2 15
 set +e
 "$HELPER" --preflight > "$BASE/current-values.txt" 2> "$BASE/current-error.txt"
 current_rc=$?
 if test "$ATTEMPTED" = 1 && test -n "$BEFORE"; then
  CURRENT=
  if test "$current_rc" = 0; then CURRENT=$(value "$BASE/current-values.txt"); fi
  if test "$CURRENT" = "$NEXT" && test "$CURRENT" != "$BEFORE"; then
   # A partial failure can precede the stock 3-second persistence debounce.
   sleep 4
   LD_LIBRARY_PATH=/usr/local/lib NCC_NATIVE_TRIAL=1 NCC_NOTIFICATION_RESTORE=1 "$HELPER" --apply "$ID" "$CURRENT" "$BEFORE" > "$BASE/restore.txt" 2> "$BASE/restore-error.txt" || result=1
  elif test "$CURRENT" != "$BEFORE"; then echo 'Value changed or unknown; no blind restore' >&2; result=1; fi
  sleep 4
 fi
 "$HELPER" --preflight > "$BASE/after-values.txt" 2> "$BASE/after-error.txt" || result=1
 capture_saved "$BASE/saved-after.dat" || result=1
 capture_stock > "$BASE/stock-after.txt" || result=1
 if test "$BASELINE" = 1; then
  cmp -s "$BASE/before-values.txt" "$BASE/after-values.txt" || { echo 'Stored native values/global semaphores differ' >&2; result=1; }
  cmp -s "$BASE/saved-before.dat" "$BASE/saved-after.dat" || { echo 'Complete persistent backup differs after restore' >&2; result=1; }
  cmp -s "$BASE/stock-before.txt" "$BASE/stock-after.txt" || { echo 'Stock/consumer PID/state changed' >&2; result=1; }
 fi
 rm -f "$HELPER"
 printf 'Private notification native trial records: %s\n' "$BASE"
 exit "$result"
}
trap finish 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 143' 15
capture_core > "$BASE/core-before.txt"
test "$(grep -c '^ActiveState=active$' "$BASE/core-before.txt")" = 8 && test "$(grep -c '^MainPID=[1-9][0-9]*$' "$BASE/core-before.txt")" = 8 || exit 78
capture_stock > "$BASE/stock-before.txt"
test "$(grep -c '^ActiveState=active$' "$BASE/stock-before.txt")" = 10 && test "$(grep -c '^MainPID=[1-9][0-9]*$' "$BASE/stock-before.txt")" = 10 || { echo 'Core/Navigation/VirtualPassenger baseline is not active' >&2; exit 78; }
"$HELPER" --preflight > "$BASE/before-values.txt" 2> "$BASE/before-error.txt"
capture_saved "$BASE/saved-before.dat"
BASELINE=1
if test "$MODE" = preflight; then echo 'Notification preflight only; vendor not loaded'; exit 0; fi
BEFORE=$(value "$BASE/before-values.txt")
test -n "$BEFORE" && test "$BEFORE" != "$NEXT" && test "$(saved "$BASE/before-values.txt")" = "$BEFORE" || exit 75
ATTEMPTED=1
invoke --apply "$ID" "$BEFORE" "$NEXT" > "$BASE/apply.txt" 2> "$BASE/apply-error.txt"
sleep 4
"$HELPER" --preflight > "$BASE/applied-values.txt" 2> "$BASE/applied-error.txt"
test "$(value "$BASE/applied-values.txt")" = "$NEXT" && test "$(saved "$BASE/applied-values.txt")" = "$NEXT"
capture_saved "$BASE/saved-applied.dat"
