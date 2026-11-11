#!/bin/sh
# Fixed native GetMComAPNStatus only. No APN apply, Proxy claim or stock changes.
set -eu
umask 077
test "$(id -u)" = 0
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$STAGE"
sha256sum -c SHA256SUMS >/dev/null
test -f bin/ncc && test ! -L bin/ncc
case "${1:-status}" in
status) BASE=/run/np1-native-apn-status; NCC_NATIVE_APN_READ_MODE=native-apn-status-probe;;
network) BASE=/run/np1-native-apn-network-check; NCC_NATIVE_APN_READ_MODE=native-apn-network-check;;
internet) BASE=/run/np1-cellular-internet-probe; NCC_NATIVE_APN_READ_MODE=cellular-internet-probe;;
*) exit 2;;
esac
NCC_NATIVE_APN_READ_BASE=$BASE
export NCC_NATIVE_APN_READ_BASE NCC_NATIVE_APN_READ_MODE
test ! -L "$BASE"
mkdir -p "$BASE"
test "$(stat -c %u "$BASE")" = 0
chmod 0700 "$BASE"
mkdir "$BASE/lock" || { echo 'A native APN status probe is in progress' >&2; exit 2; }
stock_snapshot() {
    for unit in app-@Communication app-@DriveRec app-@ImageRecog app-@Voice qmmf-server NEventWatcherCom app-@Setting; do
        systemctl show "$unit.service" -p MainPID -p ActiveState
    done
}
stock_snapshot > "$BASE/before.txt"
finish() {
    result=$?
    trap - 0 HUP INT TERM
    stock_snapshot > "$BASE/after.txt"
    if ! cmp -s "$BASE/before.txt" "$BASE/after.txt"; then
        echo 'Stock PID/state changed during native status read' >&2
        result=1
    fi
    rmdir "$BASE/lock" || result=1
    if test "$result" = 0; then echo 'Native status read completed; stock service PIDs/states preserved'; fi
    exit "$result"
}
trap finish 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
cp bin/ncc "$BASE/ncc.new"
chmod 0700 "$BASE/ncc.new"
mv -f "$BASE/ncc.new" "$BASE/ncc"
python3 - <<'PY'
import os, signal, subprocess
base=os.environ['NCC_NATIVE_APN_READ_BASE']
child=None
def stop_child():
    if child is not None and child.poll() is None:
        child.kill()
        child.wait(timeout=5)
def interrupted(signum, frame):
    stop_child()
    raise SystemExit('Native APN status interrupted')
for number in (signal.SIGHUP,signal.SIGINT,signal.SIGTERM):
    signal.signal(number,interrupted)
with open(base+'/summary.json','w') as output,open(base+'/error.txt','w') as errors:
    child=subprocess.Popen([base+'/ncc','--mode',os.environ['NCC_NATIVE_APN_READ_MODE']],stdout=output,stderr=errors)
    try:
        result=child.wait(timeout=20)
    except subprocess.TimeoutExpired:
        stop_child()
        raise SystemExit('Native APN status timed out; no automatic retry')
if result!=0:
    raise SystemExit('Native APN status failed; inspect private error.txt')
PY
cat "$BASE/summary.json"
