#!/bin/sh
# Reproducible owner control. No stock service stop/mask or update interruption.
# The CLI records exact original flag existence/content/ownership privately.
set -eu
umask 077
ACTION=${1:-status}
case "$ACTION" in install|status|disable|enable|rollback|uninstall|guard-check|guard-release) ;; *) echo 'Unsupported OTA policy action' >&2; exit 64;; esac
test "$(id -u)" = 0
BASE=/overlay/owner-apps/np1-control
RUNTIME=$(mktemp -d /run/np1-ota-policy.XXXXXX)
capture_stock(){
 for unit in app-@Communication app-@DriveRec app-@ImageRecog app-@Voice qmmf-server NEventWatcherCom app-@Setting app-@Update; do
  printf 'Unit=%s.service\n' "$unit"
  systemctl show "$unit.service" -p MainPID -p ActiveState -p SubState
 done
}
finish(){
 result=$?
 trap - 0
 set +e
 rm -f "$RUNTIME/ncc"
 capture_stock > "$RUNTIME/stock-after.txt" || result=1
 cmp -s "$RUNTIME/stock-before.txt" "$RUNTIME/stock-after.txt" || { echo 'Stock service PID/state changed; inspect private records' >&2; result=1; }
 printf 'Private OTA policy records: %s\n' "$RUNTIME"
 exit "$result"
}
capture_stock > "$RUNTIME/stock-before.txt"
trap finish 0
test -f "$BASE/bin/ncc" && test ! -L "$BASE/bin/ncc"
cp "$BASE/bin/ncc" "$RUNTIME/ncc"
chmod 0700 "$RUNTIME/ncc"
case "$ACTION" in
guard-check) "$RUNTIME/ncc" --mode ota-guard --ota-action check > "$RUNTIME/policy.json" ;;
# Removing the network layer requires the guard to be stopped first, or it would rebuild the chain.
guard-release) systemctl stop np1-control-ota-guard.service 2>/dev/null || true; "$RUNTIME/ncc" --mode ota-guard --ota-action release > "$RUNTIME/policy.json" ;;
*) "$RUNTIME/ncc" --mode ota-policy --ota-action "$ACTION" > "$RUNTIME/policy.json" ;;
esac
cat "$RUNTIME/policy.json"
