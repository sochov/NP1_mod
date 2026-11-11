#!/bin/sh
# Explicit controlled SAME-profile1 owner write. Never accepts new APN values.
set -eu
umask 077
test "$(id -u)" = 0
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$STAGE"
sha256sum -c SHA256SUMS >/dev/null
test -f bin/ncc && test ! -L bin/ncc
BASE=/run/np1-native-apn-same-profile
test ! -L "$BASE"
mkdir -p "$BASE"
test "$(stat -c %u "$BASE")" = 0
chmod 0700 "$BASE"
mkdir "$BASE/lock" || { echo 'A native APN trial is in progress' >&2; exit 2; }
WORKER_WAS_ACTIVE=0
if systemctl is-active --quiet np1-control-modem.service; then WORKER_WAS_ACTIVE=1; fi
stock_snapshot() {
    for unit in app-@Communication app-@DriveRec app-@ImageRecog app-@Voice qmmf-server NEventWatcherCom app-@Setting; do
        systemctl show "$unit.service" -p MainPID -p ActiveState
    done
}
network_snapshot() {
    ip -o -4 address show | awk '{print $2,$4}' > "$BASE/$1-addresses.txt"
    ip -4 route show table all > "$BASE/$1-routes.txt"
    ip -4 rule show > "$BASE/$1-rules.txt"
    iptables-save > "$BASE/$1-firewall.txt"
}
finish() {
    result=$?
    trap - 0 HUP INT TERM
    if test "$WORKER_WAS_ACTIVE" = 1; then
        if ! systemctl start np1-control-modem.service; then result=1; fi
        if ! "$BASE/ncc" --mode profiles-observer-check > "$BASE/observer-check.txt" 2>&1; then
            echo 'Modem observer did not recover measured signal' >&2
            result=1
        fi
    fi
    stock_snapshot > "$BASE/after.txt"
    network_snapshot after || result=1
    if ! cmp -s "$BASE/before.txt" "$BASE/after.txt"; then echo 'Stock PID/state changed during native trial' >&2; result=1; fi
    for item in addresses routes rules; do
        if ! cmp -s "$BASE/before-$item.txt" "$BASE/after-$item.txt"; then
            echo "Network $item changed; private snapshots retained" >&2
            result=1
        fi
    done
    python3 - <<'PY' || result=1
import re
base='/run/np1-native-apn-same-profile/'
def normalized(name):
    with open(base+name) as source:
        return [re.sub(r'\[[0-9]+:[0-9]+\]', '[COUNTERS]', line.strip()) for line in source if not line.startswith('#')]
if normalized('before-firewall.txt') != normalized('after-firewall.txt'):
    raise SystemExit('Firewall rules changed; private snapshots retained')
PY
    rmdir "$BASE/lock" || result=1
    if test "$result" = 0; then echo 'Same-profile owner trial completed; stock services and network preserved'; fi
    exit "$result"
}
trap finish 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
stock_snapshot > "$BASE/before.txt"
network_snapshot before
cp bin/ncc "$BASE/ncc.new"
chmod 0700 "$BASE/ncc.new"
mv -f "$BASE/ncc.new" "$BASE/ncc"
if test "$WORKER_WAS_ACTIVE" = 1; then systemctl stop np1-control-modem.service; fi
python3 - <<'PY'
import signal, subprocess
base='/run/np1-native-apn-same-profile'
child=None
def stop_child():
    if child is not None and child.poll() is None:
        child.kill()
        child.wait(timeout=5)
def interrupted(signum, frame):
    stop_child()
    raise SystemExit('Native APN trial interrupted; no automatic retry')
for number in (signal.SIGHUP,signal.SIGINT,signal.SIGTERM):
    signal.signal(number,interrupted)
with open(base+'/summary.json','w') as output,open(base+'/error.txt','w') as errors:
    child=subprocess.Popen([base+'/ncc','--mode','native-apn-same-profile-trial'],stdout=output,stderr=errors)
    try:
        result=child.wait(timeout=70)
    except subprocess.TimeoutExpired:
        stop_child()
        raise SystemExit('Native APN trial timed out; inspect private state, no retry')
if result!=0:
    raise SystemExit('Native APN trial failed; inspect private error/summary')
PY
cat "$BASE/summary.json"
