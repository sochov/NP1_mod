#!/bin/sh
# Restore only the missing default in our latest validated private APN backup.
# No native APN command, QMI query, route replace/flush or service stop.
set -eu
umask 077
test "$(id -u)" = 0
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$STAGE"
sha256sum -c SHA256SUMS >/dev/null
BASE=/run/np1-native-apn-route-recovery
test ! -L "$BASE"
mkdir -p "$BASE"
chmod 0700 "$BASE"
test "$(stat -c %u "$BASE")" = 0
mkdir "$BASE/lock"
trap 'rmdir "$BASE/lock"' 0
cp bin/ncc "$BASE/ncc.new"
chmod 0700 "$BASE/ncc.new"
mv -f "$BASE/ncc.new" "$BASE/ncc"
python3 - <<'PY'
import subprocess
base='/run/np1-native-apn-route-recovery'
with open(base+'/summary.json','w') as out,open(base+'/error.txt','w') as err:
    child=subprocess.Popen([base+'/ncc','--mode','native-apn-route-recover'],stdout=out,stderr=err)
    try:
        result=child.wait(timeout=20)
    except subprocess.TimeoutExpired:
        child.kill(); child.wait(timeout=5)
        raise SystemExit('Route recovery timed out; no retry')
if result!=0:
    raise SystemExit('Route recovery failed; inspect private error.txt')
PY
cat "$BASE/summary.json"
