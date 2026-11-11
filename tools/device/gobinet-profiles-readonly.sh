#!/bin/sh
# Fixed WDS reads only. No APN apply or stock service mutation.
set -eu
umask 077
test "$(id -u)" = 0
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$STAGE"
sha256sum -c SHA256SUMS >/dev/null
test -f bin/ncc && test ! -L bin/ncc
case "${1:-capture}" in
    capture) NCC_PROFILE_PROBE_MODE=gobinet-profiles-probe; BASE=/run/np1-apn-profiles-probe;;
    inventory) NCC_PROFILE_PROBE_MODE=gobinet-profile-list-probe; BASE=/run/np1-apn-profile-inventory;;
    seed) NCC_PROFILE_PROBE_MODE=apn-editor-seed; BASE=/run/np1-apn-editor-seed;;
    *) echo 'Unsupported fixed profile query' >&2; exit 2;;
esac
NCC_PROFILE_PROBE_BASE=$BASE
export NCC_PROFILE_PROBE_MODE NCC_PROFILE_PROBE_BASE
test ! -L "$BASE"
mkdir -p "$BASE"
test "$(stat -c %u "$BASE")" = 0
chmod 0700 "$BASE"
mkdir "$BASE/lock" || { echo 'A profile probe is already in progress' >&2; exit 2; }
WORKER_WAS_ACTIVE=0
if test "$NCC_PROFILE_PROBE_MODE" != apn-editor-seed && systemctl is-active --quiet np1-control-modem.service; then WORKER_WAS_ACTIVE=1; fi
stock_snapshot() {
    for unit in app-@Communication app-@DriveRec app-@ImageRecog app-@Voice qmmf-server NEventWatcherCom app-@Setting; do
        systemctl show "$unit.service" -p MainPID -p ActiveState
    done
}
stock_snapshot > "$BASE/before.txt"
finish() {
    result=$?
    trap - 0 HUP INT TERM
    if test "$WORKER_WAS_ACTIVE" = 1; then
        if ! systemctl start np1-control-modem.service; then result=1; fi
        if ! "$BASE/ncc" --mode profiles-observer-check > "$BASE/observer-check.txt" 2>&1; then
            echo 'Modem observer did not recover a measured signal; inspect private observer-check.txt' >&2
            result=1
        fi
    fi
    stock_snapshot > "$BASE/after.txt"
    if ! cmp -s "$BASE/before.txt" "$BASE/after.txt"; then
        echo 'Stock service PID/state changed during profile read; inspect private snapshots' >&2
        result=1
    fi
    rmdir "$BASE/lock" || result=1
    if test "$result" = 0; then
        echo 'Fixed profile read completed; stock service PIDs/states preserved'
    fi
    exit "$result"
}
trap finish 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
cp bin/ncc "$BASE/ncc.new"
chmod 0700 "$BASE/ncc.new"
mv -f "$BASE/ncc.new" "$BASE/ncc"
if test "$WORKER_WAS_ACTIVE" = 1; then systemctl stop np1-control-modem.service; fi
python3 - <<'PY'
import os, signal, subprocess
base=os.environ['NCC_PROFILE_PROBE_BASE']
child=None
def stop_child():
    if child is not None and child.poll() is None:
        child.kill()
        child.wait(timeout=5)
def interrupted(signum, frame):
    stop_child()
    raise SystemExit('Profile read interrupted')
for number in (signal.SIGHUP,signal.SIGINT,signal.SIGTERM):
    signal.signal(number,interrupted)
with open(base+'/summary.json','w') as output,open(base+'/error.txt','w') as errors:
    child=subprocess.Popen([base+'/ncc','--mode',os.environ['NCC_PROFILE_PROBE_MODE']],stdout=output,stderr=errors)
    try:
        result=child.wait(timeout=15)
    except subprocess.TimeoutExpired:
        stop_child()
        raise SystemExit('Saved-profile helper timed out; no automatic retry')
if result!=0:
    raise SystemExit('Saved-profile helper failed; inspect private error.txt')
PY
# The fixed Go mode emits a typed summary only, after strict root/0600 Load.
cat "$BASE/summary.json"
