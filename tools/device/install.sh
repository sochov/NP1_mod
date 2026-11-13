#!/bin/sh
# Targets: our /overlay tree, our two units, one marked late-boot block and a dedicated ncc account.
# Backup: recovery/<UTC timestamp>; rollback: /bin/sh tools/rollback.sh (from this bundle).
# Success: TLS responds with 401 unauthenticated, nonroot worker active, stock services unchanged.
set -eu
umask 077
BASE=/overlay/owner-apps/np1-control
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
test "$(id -u)" = 0
test "$(uname -m)" = aarch64
test "$(cat /proc/sys/kernel/osrelease)" = 4.14.206-perf || { echo 'Unexpected firmware kernel; inspect before installing' >&2; exit 1; }
grep -q '^VERSION_ID="202404231204"' /etc/os-release
test -f /etc/init.post_boot.sh
test ! -L "$BASE"
for cmd in sha256sum systemctl useradd python3; do command -v "$cmd" >/dev/null; done
cd "$STAGE"
sha256sum -c SHA256SUMS
/bin/chmod 0755 bin/ncc
bin/ncc --mode self-test
if test ! -d /overlay/owner-apps; then mkdir -m 0755 /overlay/owner-apps; fi
mkdir -p "$BASE/recovery" "$BASE/bin" "$BASE/config" "$BASE/state"
mkdir -p "$BASE/private/led"
mkdir -p "$BASE/private/apn"
mkdir -p "$BASE/private/wifi" "$BASE/private/ap"
mkdir -p "$BASE/private/ota-policy"
mkdir -p "$BASE/private/nas"
chmod 0700 "$BASE/private/nas"
chmod 0700 "$BASE/private/ota-policy"
chmod 0700 "$BASE/private/wifi" "$BASE/private/ap"
chmod 0700 "$BASE/private/apn"
chmod 0700 "$BASE/private" "$BASE/private/led"
chmod 0755 "$BASE" "$BASE/bin"
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
BACKUP="$BASE/recovery/$STAMP"
test ! -e "$BACKUP"
mkdir -m 0700 "$BACKUP"
cp -p /etc/init.post_boot.sh "$BACKUP/init.post_boot.sh"
cp -p /overlay/etc/init.post_boot.sh "$BACKUP/init.post_boot.upper.sh"
sha256sum /etc/init.post_boot.sh > "$BACKUP/original.sha256"
for unit in np1-control-ota-guard.service np1-control.service np1-control-broker.service np1-control-firewall.service np1-control-led.service np1-control-modem.service np1-control-native.service np1-control-ap.service np1-control-apn.service np1-control-plate.service; do
    if test -f "/etc/systemd/system/$unit"; then cp -p "/etc/systemd/system/$unit" "$BACKUP/$unit"; fi
done
if test -f "$BASE/bin/ncc"; then cp -p "$BASE/bin/ncc" "$BACKUP/ncc"; fi
if test -f "$BASE/config/config.json"; then cp -p "$BASE/config/config.json" "$BACKUP/config.json"; fi
if test -f "$BASE/tools/firewall.sh"; then cp -p "$BASE/tools/firewall.sh" "$BACKUP/firewall.sh"; fi
printf '%s\n' "$BACKUP" > "$BASE/recovery/latest"
trap 'code=$?; if test "$code" -ne 0; then echo "Install failed; restoring NCC only" >&2; sh "$STAGE/tools/rollback.sh" || true; fi' 0
if test ! -f "$BASE/private/ota-policy/ledger.json"; then touch "$BACKUP/ota-policy-initialized"; fi
bin/ncc --mode ota-policy --ota-action install
for unit in app-@Communication app-@DriveRec app-@ImageRecog app-@Voice qmmf-server NEventWatcherCom; do systemctl show "$unit.service" -p Id -p ActiveState -p SubState; done > "$BACKUP/stock-before.txt"
if ! id ncc >/dev/null 2>&1; then
    useradd -r -U -M -d /nonexistent -s /bin/false ncc
    touch "$BASE/recovery/account-created"
