#!/bin/sh
# Explicit opt-in fixed GET diagnostics alongside the verified stock CM only.
# Never changes STA/AP/CM, stops services, binds a data call, or applies APN.
set -eu
umask 077
test "$#" = 1
case "$1" in
 nas) MODE=stock-cm-nas-readonly;;
 profiles) MODE=stock-cm-profiles-readonly;;
 *) exit 64;;
esac
test "$(id -u)" = 0
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$STAGE"
sha256sum -c SHA256SUMS >/dev/null
test -f bin/ncc && test ! -L bin/ncc
exec 9>/run/np1-stock-cm-fixed-get.lock
flock -n 9 || exit 75
BASE=$(mktemp -d /run/np1-stock-cm-get.XXXXXX)
chmod 0700 "$BASE"
cp bin/ncc "$BASE/ncc"
chmod 0700 "$BASE/ncc"
stock() {
 systemctl show app-@Communication.service app-@DriveRec.service app-@ImageRecog.service app-@Voice.service qmmf-server.service NEventWatcherCom.service app-@Setting.service -p Id -p ActiveState -p MainPID -p InvocationID
}
finish() {
 result=$?
 trap - 0
 trap '' HUP INT TERM
 set +e
 stock >"$BASE/stock-after.private"
 cmp -s "$BASE/stock-before.private" "$BASE/stock-after.private" || result=1
 ip -4 route show table main >"$BASE/routes-after.private"
 rm -f "$BASE/ncc"
 printf 'Private fixed CM GET records: %s\n' "$BASE"
 printf 'NCC_EXIT_STATUS=%s\n' "$result"
 exit "$result"
}
trap finish 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
stock >"$BASE/stock-before.private"
ip -4 route show table main >"$BASE/routes-before.private"
timeout -t 15 -s TERM "$BASE/ncc" --mode "$MODE" >"$BASE/result.json" 2>"$BASE/error-private.txt"
cat "$BASE/result.json"
