#!/bin/sh
# Update the owner AP worker without restarting Web/broker/native applications.
# Configuration is never rewritten. Save the old owner binary and state, restore
# that binary on failure, and restart only np1-control-ap.service.
set -eu
umask 077
BASE=/overlay/owner-apps/np1-control
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
test "$(id -u)" = 0
systemctl is-active --quiet np1-control-ap.service
test -f "$BASE/private/ap/autostart"
cd "$STAGE"
sha256sum -c SHA256SUMS >/dev/null
/bin/chmod 0755 bin/ncc
bin/ncc --mode self-test
BACKUP="$BASE/recovery/ap-worker-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -m 0700 "$BACKUP"
cp -p "$BASE/bin/ncc" "$BACKUP/ncc"
systemctl show np1-control.service np1-control-broker.service -p Id -p MainPID -p InvocationID -p ActiveState -p SubState > "$BACKUP/web-before.txt"
sha256sum "$BASE/private/ap/config.json" > "$BACKUP/config-before.sha256"
COMPLETED=0
finish(){
    result=$?
    trap - 0
    if test "$COMPLETED" != 1; then
        cp "$BACKUP/ncc" "$BASE/bin/ncc.rollback"
        chmod 0755 "$BASE/bin/ncc.rollback"
        mv -f "$BASE/bin/ncc.rollback" "$BASE/bin/ncc"
        systemctl restart np1-control-ap.service || result=1
        echo 'AP update failed; previous owner binary restored' >&2
    fi
    exit "$result"
}
trap finish 0
cp bin/ncc "$BASE/bin/ncc.ap-new"
chmod 0755 "$BASE/bin/ncc.ap-new"
mv -f "$BASE/bin/ncc.ap-new" "$BASE/bin/ncc"
systemctl restart np1-control-ap.service
systemctl is-active --quiet np1-control-ap.service
count=0
until grep -Eq '"dhcpReady"[[:space:]]*:[[:space:]]*true' /run/np1-control-ap/status.json && grep -Eq '"state"[[:space:]]*:[[:space:]]*"enabled"' /run/np1-control-ap/status.json; do
    count=$((count+1))
    test "$count" -lt 40
    sleep 0.5
done
systemctl show np1-control.service np1-control-broker.service -p Id -p MainPID -p InvocationID -p ActiveState -p SubState > "$BACKUP/web-after.txt"
cmp "$BACKUP/web-before.txt" "$BACKUP/web-after.txt"
sha256sum -c "$BACKUP/config-before.sha256"
sh tools/verify.sh
COMPLETED=1
echo 'AP worker updated; Web/broker identities and AP configuration preserved'