else
    test "$(awk -F: '$1 == "ncc" {print $6 ":" $7}' /etc/passwd)" = '/nonexistent:/bin/false'
fi
NCC_UID=$(id -u ncc)
if test ! -f "$BASE/config/config.json"; then
    bin/ncc --mode setup --config-dir "$BASE/config"
fi
bin/ncc --mode network-trust --config "$BASE/config/config.json"
chown ncc:ncc "$BASE/config" "$BASE/state"
chown ncc:ncc "$BASE/config/config.json" "$BASE/config/tls.pem" "$BASE/config/tls.key"
chmod 0700 "$BASE/config" "$BASE/state"
chmod 0600 "$BASE/config/config.json" "$BASE/config/tls.pem" "$BASE/config/tls.key"
cp bin/ncc "$BASE/bin/ncc.new"
chmod 0755 "$BASE/bin/ncc.new"
mv -f "$BASE/bin/ncc.new" "$BASE/bin/ncc"
cp systemd/np1-control.service /etc/systemd/system/np1-control.service
mkdir -p "$BASE/tools"
cp tools/*.sh tools/postboot.py "$BASE/tools/"
chmod 0700 "$BASE/tools"
cp systemd/np1-control-firewall.service /etc/systemd/system/np1-control-firewall.service
cp systemd/np1-control-led.service /etc/systemd/system/np1-control-led.service
cp systemd/np1-control-modem.service /etc/systemd/system/np1-control-modem.service
cp systemd/np1-control-apn.service /etc/systemd/system/np1-control-apn.service
cp systemd/np1-control-ap.service /etc/systemd/system/np1-control-ap.service
cp systemd/np1-control-ota-guard.service /etc/systemd/system/np1-control-ota-guard.service
cp tools/ap-channel.py "$BASE/tools/ap-channel.py"
python3 tools/postboot.py add-ap
if test -f experimental/ncc-stock-start-helper; then
    mkdir -p "$BASE/experimental"
    chmod 0700 "$BASE/experimental"
    cp experimental/ncc-stock-start-helper "$BASE/experimental/ncc-stock-start-helper.new"
    chmod 0600 "$BASE/experimental/ncc-stock-start-helper.new"
    mv -f "$BASE/experimental/ncc-stock-start-helper.new" "$BASE/experimental/ncc-stock-start-helper"
fi
if test -f experimental/ncc-native-setting-helper; then
    mkdir -p "$BASE/experimental"
    chmod 0700 "$BASE/experimental"
    cp experimental/ncc-native-setting-helper "$BASE/experimental/ncc-native-setting-helper.new"
    chmod 0600 "$BASE/experimental/ncc-native-setting-helper.new"
    mv -f "$BASE/experimental/ncc-native-setting-helper.new" "$BASE/experimental/ncc-native-setting-helper"
    cp systemd/np1-control-native.service /etc/systemd/system/np1-control-native.service
    if test -f experimental/ncc-native-alexa-helper; then
        cp experimental/ncc-native-alexa-helper "$BASE/experimental/ncc-native-alexa-helper.new"
        chmod 0600 "$BASE/experimental/ncc-native-alexa-helper.new"
        mv -f "$BASE/experimental/ncc-native-alexa-helper.new" "$BASE/experimental/ncc-native-alexa-helper"
    fi
    if test -f experimental/ncc-native-setting-extended-helper; then
        cp experimental/ncc-native-setting-extended-helper "$BASE/experimental/ncc-native-setting-extended-helper.new"
        chmod 0600 "$BASE/experimental/ncc-native-setting-extended-helper.new"
        mv -f "$BASE/experimental/ncc-native-setting-extended-helper.new" "$BASE/experimental/ncc-native-setting-extended-helper"
    fi
    if test -f experimental/ncc-native-setting-choice-helper; then
        cp experimental/ncc-native-setting-choice-helper "$BASE/experimental/ncc-native-setting-choice-helper.new"
        chmod 0600 "$BASE/experimental/ncc-native-setting-choice-helper.new"
        mv -f "$BASE/experimental/ncc-native-setting-choice-helper.new" "$BASE/experimental/ncc-native-setting-choice-helper"
    fi
    if test -f experimental/ncc-native-setting-notification-helper; then
        cp experimental/ncc-native-setting-notification-helper "$BASE/experimental/ncc-native-setting-notification-helper.new"
        chmod 0600 "$BASE/experimental/ncc-native-setting-notification-helper.new"
        mv -f "$BASE/experimental/ncc-native-setting-notification-helper.new" "$BASE/experimental/ncc-native-setting-notification-helper"
    fi
    if test -f experimental/ncc-native-audio-helper; then
        cp experimental/ncc-native-audio-helper "$BASE/experimental/ncc-native-audio-helper.new"
        chmod 0600 "$BASE/experimental/ncc-native-audio-helper.new"
        mv -f "$BASE/experimental/ncc-native-audio-helper.new" "$BASE/experimental/ncc-native-audio-helper"
    fi
    python3 tools/postboot.py add-native
fi
# Plate recognition: engine and models under our tree, sensor unit, post-boot
# start. The sensor stays idle until the owner enables recording in the UI.
if test -f plate/plate-engine && test -f plate/models/detector.tflite && test -f plate/models/ocr.tflite && test -f plate/models/plate-model.json; then
    mkdir -p "$BASE/plate"
    chmod 0755 "$BASE/plate"
    rm -rf "$BASE/plate/models.new" "$BASE/plate/licenses.new"
    cp -R plate/models "$BASE/plate/models.new"
    cp -R plate/licenses "$BASE/plate/licenses.new"
    chmod 0755 "$BASE/plate/models.new" "$BASE/plate/licenses.new"
    chmod 0644 "$BASE/plate/models.new/"* "$BASE/plate/licenses.new/"*
    for d in models licenses; do
        rm -rf "$BASE/plate/$d.old"
        if test -d "$BASE/plate/$d"; then mv "$BASE/plate/$d" "$BASE/plate/$d.old"; fi
        mv "$BASE/plate/$d.new" "$BASE/plate/$d"
        rm -rf "$BASE/plate/$d.old"
    done
    cp plate/plate-engine "$BASE/plate/plate-engine.new"
    chmod 0750 "$BASE/plate/plate-engine.new"
    mv -f "$BASE/plate/plate-engine.new" "$BASE/plate/plate-engine"
    cp systemd/np1-control-plate.service /etc/systemd/system/np1-control-plate.service
    python3 tools/postboot.py add-plate
fi
sed "s/@NCC_UID@/$NCC_UID/g" systemd/np1-control-broker.service > /etc/systemd/system/np1-control-broker.service
python3 tools/postboot.py add
python3 tools/postboot.py add-led
python3 tools/postboot.py add-modem
python3 tools/postboot.py add-apn
python3 tools/postboot.py add-ota-guard
sh -n /etc/init.post_boot.sh
touch "$BASE/enabled"
sync
systemctl daemon-reload
touch /run/xtables.lock
systemctl restart np1-control-firewall.service
systemctl restart np1-control-ota-guard.service
systemctl restart np1-control-modem.service
if test -f "$BASE/private/ap/autostart"; then systemctl restart np1-control-ap.service; fi
if test -f /etc/systemd/system/np1-control-native.service; then systemctl restart np1-control-native.service; fi
systemctl enable np1-control-broker.service np1-control.service
systemctl restart np1-control-broker.service
systemctl restart np1-control-apn.service
systemctl restart np1-control.service
if test -f /etc/systemd/system/np1-control-plate.service; then systemctl restart np1-control-plate.service; fi
sh tools/verify.sh
for unit in app-@Communication app-@DriveRec app-@ImageRecog app-@Voice qmmf-server NEventWatcherCom; do systemctl show "$unit.service" -p Id -p ActiveState -p SubState; done > "$BACKUP/stock-after.txt"
cmp "$BACKUP/stock-before.txt" "$BACKUP/stock-after.txt"
echo "Installed; recovery snapshot: $BACKUP"
