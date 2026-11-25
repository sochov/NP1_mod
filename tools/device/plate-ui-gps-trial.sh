#!/bin/sh
# Replace the existing owner plate Web trial and use the matching GPS broker.
# The previous Web binary stays available for the rollback action.
set -eu
umask 077

base=/overlay/owner-apps/np1-control
web=np1-control.service
broker=np1-control-broker.service
units=/etc/systemd/system
web_trial=$units/$web.d/90-plate-drive-trial.conf
broker_dir=$units/$broker.d
broker_trial=$broker_dir/90-plate-gps-trial.conf
worker=$base/bin/ncc-plate-web
backup=$base/recovery/plate-ui-gps-previous
web_runtime=/run/np1-control/ncc-plate-web
base_broker_runtime=/run/np1-control/ncc
broker_runtime=/run/np1-control/ncc-plate-gps-broker
stage=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)

test "$(id -u)" = 0
case "$stage" in /run/np1-plate-ui-gps-*) ;; *) exit 64 ;; esac
test -f "$web_trial" && test ! -L "$web_trial"
test -f "$units/$broker" && test "$units/$broker" -ef "/overlay$units/$broker"
test -f "$worker" && test ! -L "$worker"
test ! -L "$broker_dir" && test ! -L "$broker_trial"
test ! -L "$backup"
exec 9>"$base/recovery/.plate-drive-web.lock"
flock -n 9

replace_worker() {
    test ! -e "$worker.next" && test ! -L "$worker.next" || return 1
    cp -p -- "$1" "$worker.next" || return 1
    mv -f -- "$worker.next" "$worker"
}

rollback() {
    test -f "$backup/ncc-plate-web" && test ! -L "$backup/ncc-plate-web" || return 1
    rm -f -- "$worker.next" "$broker_trial" || return 1
    replace_worker "$backup/ncc-plate-web" || return 1
    systemctl daemon-reload || return 1
    systemctl restart "$broker" || return 1
    systemctl restart "$web" || return 1
    cmp "$backup/ncc-plate-web" "$web_runtime" || return 1
    systemctl is-active --quiet "$broker" && systemctl is-active --quiet "$web"
}

case "${1:-}" in
    rollback)
        rollback
        echo 'Previous Web and broker restored'
        exit
        ;;
    apply) ;;
    *) exit 64 ;;
esac

test ! -e "$broker_trial" && test ! -e "$worker.next"
test -f "$stage/ncc-ui" && test ! -L "$stage/ncc-ui"
(cd "$stage" && sha256sum -c SHA256SUMS >/dev/null)
chmod 0755 "$stage/ncc-ui"
"$stage/ncc-ui" --mode self-test
cmp "$worker" "$web_runtime"
version=$("$stage/ncc-ui" --mode update-version)
contract=$("$stage/ncc-ui" --mode update-recorder-contract)
test "$("$web_runtime" --mode update-version)" = "$version"
for previous in "$web_runtime" "$base_broker_runtime"; do
    test "$("$previous" --mode update-recorder-contract)" = "$contract"
done
uid=$(id -u ncc)
case "$uid" in ''|*[!0-9]*) exit 1 ;; esac
systemctl is-active --quiet "$web"
systemctl is-active --quiet "$broker"
grep -q ' /mnt/sdcard ' /proc/mounts

stock() {
    systemctl show -p Id -p MainPID -p NRestarts -p ActiveState \
        app-@Communication.service app-@DriveRec.service app-@ImageRecog.service \
        app-@Voice.service qmmf-server.service
}
status() {
    wget -qO "$stage/status" -T 5 http://127.0.0.1/api/v1/plate/status &&
        grep -q '"enabled":true' "$stage/status" &&
        wget -qO "$stage/gps" -T 5 http://127.0.0.1/api/v1/device/gps/live &&
        grep -q '"fix":' "$stage/gps"
}
stock > "$stage/stock-before"
status
fix_required=0
live_required=0
if grep -q '"fix":true' "$stage/gps"; then fix_required=1; fi
if grep -q '"live":true' "$stage/gps"; then live_required=1; fi
if test ! -e "$backup"; then
    mkdir -m 0700 -- "$backup"
    cp -p -- "$worker" "$backup/ncc-plate-web"
fi
test -f "$backup/ncc-plate-web" && test ! -L "$backup/ncc-plate-web"
cmp "$worker" "$backup/ncc-plate-web"
completed=0
finish() {
    result=$?
    trap - 0
    if test "$completed" = 0; then
        if ! rollback; then result=1; fi
    fi
    exit "$result"
}
trap finish 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
replace_worker "$stage/ncc-ui"
mkdir -p -- "$broker_dir"
cat > "$broker_trial" <<EOF
[Service]
ExecStartPre=+/bin/cp -p $worker $broker_runtime
ExecStart=
ExecStart=$broker_runtime --mode broker --worker-uid $uid
EOF
systemctl daemon-reload
systemctl restart "$broker"
systemctl restart "$web"
cmp "$stage/ncc-ui" "$web_runtime"
cmp "$stage/ncc-ui" "$broker_runtime"

for attempt in 1 2 3 4 5 6 7 8; do
    if status && { test "$fix_required" = 0 || grep -q '"fix":true' "$stage/gps"; } &&
        { test "$live_required" = 0 || grep -q '"live":true' "$stage/gps"; }; then
        break
    fi
    test "$attempt" -lt 8
    sleep 2
done
stock > "$stage/stock-after"
cmp "$stage/stock-before" "$stage/stock-after"
completed=1
echo 'Matching Web and GPS broker active; rollback retained'
