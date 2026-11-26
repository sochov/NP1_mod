#!/bin/sh
# Switch the Web collector and plate sensor together; leave the recorder running.
# apply: stage ncc, plate-engine, SHA256SUMS, this script and update-lock.sh in
# a root-only /run/np1-plate-tracking-XXXXXXXX directory. Rollback uses the
# saved script in the recovery directory and the same installed paths.
set -eu
umask 077
BASE=/overlay/owner-apps/np1-control
here=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
action=${1:?apply or rollback}
case "$action:$here" in
    apply:/run/np1-plate-tracking-????????) ;;
    rollback:/overlay/owner-apps/np1-control/recovery/plate-tracking-????????) ;;
    *) exit 64 ;;
esac
test "$(id -u)" = 0
test "$(stat -c %u:%a "$here")" = 0:700
. "$here/update-lock.sh"
exec 9>"$BASE/recovery/.plate-drive-web.lock"
flock -n 9
web=np1-control.service
sensor=np1-control-plate.service
runtime=/run/np1-control/ncc
owner=$BASE/bin/ncc
worker=$BASE/plate/ncc-worker
engine=$BASE/plate/plate-engine
for file in "$runtime" "$owner" "$worker" "$engine"; do test -f "$file" && test ! -L "$file"; done

stock() {
    systemctl show -p Id -p ActiveState -p MainPID -p NRestarts app-@Communication.service \
        app-@DriveRec.service app-@ImageRecog.service app-@Voice.service qmmf-server.service
}
replace() {
    # Keep each target's existing ownership and permissions, even in /run.
    temp=$(mktemp "$2.next.XXXXXXXX") || return 1
    cp -p "$2" "$temp" && cat "$1" > "$temp" && cmp "$1" "$temp" && mv -f "$temp" "$2" || {
        rm -f "$temp"; return 1
    }
}
switch_to() {
    systemctl stop "$web" "$sensor" &&
        replace "$1" "$owner" && replace "$2" "$worker" && replace "$3" "$engine" &&
        replace "$1" "$runtime" && systemctl start "$sensor" "$web"
}
check() {
    cmp "$owner" "/proc/$(systemctl show --value -p MainPID "$web")/exe" &&
        cmp "$worker" "/proc/$(systemctl show --value -p MainPID "$sensor")/exe" &&
        cmp "$engine" /run/np1-control-plate/plate-engine &&
        test -S /run/np1-control-plate/sensor.sock &&
        wget -qO - -T 5 http://127.0.0.1/api/v1/plate/status | grep -q '"enabled":true'
}
wait_ready() {
    for i in 1 2 3 4 5 6 7 8 9 10; do
        if check; then return 0; fi
        sleep 1
    done
    return 1
}
restore() {
    (cd "$backup" && sha256sum -c originals.sha256 >/dev/null) &&
        switch_to "$backup/ncc" "$backup/ncc-worker" "$backup/plate-engine" && wait_ready
}
if test "$action" = rollback; then
    backup=$here
    (cd "$BASE" && sha256sum -c "$backup/installed.sha256" >/dev/null)
    restore
    exit
fi

(cd "$here" && sha256sum -c SHA256SUMS)
"$here/ncc" --mode self-test
test "$("$here/ncc" --mode update-recorder-contract)" = "$("$runtime" --mode update-recorder-contract)"
test "$("$here/ncc" --mode update-version)" = "$("$runtime" --mode update-version)"
cmp "$owner" "$runtime"
check
backup=$(mktemp -d "$BASE/recovery/plate-tracking-XXXXXXXX")
chmod 0700 "$backup"
# Atomic replacements leave these original inodes intact without copying them.
ln "$owner" "$backup/ncc"
ln "$worker" "$backup/ncc-worker"
ln "$engine" "$backup/plate-engine"
cp "$here/activate-plate-tracking.sh" "$here/update-lock.sh" "$backup/"
(cd "$backup" && sha256sum ncc ncc-worker plate-engine > originals.sha256)
stock > "$backup/stock-before"
echo "Recovery directory: $backup"
success=0
finish() {
    rc=$?
    trap - 0 HUP INT TERM
    if test "$success" = 0; then restore || rc=1; fi
    exit "$rc"
}
trap finish 0
trap 'exit 1' HUP INT TERM
switch_to "$here/ncc" "$here/ncc" "$here/plate-engine"
wait_ready
stock > "$backup/stock-after"
cmp "$backup/stock-before" "$backup/stock-after"
(cd "$BASE" && sha256sum bin/ncc plate/ncc-worker plate/plate-engine > "$backup/installed.sha256")
success=1
echo 'Web collector and plate sensor verified'
