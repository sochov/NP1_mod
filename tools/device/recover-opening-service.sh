#!/bin/sh
# Fixed Opening-only recovery. Stage recover-opening-sync.py beside this script.
# No stop/restart/kill/config change. Quarantined map is retained for forensics.
set -eu
umask 077
test "$(id -u)" = 0
HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
BASE=$(mktemp -d /tmp/ncc-opening-recovery-XXXXXX)
systemctl list-units --all --type=service --plain --no-pager --no-legend | awk '{print $1}' | sed '/^app-@Opening.service$/d' | sort > "$BASE/units.txt"
capture(){
    cat /proc/sys/kernel/random/boot_id
    # list-units supplies systemd unit tokens, never arbitrary shell fragments.
    # shellcheck disable=SC2046
    systemctl show $(cat "$BASE/units.txt") --no-pager -p Id -p MainPID -p InvocationID -p ActiveState -p SubState
}
capture > "$BASE/before.txt"
systemctl show app-@Opening.service > "$BASE/opening-before.txt"
python3 "$HERE/recover-opening-sync.py" --quarantine > "$BASE/quarantine.txt" 2>&1
systemctl --no-block --job-mode=ignore-dependencies start app-@Opening.service
stable=0
count=0
last_pid=0
while test "$count" -lt 60; do
    state=$(systemctl show app-@Opening.service -p ActiveState --value)
    pid=$(systemctl show app-@Opening.service -p MainPID --value)
    if test "$state" = active && test "$pid" -gt 0; then
        if test "$pid" = "$last_pid"; then stable=$((stable+1)); else stable=0; fi
        last_pid=$pid
        if test "$stable" -ge 20; then break; fi
    else
        stable=0
        if test "$state" = failed; then break; fi
    fi
    count=$((count+1))
    sleep 0.25
done
systemctl show app-@Opening.service > "$BASE/opening-after.txt"
capture > "$BASE/after.txt"
printf 'Private evidence: %s\n' "$BASE"
test "$stable" -ge 20
cmp -s "$BASE/before.txt" "$BASE/after.txt"
echo 'PASS: Opening active with one PID for five seconds; every other loaded service and boot preserved'
