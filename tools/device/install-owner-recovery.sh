#!/bin/sh
# Fresh install through the existing owner recovery unit's late-boot transaction.
set -eu
umask 077
BASE=/overlay/owner-apps/np1-control
UNITS=/etc/systemd/system
ANCHOR=np1-owner-recovery.service
WANTS=$UNITS/$ANCHOR.wants
NCC_ID=61001
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)

stock_units() {
    for unit in app-@Communication.service app-@DriveRec.service app-@ImageRecog.service app-@Voice.service qmmf-server.service "$ANCHOR"; do
        systemctl show "$unit" -p Id -p MainPID -p InvocationID -p ActiveState -p SubState -p Result -p NRestarts
    done
}

rollback() {
    test -f "$BASE/recovery/owner-recovery-install" || return 1
    . "$STAGE/tools/update-lock.sh"
    sh "$STAGE/tools/update-remove.sh" || return 1
    for file in "$BASE"/units/*.path "$BASE"/units/*.service; do
        test -f "$file" || continue
        unit=${file##*/}
        if test -e "$UNITS/$unit"; then cmp -s "$file" "$UNITS/$unit" || return 1; fi
    done
    if test -d "$BASE/recovery/pics-rt"; then
        sh "$BASE/tools/pics-stock-rt-install.sh" rollback || return 1
    fi
    recorder_dropin=$UNITS/app-@DriveRec.service.d/90-ncc-recorder-hook.conf
    if test -f "$BASE/recorder-hook/90-ncc-recorder-hook.conf" && test -e "$recorder_dropin"; then
        cmp "$BASE/recorder-hook/90-ncc-recorder-hook.conf" "$recorder_dropin" || return 1
        rm "$recorder_dropin" || return 1
    fi
    for file in "$BASE"/units/*.path "$BASE"/units/*.service; do
        test -f "$file" || continue
        unit=${file##*/}
        if test "$(readlink "$WANTS/$unit" 2>/dev/null || true)" = "$UNITS/$unit"; then
            rm "$WANTS/$unit" || return 1
        fi
        if test -e "$UNITS/$unit"; then
            systemctl stop "$unit" || return 1
            rm "$UNITS/$unit" || return 1
        fi
    done
    if test -f "$BASE/private/ota-policy/ledger.json" && test -x "$BASE/bin/ncc"; then
        "$BASE/bin/ncc" --mode ota-guard --ota-action release || return 1
        "$BASE/bin/ncc" --mode ota-policy --ota-action uninstall || return 1
    fi
    if test -f "$BASE/firewall-owned"; then sh "$BASE/tools/firewall.sh" remove || return 1; fi
    systemctl daemon-reload || return 1
    archive=$BASE.removed-$(date -u +%Y%m%dT%H%M%SZ)
    test ! -e "$archive" && mv "$BASE" "$archive" || return 1
    echo "Owner installation removed; files retained at $archive"
}

test "$(id -u)" = 0
if test "${1:-}" = rollback; then rollback; exit; fi
test "$#" = 0
test "$(uname -m)" = aarch64
test "$(uname -r)" = 4.14.206-perf
grep -q '^VERSION_ID="202404231204"' /etc/os-release
grep -Eq ' /etc overlay [^ ]*upperdir=/overlay/etc(,| )' /proc/mounts
systemctl is-active --quiet "$ANCHOR"
test "$(systemctl show "$ANCHOR" -p FragmentPath --value)" = "$UNITS/$ANCHOR"
grep -q '^# NP1_OWNER_RECOVERY_SERVICE_V1$' /etc/init.post_boot.sh
test ! -e "$BASE" && test ! -L "$BASE"
test ! -L "${BASE%/*}" && test ! -L "$WANTS"
if grep -Eq "^[^:]+:[^:]+:$NCC_ID:" /etc/passwd /etc/group; then
    echo 'Dedicated numeric service UID/GID already in use' >&2
    exit 1
fi
if find "$UNITS" -name 'np1-control*' | grep -q .; then
    echo 'Existing Control Center units found; fresh install refused' >&2
    exit 1
fi

cd "$STAGE"
sha256sum -c SHA256SUMS >/dev/null
for required in \
    experimental/libncc-driverec-hook.so \
    experimental/ncc-recorder-injector \
    experimental/ncc-native-setting-helper \
    experimental/ncc-native-alexa-helper \
    experimental/ncc-native-setting-extended-helper \
    experimental/ncc-native-setting-choice-helper \
    experimental/ncc-native-setting-notification-helper \
    experimental/ncc-native-audio-helper \
    experimental/ncc-native-operations-helper \
    experimental/ncc-native-wifi-sta-helper \
    experimental/ncc-stock-start-helper
do
    test -f "$required" || { echo "Full install component missing: $required" >&2; exit 1; }
