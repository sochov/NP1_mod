#!/bin/sh
# Diagnostic read-only STATUS, with a temporary reply socket under our runtime directory.
set -eu
systemctl show np1-control-broker.service -p User -p Group -p SupplementaryGroups
ls -ld /run/wifi /run/wifi/wpa_supplicant /run/wifi/wpa_supplicant/wlan0
python3 - <<'PY'
import os
import socket
import pwd
local = '/run/np1-control/wpa-diagnostic'
stream = socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM)
try:
    stream.bind(local)
    os.chown(local, 0, pwd.getpwnam('NEventWatcherCom').pw_gid)
    os.chmod(local, 0o660)
    stream.settimeout(3)
    stream.connect('/run/wifi/wpa_supplicant/wlan0')
    stream.send(b'STATUS')
    value = stream.recv(8192).decode()
    for line in value.splitlines():
        if line.startswith(('wpa_state=', 'freq=', 'key_mgmt=')):
            print(line)
finally:
    stream.close()
    os.unlink(local)
PY
