#!/bin/sh
# One fixed NAS query, only after /dev/qcqmi0 ownership has been inspected.
# No APN/PDP/registration writes. Raw result stays in /run.
set -eu
umask 077
mkdir -p /run/np1-modem-observer
python3 - <<'PY'
import os
import subprocess
for pid in os.listdir('/proc'):
    if not pid.isdigit():
        continue
    try:
        names=os.listdir('/proc/'+pid+'/fd')
    except OSError:
        continue
    for name in names:
        try:
            if os.readlink('/proc/'+pid+'/fd/'+name)=='/dev/qcqmi0':
                raise RuntimeError('QMI character device is already owned; query not started')
        except FileNotFoundError:
            pass
with open('/run/np1-modem-observer/qmi-signal.txt','w') as output:
    process=subprocess.Popen(['qmicli','-d','/dev/qcqmi0','--device-open-qmi','--nas-get-signal-info'],stdout=output,stderr=subprocess.STDOUT)
    try:
        result=process.wait(timeout=12)
    except subprocess.TimeoutExpired:
        process.terminate()
        try:
            process.wait(timeout=3)
        except subprocess.TimeoutExpired:
            process.kill();process.wait()
        raise RuntimeError('NAS query timed out; only our query process was stopped')
    print('NAS query exit status:',result)
PY
