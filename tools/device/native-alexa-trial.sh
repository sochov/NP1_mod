#!/bin/sh
# Separate staged helper. Does not stop/restart the proven native control worker.
set -eu
umask 077
MODE=${1:-}
NEXT=${2:-}
case "$MODE:$#" in preflight:1|get:1) ;; roundtrip:2) case "$NEXT" in 0|1) ;; *) exit 64;; esac;; *) echo 'Usage: native-alexa-trial.sh preflight|get|roundtrip <0|1>' >&2; exit 64;; esac
test "$(id -u)" = 0
PERSISTENT=/overlay/owner-apps/np1-control/experimental/ncc-native-alexa-helper
test -f "$PERSISTENT" && test ! -L "$PERSISTENT"
mkdir -p /run/np1-control-native
exec 8>/run/np1-control-native/control.lock
flock -n 8 || exit 75
exec 9>/run/np1-native-alexa-trial.lock
flock -n 9 || exit 75
BASE=$(mktemp -d /run/np1-native-alexa-trial.XXXXXX)
HELPER=$BASE/helper
cp "$PERSISTENT" "$HELPER"
chmod 0700 "$HELPER"
sha256sum "$HELPER" > "$BASE/helper-sha256.txt"
capture_stock(){ for unit in app-@Communication app-@DriveRec app-@ImageRecog app-@Voice qmmf-server NEventWatcherCom app-@Setting app-@Update; do printf 'Unit=%s.service\n' "$unit"; systemctl show "$unit.service" -p MainPID -p ActiveState -p SubState; done; }
invoke(){ LD_LIBRARY_PATH=/usr/local/lib NCC_NATIVE_ALEXA_TRIAL=1 "$HELPER" "$@"; }
wire(){ awk -F= '$1=="alexa-wire" && $2 ~ /^[012]$/ {print $2}' "$1"; }
capture_saved(){
 source=/pio_data/SYSTEM/SETTING/Setting.DAT
 test -f "$source" && test ! -L "$source"
 cp "$source" "$1"
 chmod 0600 "$1"
 test "$(wc -c < "$1")" = 704
}
BEFORE=
ATTEMPTED=0
BASELINE=0
finish(){
 result=$?
 trap - 0
 trap '' 1 2 15
 set +e
 if test "$ATTEMPTED" = 1 && test -n "$BEFORE"; then
  invoke --get "$VOICE_PID" > "$BASE/current.txt" 2> "$BASE/current-error.txt"
  current_rc=$?
  CURRENT=
  if test "$current_rc" = 0; then CURRENT=$(wire "$BASE/current.txt"); fi
  if test "$CURRENT" = "$NEXT" && test "$CURRENT" != "$BEFORE"; then
   invoke --apply "$VOICE_PID" "$CURRENT" "$BEFORE" > "$BASE/restore.txt" 2> "$BASE/restore-error.txt" || result=1
  elif test "$CURRENT" != "$BEFORE"; then echo 'Alexa state changed or unknown; no blind restoration' >&2; result=1; fi
  sleep 4
 fi
 "$HELPER" --preflight > "$BASE/after-values.txt" 2> "$BASE/after-error.txt" || result=1
 capture_saved "$BASE/saved-after.dat" || result=1
 capture_stock > "$BASE/stock-after.txt" || result=1
 if test "$BASELINE" = 1; then
  cmp -s "$BASE/before-values.txt" "$BASE/after-values.txt" || { echo 'Stored native values/global semaphores differ' >&2; result=1; }
  cmp -s "$BASE/stock-before.txt" "$BASE/stock-after.txt" || { echo 'Stock service PID/state changed' >&2; result=1; }
 fi
 rm -f "$HELPER"
 printf 'Private Alexa trial records: %s\n' "$BASE"
 exit "$result"
}
trap finish 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 143' 15
capture_stock > "$BASE/stock-before.txt"
test "$(grep -c '^ActiveState=active$' "$BASE/stock-before.txt")" = 8 && test "$(grep -c '^MainPID=[1-9][0-9]*$' "$BASE/stock-before.txt")" = 8 || exit 78
VOICE_PID=$(systemctl show app-@Voice.service -p MainPID --value)
case "$VOICE_PID" in ''|*[!0-9]*) exit 78;; esac
"$HELPER" --preflight > "$BASE/before-values.txt" 2> "$BASE/before-error.txt"
capture_saved "$BASE/saved-before.dat"
BASELINE=1
case "$MODE" in
 preflight) echo 'Alexa preflight only; vendor not loaded';;
 get) invoke --get "$VOICE_PID" > "$BASE/get.txt" 2> "$BASE/get-error.txt"; cat "$BASE/get.txt";;
 roundtrip)
  invoke --get "$VOICE_PID" > "$BASE/before-get.txt" 2> "$BASE/before-get-error.txt"
  BEFORE=$(wire "$BASE/before-get.txt")
  case "$BEFORE" in 0|1) ;; *) echo 'Alexa is fixed by stock linkage or unknown; no change attempted' >&2; exit 78;; esac
  test "$BEFORE" != "$NEXT" || exit 64
  ATTEMPTED=1
  invoke --apply "$VOICE_PID" "$BEFORE" "$NEXT" > "$BASE/apply.txt" 2> "$BASE/apply-error.txt"
  sleep 4
  invoke --get "$VOICE_PID" > "$BASE/applied-get.txt" 2> "$BASE/applied-get-error.txt"
  test "$(wire "$BASE/applied-get.txt")" = "$NEXT"
  capture_saved "$BASE/saved-applied.dat"
 ;;
esac
