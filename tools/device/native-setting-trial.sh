#!/bin/sh
# Owner-only controlled trial. Never run concurrently with another device mutation.
# The standalone helper is deliberately outside the normal Web/broker binary.
set -eu
umask 077
PERSISTENT_HELPER=/overlay/owner-apps/np1-control/experimental/ncc-native-setting-helper
MODE=${1:-}
ID=${2:-}
NEXT=${3:-}
case "$MODE:$#" in
 preflight:1|vendor-read:1) ;;
 roundtrip:3)
  case "$ID:$NEXT" in tailgating-detection:0|tailgating-detection:1|tailgating-sensitivity:0|tailgating-sensitivity:1|tailgating-sensitivity:2) ;;
   *) echo 'Only confirmed native enums for tailgating detection/sensitivity are accepted' >&2; exit 64;; esac ;;
 *) echo 'Usage: native-setting-trial.sh preflight | vendor-read | roundtrip <id> <next-native-value>' >&2; exit 64;;
esac
test "$(id -u)" = 0 && test -f "$PERSISTENT_HELPER" && test -r "$PERSISTENT_HELPER"
for tool in systemctl mktemp cmp grep awk flock sleep cp chmod sha256sum; do command -v "$tool" >/dev/null || exit 1; done
mkdir -p /run/np1-control-native
exec 9>/run/np1-control-native/control.lock
flock -n 9 || { echo 'Another native trial is active' >&2; exit 1; }
BASE=$(mktemp -d /run/np1-native-setting-trial.XXXXXX)
HELPER=$BASE/ncc-native-setting-helper
cp "$PERSISTENT_HELPER" "$HELPER"
chmod 0700 "$HELPER"
sha256sum "$HELPER" > "$BASE/helper-sha256.txt"
BEFORE=
ATTEMPTED=0
BASELINE=0
capture_stock(){
 rc=0
 for unit in app-@Communication app-@DriveRec app-@ImageRecog app-@Voice qmmf-server NEventWatcherCom app-@Setting; do
  printf 'Unit=%s.service\n' "$unit"
  systemctl show "$unit.service" -p MainPID -p ActiveState -p SubState || rc=1
 done
 return "$rc"
}
value(){
 case "$ID" in tailgating-detection) key=50;; tailgating-sensitivity) key=51;; *) return 1;; esac
 awk -F= -v key="$key" '$1==key && $2 ~ /^[012]$/ {print $2}' "$1"
}
finish(){
 result=$?
 trap - 0
 trap '' 1 2 15
 set +e
 # A failing vendor return might follow an already-applied setting. Re-read before restoration.
 "$HELPER" --preflight > "$BASE/current-values.txt" 2> "$BASE/current-error.txt"
 current_rc=$?
 if test "$MODE" = roundtrip && test "$ATTEMPTED" = 1 && test -n "$BEFORE"; then
  CURRENT=
  if test "$current_rc" = 0; then CURRENT=$(value "$BASE/current-values.txt"); fi
  if test "$CURRENT" = "$NEXT" && test "$CURRENT" != "$BEFORE"; then
   if ! LD_LIBRARY_PATH=/usr/local/lib NCC_NATIVE_TRIAL=1 "$HELPER" --apply "$ID" "$CURRENT" "$BEFORE" > "$BASE/restore.txt" 2> "$BASE/restore-error.txt"; then
    echo 'Vendor restoration was not confirmed; inspect read-back and do not retry blindly' >&2
    result=1
   fi
  elif test "$CURRENT" != "$BEFORE"; then
   echo 'Current value is unknown or changed by another actor; no blind restoration attempted' >&2
   result=1
  fi
  # Give the stock settings service its documented debounce window; persistence still needs a separate reboot trial.
  sleep 4
  cp -p /pio_data/SYSTEM/SETTING/Setting.DAT "$BASE/after-Setting.DAT" || result=1
 fi
 "$HELPER" --preflight > "$BASE/after-values.txt" 2> "$BASE/after-error.txt" || result=1
 if test "$MODE" = roundtrip && test "$ATTEMPTED" = 1 && test -n "$BEFORE"; then
  AFTER=$(value "$BASE/after-values.txt")
  test "$AFTER" = "$BEFORE" || { echo 'Original setting restoration is unverified' >&2; result=1; }
 fi
 if test "$BASELINE" = 1; then cmp -s "$BASE/before-values.txt" "$BASE/after-values.txt" || { echo 'Native values/global semaphore counts did not return to baseline' >&2; result=1; }; fi
 capture_stock > "$BASE/stock-after.txt" || result=1
 if test "$BASELINE" = 1; then cmp -s "$BASE/stock-before.txt" "$BASE/stock-after.txt" || { echo 'Stock service PID/state changed' >&2; result=1; }; fi
 printf 'Private native-setting trial read-back: %s\n' "$BASE"
 if test "$result" = 0; then echo 'Requested trial/read-back completed; stock PID/state preserved. Recipient function still requires separate verification.'; else echo 'Native trial failed or inconclusive; inspect private read-back' >&2; fi
 exit "$result"
}
trap finish 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 143' 15
capture_stock > "$BASE/stock-before.txt"
BASELINE=1
test "$(grep -c '^ActiveState=active$' "$BASE/stock-before.txt")" = 7 && test "$(grep -c '^MainPID=[1-9][0-9]*$' "$BASE/stock-before.txt")" = 7 || { echo 'Stock service baseline unhealthy; refusing trial' >&2; exit 1; }
"$HELPER" --preflight > "$BASE/before-values.txt" 2> "$BASE/before-error.txt"
cp -p /pio_data/SYSTEM/SETTING/Setting.DAT "$BASE/before-Setting.DAT"
case "$MODE" in
 preflight) echo 'Preflight only; vendor library not loaded';;
 vendor-read) LD_LIBRARY_PATH=/usr/local/lib NCC_NATIVE_TRIAL=1 "$HELPER" --vendor-read > "$BASE/vendor-read.txt" 2> "$BASE/vendor-read-error.txt";;
 roundtrip)
  BEFORE=$(value "$BASE/before-values.txt")
  test -n "$BEFORE" && test "$BEFORE" != "$NEXT" || { echo 'A distinct known native target is required' >&2; exit 64; }
  ATTEMPTED=1
  LD_LIBRARY_PATH=/usr/local/lib NCC_NATIVE_TRIAL=1 "$HELPER" --apply "$ID" "$BEFORE" "$NEXT" > "$BASE/apply.txt" 2> "$BASE/apply-error.txt"
  sleep 4
  "$HELPER" --preflight > "$BASE/applied-values.txt" 2> "$BASE/applied-readback-error.txt"
  cp -p /pio_data/SYSTEM/SETTING/Setting.DAT "$BASE/applied-Setting.DAT"
  test "$(value "$BASE/applied-values.txt")" = "$NEXT"
  # EXIT performs conditional compare-and-restore and all stock/read-back checks.
 ;;
esac
# Do not put an external hard-kill timeout around vendor mode. The helper defers
# HUP/INT/TERM while holding vendor locks and bounds mutex waits internally.
