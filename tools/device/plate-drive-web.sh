#!/bin/sh
# Switch only the owner Web/collector to the drive-trial binary. The original
# Web binary remains the rollback target. The plate sensor runs independently.
set -eu
umask 077

base=/overlay/owner-apps/np1-control
service=np1-control.service
unit=/etc/systemd/system/$service
dropin=$unit.d/90-plate-drive-trial.conf
worker=$base/bin/ncc-plate-web
images=/mnt/sdcard/NP1CC/plate/images
runtime=/run/np1-control/ncc-plate-web

test "$(id -u)" = 0
test -f "$unit" && test "$unit" -ef "/overlay$unit"
test ! -L "$unit.d" && test ! -L "$dropin" && test ! -L "$worker"
exec 9>"$base/recovery/.plate-drive-web.lock"
flock -n 9

rollback() {
    rm -f -- "$dropin" || return 1
    systemctl daemon-reload || return 1
    systemctl restart "$service" || return 1
    rm -f -- "$worker" "$runtime"
}

case "${1:-}" in
    rollback) rollback; exit ;;
    apply) ;;
    *) exit 64 ;;
esac

stage=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
case "$stage" in /run/np1-plate-web-*) ;; *) exit 1 ;; esac
test ! -e "$worker" && test ! -e "$dropin"
test "$(systemctl show --value -p ActiveState "$service")" = active
grep -q ' /mnt/sdcard ' /proc/mounts
mkdir -p -- "$images"
systemd-run --wait --quiet --uid=ncc --gid=ncc -p SupplementaryGroups=apluser \
    -p ProtectSystem=strict -p "ReadWritePaths=$images" \
    /bin/sh -ec 'probe=$(mktemp "$1/.probe-XXXXXXXX"); rm -f -- "$probe"' sh "$images"
(cd "$stage" && sha256sum -c SHA256SUMS >/dev/null)
chmod 0755 "$stage/ncc-plate-web"
"$stage/ncc-plate-web" -mode=self-test

stock() {
    systemctl show -p Id -p MainPID -p NRestarts -p ActiveState \
        app-@Communication.service app-@DriveRec.service app-@ImageRecog.service \
        app-@Voice.service qmmf-server.service
}
stock > "$stage/stock-before"
completed=0
finish() {
    status=$?
    trap - 0
    if test "$completed" = 0; then rollback || status=1; fi
    exit "$status"
}
trap finish 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
cp -p "$stage/ncc-plate-web" "$worker"
mkdir -p -- "$unit.d"
cat > "$dropin" <<EOF
[Service]
SupplementaryGroups=apluser
ReadWritePaths=-$images
Environment=NCC_PLATE_IMAGE_DIR=$images
Environment=NCC_PLATE_DIAGNOSTICS=1
ExecStartPre=+/bin/cp -p $worker $runtime
ExecStart=
ExecStart=$runtime --mode server
EOF
systemctl daemon-reload
systemctl restart "$service"
systemctl is-active --quiet "$service"
cmp "$worker" "$runtime"
wget -qO "$stage/status" http://127.0.0.1/api/v1/plate/status
grep -q '"enabled":true' "$stage/status"
stock > "$stage/stock-after"
cmp "$stage/stock-before" "$stage/stock-after"
completed=1
echo 'Trial Web/collector active; original Web binary retained'
