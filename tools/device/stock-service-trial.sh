#!/bin/sh
# Reproducible individual service trial. No unit/config/startup changes.
# Backup: private /tmp/ncc-stock-service-* before/after state and boot ID.
# Restore: start the originally active Opening unit on every exit, without
# dependency jobs. Never send SIGKILL or restart/mask/disable another unit.
set -eu
umask 077
test "$(id -u)" = 0
UNIT=app-@Opening.service
HERE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
test -f "$HERE/recover-opening-sync.py"
BASE=$(mktemp -d /tmp/ncc-stock-service-XXXXXX)
UNITS='app-@Communication.service app-@DriveRec.service app-@ImageRecog.service app-@Voice.service app-@Setting.service app-@Update.service app-@Navigation.service app-@VirtualPassenger.service app-@InfoMtgEngine.service app-@Misc.service app-@RemoteMonitoring.service app-@SitAwrEng.service app-@System.service app-AppManager.service NEventWatcherAV.service NEventWatcherCom.service NEventWatcherITS.service NEventWatcherSys.service qmmf-server.service QCMAP_ConnectionManagerd.service loc_launcher.service location_hal_daemon.service pulseaudio.service np1-control.service np1-control-broker.service np1-control-ap.service'
capture(){
    cat /proc/sys/kernel/random/boot_id
    systemctl show $UNITS --no-pager -p Id -p MainPID -p InvocationID -p ActiveState -p SubState
}
wait_state(){
    count=0
    while test "$count" -lt 60; do
        state=$(systemctl show "$UNIT" -p ActiveState --value)
        pid=$(systemctl show "$UNIT" -p MainPID --value)
        if test "$state" = "$1"; then
            if test "$1" = inactive && test "$pid" = 0; then return 0; fi
            if test "$1" = active && test "$pid" -gt 0; then return 0; fi
        fi
        count=$((count+1))
        sleep 0.25
    done
    return 1
}
systemctl is-active --quiet "$UNIT"
capture > "$BASE/before.txt"
systemctl cat "$UNIT" app-AppManager.service > "$BASE/unit-before.txt"
ORIGINAL=$(systemctl show "$UNIT" -p InvocationID --value)
cleanup(){
    result=$?
    trap - EXIT INT TERM
    set +e
    systemctl --no-block --job-mode=ignore-dependencies start "$UNIT" || result=1
    wait_state active || result=1
    capture > "$BASE/after.txt"
    systemctl cat "$UNIT" app-AppManager.service > "$BASE/unit-after.txt"
    cmp -s "$BASE/before.txt" "$BASE/after.txt" || { echo 'Unrelated service or boot changed; inspect private evidence' >&2; result=1; }
    cmp -s "$BASE/unit-before.txt" "$BASE/unit-after.txt" || result=1
    echo "Private evidence: $BASE"
    if test "$result" = 0; then echo 'PASS: individual stop/start/restart; unrelated services and boot preserved'; fi
    exit "$result"
}
trap cleanup EXIT INT TERM
systemctl --no-block --job-mode=ignore-dependencies stop "$UNIT"
wait_state inactive
capture > "$BASE/stopped.txt"
cmp -s "$BASE/before.txt" "$BASE/stopped.txt"
sleep 2
test "$(systemctl show "$UNIT" -p ActiveState --value)" = inactive
python3 "$HERE/recover-opening-sync.py" --quarantine
systemctl --no-block --job-mode=ignore-dependencies start "$UNIT"
wait_state active
sleep 5
test "$(systemctl show "$UNIT" -p ActiveState --value)" = active
NEXT=$(systemctl show "$UNIT" -p InvocationID --value)
test -n "$NEXT" && test "$NEXT" != "$ORIGINAL"
systemctl --no-block --job-mode=ignore-dependencies stop "$UNIT"
wait_state inactive
python3 "$HERE/recover-opening-sync.py" --quarantine
systemctl --no-block --job-mode=ignore-dependencies start "$UNIT"
wait_state active
count=0
while test "$(systemctl show "$UNIT" -p InvocationID --value)" = "$NEXT"; do
    count=$((count+1))
    test "$count" -lt 60
    sleep 0.25
done
wait_state active
sleep 5
test "$(systemctl show "$UNIT" -p ActiveState --value)" = active