done
chmod 0755 bin/ncc
bin/ncc --mode self-test
mkdir -p -m 0755 "${BASE%/*}"
mkdir -m 0755 "$BASE"
mkdir "$BASE/recovery" "$BASE/config" "$BASE/state" "$BASE/private" "$BASE/tools" "$BASE/units" "$BASE/recorder-hook"
touch "$BASE/recovery/owner-recovery-install"
trap 'result=$?; if test "$result" -ne 0; then (rollback) || echo "Owner install rollback failed" >&2; fi' 0
stock_units > "$BASE/recovery/stock-before.txt"
sha256sum /etc/init.post_boot.sh /overlay/etc/init.post_boot.sh /etc/passwd /etc/group "$UNITS/$ANCHOR" > "$BASE/recovery/stock-files.sha256"
for directory in led apn wifi ap nas ota-policy uplink tailscale; do mkdir "$BASE/private/$directory"; done
chmod 0700 "$BASE/private/tailscale"
mkdir -m 0755 "$BASE/bin"
cp bin/ncc "$BASE/bin/ncc"
chmod 0755 "$BASE/bin/ncc"
bin/ncc --mode ota-policy --ota-action install >/dev/null
cp tools/*.sh tools/postboot.py tools/ap-channel.py tools/run-owner-web.py "$BASE/tools/"

RECORDER_DROPIN_DIR=$UNITS/app-@DriveRec.service.d
RECORDER_DROPIN=$RECORDER_DROPIN_DIR/90-ncc-recorder-hook.conf
test ! -L "$RECORDER_DROPIN_DIR" && test ! -e "$RECORDER_DROPIN"
cp experimental/libncc-driverec-hook.so "$BASE/recorder-hook/libncc-driverec-hook.so"
chmod 0555 "$BASE/recorder-hook/libncc-driverec-hook.so"

bin/ncc --mode setup --config-dir "$BASE/config"
chown "$NCC_ID:$NCC_ID" "$BASE/config" "$BASE/config/"* "$BASE/state"
printf '%s\n' "$NCC_ID" > "$BASE/worker-uid"
touch "$BASE/enabled"

# The owner-recovery fresh-install path includes the same checksum-verified
# Tailscale payload as the standard installer.  Authentication is deliberately
# left to the owner; a future reinstall keeps state under private/tailscale.
test -f vpn/bin/tailscale && test -f vpn/bin/tailscaled
mkdir -m 0700 "$BASE/vpn"
cp vpn/bin/tailscale vpn/bin/tailscaled "$BASE/vpn/"
chmod 0755 "$BASE/vpn/tailscale" "$BASE/vpn/tailscaled"
touch "$BASE/vpn-enabled"

workers='broker firewall modem ap apn led led-autostart plate android-auto tailscale'
if test -d experimental; then
    cp -R experimental "$BASE/experimental"
    chmod 0700 "$BASE/experimental"
    if test -f experimental/ncc-native-setting-helper; then workers="$workers native"; fi
    if test -f experimental/ncc-native-wifi-sta-helper; then workers="$workers uplink uplink-settings"; fi
fi
cp -R plate "$BASE/plate"
chmod 0755 "$BASE/plate" "$BASE/plate/models"
chmod 0750 "$BASE/plate/plate-engine"
chmod 0644 "$BASE/plate/libonnxruntime.so.1.30.0" "$BASE/plate/models/"*.tflite "$BASE/plate/models/"*.onnx "$BASE/plate/models/"*.json

mkdir -p -m 0700 "$BASE/androidauto"
mkdir -p -m 0755 "$WANTS"
start_units=''
for name in web $workers; do
    case "$name" in
        web) unit=np1-control.service ;;
        uplink-settings) unit=np1-control-uplink-settings.path ;;
        *) unit=np1-control-$name.service ;;
    esac
    output="$BASE/units/$unit"
    sed -e 's/^Group=ncc$/Group=root/' -e "s/@NCC_UID@/$NCC_ID/g" "systemd/$unit" > "$output"
    case "$name" in
        web)
            sed -i -e 's/^User=ncc$/User=root/' \
                -e "s|^ExecStart=.*|ExecStart=/usr/bin/python3 $BASE/tools/run-owner-web.py $NCC_ID|" \
                -e 's/^CapabilityBoundingSet=.*/CapabilityBoundingSet=CAP_NET_BIND_SERVICE CAP_SETUID CAP_SETGID CAP_SETPCAP/' \
                -e '/^AmbientCapabilities=/d' "$output"
            ;;
        broker|ap|modem|plate|android-auto)
            runtime=/run/np1-control-$name
            if test "$name" = broker; then runtime=/run/np1-control; fi
            sed -i "/^RuntimeDirectoryMode=/a ExecStartPre=+/bin/chown root:$NCC_ID $runtime\nExecStartPre=+/bin/chmod g+s $runtime" "$output"
            ;;
    esac
    if test "$name" = ap; then sed -i "/^\[Unit\]\$/a ConditionPathExists=$BASE/private/ap/autostart" "$output"; fi
    chmod 0644 "$BASE/units/$unit"
    cp -p "$BASE/units/$unit" "$UNITS/$unit"
    cmp "$BASE/units/$unit" "/overlay$UNITS/$unit"
    # LED autostart and the first explicit uplink settings save have dedicated units.
    case "$name" in led|uplink) continue ;; esac
    ln -s "$UNITS/$unit" "$WANTS/$unit"
    test "$(readlink "/overlay$WANTS/$unit")" = "$UNITS/$unit"
    start_units="$start_units $unit"
done

systemctl daemon-reload
sh tools/pics-stock-rt-install.sh apply
cmp experimental/libncc-driverec-hook.so "$BASE/recorder-hook/libncc-driverec-hook.so"
cmp experimental/ncc-recorder-injector "$BASE/experimental/ncc-recorder-injector"
test ! -e "$RECORDER_DROPIN"
! systemctl show app-@DriveRec.service -p Environment --value | grep -Fq "LD_PRELOAD="
touch /run/xtables.lock
systemctl start $start_units
systemctl is-active --quiet np1-control-firewall.service
sh "$BASE/tools/ota-policy.sh" guard-check >/dev/null
sh tools/verify.sh
stock_units > "$BASE/recovery/stock-after.txt"
cmp "$BASE/recovery/stock-before.txt" "$BASE/recovery/stock-after.txt"
sha256sum -c "$BASE/recovery/stock-files.sha256"
sync
echo "Installed via owner recovery; baseline retained at $BASE/recovery"
