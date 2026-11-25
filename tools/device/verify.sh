#!/bin/sh
set -eu
systemctl is-active np1-control-broker.service np1-control.service
systemctl is-active --quiet np1-control-ota-guard.service
systemctl is-active --quiet np1-control-native.service
PID=$(systemctl show np1-control.service -p MainPID --value)
test "$PID" -gt 1
BASE=/overlay/owner-apps/np1-control
if test -f "$BASE/recovery/owner-recovery-install"; then
    worker_uid=$(cat "$BASE/worker-uid")
else
    worker_uid=$(id -u ncc)
fi
test "$worker_uid" -gt 0
test "$(awk '/^Uid:/ {print $2}' /proc/"$PID"/status)" = "$worker_uid"
if test -f "$BASE/recovery/owner-recovery-install"; then
    awk -v uid="$worker_uid" '
        /^Uid:|^Gid:/ { if ($2 != uid || $3 != uid || $4 != uid || $5 != uid) exit 1 }
        /^Groups:/ { if (NF != 1) exit 1 }
        /^Cap(Eff|Bnd|Amb):/ { if ($2 !~ /^0*400$/) exit 1; caps++ }
        END { if (caps != 3) exit 1 }
    ' /proc/"$PID"/status
fi
test -S /run/np1-control/broker.sock
python3 - <<'PY'
import ssl
import time
import urllib.request
import urllib.error
ctx = ssl._create_unverified_context()
for url in ('http://127.0.0.1/api/v1/auth/session', 'https://127.0.0.1/api/v1/auth/session'):
    with urllib.request.urlopen(url, context=ctx, timeout=10) as response:
        if response.code != 200:
            raise RuntimeError('Password-free session failed')
with urllib.request.urlopen('https://127.0.0.1/', context=ctx, timeout=10) as response:
    if b'NP1 Control Center' not in response.read():
        raise RuntimeError('UI shell missing')
print('HTTP/HTTPS, password-free session and UI shell verified')
recorder = b''
for _ in range(30):
    try:
        with urllib.request.urlopen('http://127.0.0.1/api/v1/device/recorder-advanced', timeout=10) as response:
            payload = response.read(4097)
        if len(payload) > 4096:
            raise ValueError('recorder state is too large')
        recorder = b''.join(payload.split())
        configured = b'"configured":true' in recorder
        if (b'"available":true' in recorder and b'"hookLoaded":true' in recorder
                and b'"restartRequired":false' in recorder
                and (not configured or b'"applied":true' in recorder)):
            break
    except (OSError, ValueError, urllib.error.URLError):
        pass
    time.sleep(1)
else:
    raise RuntimeError('H.264 recorder hook was not loaded: ' + repr(recorder[:512]))
print('H.264 recorder hook is installed and loaded')
PY
systemctl show np1-control.service -p NRestarts -p Result -p ExecMainStatus
sh -n /etc/init.post_boot.sh
sh -n /overlay/etc/init.post_boot.sh
OTA_REPORT=/run/np1-control/ota-guard.json
test -f "$OTA_REPORT" && test ! -L "$OTA_REPORT"
ota_age=$(($(date +%s) - $(stat -c %Y "$OTA_REPORT")))
test "$ota_age" -ge -60 && test "$ota_age" -le 120 || { echo 'OTA guard report is stale' >&2; exit 1; }
grep -q '"managed":true' "$OTA_REPORT" || { echo 'OTA policy is not managed' >&2; exit 1; }
grep -q '"firmwareKnown":true' "$OTA_REPORT" || { echo 'OTA firmware baseline changed' >&2; exit 1; }
if grep -q '"wanted":true' "$OTA_REPORT"; then
    grep -q '"flag":{"present":true,"managed":true,"protected":true' "$OTA_REPORT" &&
        grep -q '"network":{"wanted":true,"active":true' "$OTA_REPORT" ||
        { echo 'OTA prevention layers are incomplete' >&2; exit 1; }
else
    grep -q '"flag":{"present":false,"managed":true' "$OTA_REPORT" &&
        grep -q '"network":{"wanted":false,"active":false' "$OTA_REPORT" ||
        { echo 'Explicit OTA allowance is inconsistent' >&2; exit 1; }
fi
echo 'OTA guard report is fresh and consistent'
if test -f "$BASE/recovery/owner-recovery-install"; then
    wants=$(systemctl show np1-owner-recovery.service -p Wants --value)
    for file in "$BASE"/units/*; do
        unit=${file##*/}
        case "$unit" in np1-control-led.service|np1-control-uplink.service) continue ;; esac
        case " $wants " in *" $unit "*) ;; *) echo "Missing owner boot dependency: $unit" >&2; exit 1 ;; esac
    done
else
    grep -q '^# BEGIN NP1_CONTROL_CENTER_V1$' /overlay/etc/init.post_boot.sh
fi
if test -f /etc/systemd/system/np1-control-android-auto.service; then
    for i in 1 2 3 4 5 6 7 8 9 10; do test -S /run/np1-control-android-auto/aa.sock && break; sleep 1; done
    systemctl is-active np1-control-android-auto.service
    test -S /run/np1-control-android-auto/aa.sock
    if test ! -f "$BASE/recovery/owner-recovery-install"; then
        grep -q '^# BEGIN NP1_CONTROL_CENTER_ANDROID_AUTO_V1$' /overlay/etc/init.post_boot.sh
    fi
fi
if test -f /etc/systemd/system/np1-control-plate.service; then
    for i in 1 2 3 4 5 6 7 8 9 10; do test -S /run/np1-control-plate/sensor.sock && break; sleep 1; done
    systemctl is-active np1-control-plate.service
    test -S /run/np1-control-plate/sensor.sock
    if test ! -f "$BASE/recovery/owner-recovery-install"; then
        grep -q '^# BEGIN NP1_CONTROL_CENTER_PLATE_V1$' /overlay/etc/init.post_boot.sh
    fi
fi
