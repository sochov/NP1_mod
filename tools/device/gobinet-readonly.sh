#!/bin/sh
# Firmware-pinned, independent NAS CID only; no AT/shared descriptors or WDS/WDA settings.
# A separate process bounds driver ioctl waits; Python is only the host for its timeout.
set -eu
case "${1:-signal}" in
    signal) NCC_GOBI_MODE=gobinet-probe;;
    status) NCC_GOBI_MODE=gobinet-status-probe;;
    *) echo 'Unsupported observer mode' >&2; exit 2;;
esac
export NCC_GOBI_MODE
test -f /tmp/ncc-stage/bin/ncc && test ! -L /tmp/ncc-stage/bin/ncc
chmod 0755 /tmp/ncc-stage/bin/ncc
BASE=/run/np1-gobinet-probe
mkdir -p "$BASE"
chmod 0700 "$BASE"
for unit in app-@Communication app-@DriveRec app-@ImageRecog app-@Voice qmmf-server NEventWatcherCom; do systemctl show "$unit.service" -p MainPID -p ActiveState; done > "$BASE/before.txt"
finish() {
    result=$?
    trap - 0
    for unit in app-@Communication app-@DriveRec app-@ImageRecog app-@Voice qmmf-server NEventWatcherCom; do systemctl show "$unit.service" -p MainPID -p ActiveState; done > "$BASE/after.txt"
    cmp "$BASE/before.txt" "$BASE/after.txt" || result=1
    if test "$result" = 0; then echo 'Independent NAS query finished; stock service PIDs/states preserved'; fi
    exit "$result"
}
trap finish 0
python3 - <<'PY'
import subprocess
import os
command=['/tmp/ncc-stage/bin/ncc','--mode',os.environ['NCC_GOBI_MODE']]
with open('/run/np1-gobinet-probe/signal.json','w') as output:
    child=subprocess.Popen(command,stdout=output,stderr=subprocess.STDOUT)
    try:
        result=child.wait(timeout=15)
    except subprocess.TimeoutExpired:
        child.kill()
        child.wait(timeout=5)
        raise RuntimeError('Independent NAS helper timed out; do not retry automatically')
    if result != 0:
        raise RuntimeError('NAS helper failed; inspect private signal.json')
PY
cat "$BASE/signal.json"
