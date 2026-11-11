#!/bin/sh
# Fixed pure-Setting owner requests. Never stop a service or write SDK files.
set -eu
umask 077
MODE=${1:-}
ID=${2:-}
NEXT=${3:-}
case "$ID" in mute) INDEX=1; MAX=1;; volume-correction) INDEX=22; MAX=2;; '') INDEX=0; MAX=0;; *) exit 64;; esac
case "$MODE:$#" in preflight:1|get:1) ;; roundtrip:3) test "$INDEX" != 0; case "$NEXT" in [0-2]) test "$NEXT" -le "$MAX";; *) exit 64;; esac;; *) exit 64;; esac
test "$(id -u)" = 0
test -d /run/np1-control-native && test ! -L /run/np1-control-native
exec 9>/run/np1-control-native/control.lock
flock -n 9 || { echo 'Another native control is active' >&2; exit 75; }
PERSISTENT=/overlay/owner-apps/np1-control/experimental/ncc-native-audio-helper
test -f "$PERSISTENT" && test ! -L "$PERSISTENT"
BASE=$(mktemp -d /run/np1-native-audio-trial.XXXXXX)
HELPER=$BASE/helper
cp "$PERSISTENT" "$HELPER"
chmod 0700 "$HELPER"
sha256sum "$HELPER" > "$BASE/helper-sha256.txt"
UNITS='app-@Communication.service app-@DriveRec.service app-@ImageRecog.service app-@Voice.service qmmf-server.service NEventWatcherCom.service app-@Setting.service app-@Update.service app-@Navigation.service app-@VirtualPassenger.service'
capture_stock(){ cat /proc/sys/kernel/random/boot_id; systemctl show $UNITS --no-pager -p Id -p MainPID -p ActiveState -p SubState -p InvocationID; }
capture_saved(){ source=/pio_data/SYSTEM/SETTING/Setting.DAT; test -f "$source" && test ! -L "$source"; cp "$source" "$1"; chmod 0600 "$1"; test "$(wc -c < "$1")" = 704; }
value(){ awk -F= -v key="$INDEX" '$1 ~ /^[0-9]+$/ && ($1+0)==key && $2 ~ /^[0-2]$/ {print $2}' "$1"; }
SETTING_PID=$(systemctl show app-@Setting.service -p MainPID --value)
SETTING_INV=$(systemctl show app-@Setting.service -p InvocationID --value)
test "$SETTING_PID" -gt 0 && test -n "$SETTING_INV"
invoke(){
 test "$(systemctl show app-@Setting.service -p MainPID --value)" = "$SETTING_PID"
 test "$(systemctl show app-@Setting.service -p InvocationID --value)" = "$SETTING_INV"
 LD_LIBRARY_PATH=/usr/local/lib NCC_NATIVE_AUDIO_TRIAL=1 "$HELPER" "$@"
}
get_value(){ case "$INDEX" in 1) key=audio-mute;; 22) key=audio-correction;; *) return 1;; esac; awk -F= -v key="$key" '$1==key && $2 ~ /^[0-2]$/ {print $2}' "$1"; }
BEFORE=
ATTEMPTED=0
BASELINE=0
finish(){
 result=$?
 trap - 0
 trap '' 1 2 15
 set +e
 if test "$ATTEMPTED" = 1 && test -n "$BEFORE"; then
  CURRENT=
  if invoke --get "$SETTING_PID" > "$BASE/current.txt" 2> "$BASE/current-error.txt"; then CURRENT=$(get_value "$BASE/current.txt"); fi
  if test "$CURRENT" = "$NEXT" && test "$CURRENT" != "$BEFORE"; then
   sleep 4
   invoke --apply "$SETTING_PID" "$ID" "$CURRENT" "$BEFORE" > "$BASE/restore.txt" 2> "$BASE/restore-error.txt" || result=1
  elif test "$CURRENT" != "$BEFORE"; then echo 'Unknown or competing owner state; no blind restore' >&2; result=1; fi
  sleep 4
 fi
 "$HELPER" --preflight > "$BASE/after-values.txt" 2> "$BASE/after-error.txt" || result=1
 capture_saved "$BASE/saved-after.dat" || result=1
 capture_stock > "$BASE/stock-after.txt" || result=1
 if test "$BASELINE" = 1; then
  cmp -s "$BASE/before-values.txt" "$BASE/after-values.txt" || result=1
  cmp -s "$BASE/saved-before.dat" "$BASE/saved-after.dat" || result=1
  cmp -s "$BASE/stock-before.txt" "$BASE/stock-after.txt" || result=1
 fi
 rm -f "$HELPER"
 printf 'Private audio native trial records: %s\n' "$BASE"
 exit "$result"
}
trap finish 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 143' 15
capture_stock > "$BASE/stock-before.txt"
test "$(grep -c '^ActiveState=active$' "$BASE/stock-before.txt")" = 10 && test "$(grep -c '^MainPID=[1-9][0-9]*$' "$BASE/stock-before.txt")" = 10 || exit 78
"$HELPER" --preflight > "$BASE/before-values.txt" 2> "$BASE/before-error.txt"
capture_saved "$BASE/saved-before.dat"
BASELINE=1
if test "$MODE" = preflight; then echo 'Audio preflight only; vendor not loaded'; exit 0; fi
invoke --get "$SETTING_PID" > "$BASE/get.txt" 2> "$BASE/get-error.txt"
grep -q '^cleanup=matched; own-receiver-removed$' "$BASE/get.txt"
if test "$MODE" = get; then echo 'Audio correlated owner GET completed; no setting change'; exit 0; fi
BEFORE=$(value "$BASE/before-values.txt")
test -n "$BEFORE" && test "$BEFORE" != "$NEXT" && test "$(get_value "$BASE/get.txt")" = "$BEFORE" || exit 75
ATTEMPTED=1
invoke --apply "$SETTING_PID" "$ID" "$BEFORE" "$NEXT" > "$BASE/apply.txt" 2> "$BASE/apply-error.txt"
sleep 4
"$HELPER" --preflight > "$BASE/applied-values.txt" 2> "$BASE/applied-error.txt"
test "$(value "$BASE/applied-values.txt")" = "$NEXT"
capture_saved "$BASE/saved-applied.dat"
