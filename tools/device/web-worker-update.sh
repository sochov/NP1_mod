#!/bin/sh
# Update only the owner Web/API and broker. Preserve the AP and stock services.
# Backup old owner binary; restore it on failure. No native service action.
set -eu
umask 077
BASE=/overlay/owner-apps/np1-control
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
test "$(id -u)" = 0
cd "$STAGE"
sha256sum -c SHA256SUMS >/dev/null
/bin/chmod 0755 bin/ncc
bin/ncc --mode self-test
BACKUP="$BASE/recovery/web-worker-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -m 0700 "$BACKUP"
cp -p "$BASE/bin/ncc" "$BACKUP/ncc"
capture(){
    systemctl show np1-control-ap.service np1-control-modem.service np1-control-native.service -p Id -p MainPID -p InvocationID -p ActiveState -p SubState
}
capture > "$BACKUP/workers-before.txt"
COMPLETED=0
finish(){
    result=$?
    trap - 0
    if test "$COMPLETED" != 1; then
        cp "$BACKUP/ncc" "$BASE/bin/ncc.rollback"
        chmod 0755 "$BASE/bin/ncc.rollback"
        mv -f "$BASE/bin/ncc.rollback" "$BASE/bin/ncc"
        systemctl restart np1-control-broker.service np1-control.service || result=1
        echo 'Web update failed; previous owner binary restored' >&2
    fi
    exit "$result"
}
trap finish 0
cp bin/ncc "$BASE/bin/ncc.web-new"
chmod 0755 "$BASE/bin/ncc.web-new"
mv -f "$BASE/bin/ncc.web-new" "$BASE/bin/ncc"
systemctl restart np1-control-broker.service
systemctl restart np1-control.service
sh tools/verify.sh
capture > "$BACKUP/workers-after.txt"
cmp "$BACKUP/workers-before.txt" "$BACKUP/workers-after.txt"
COMPLETED=1
echo 'Web/broker updated; AP, modem and native worker identities preserved'
