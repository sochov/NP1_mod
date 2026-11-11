#!/bin/sh
# Synthetic input only: one separate V4L2 codec context, no camera/QMMF source or SD writes.
# Verify stock capture before and after. Do not invoke QCMAP_CLI or any stock control command.
set -eu
# The previous experiment failed S_FMT and an unexplained reboot was later observed.
# Retain the exact reproducer for diagnosis; do not repeat it on the running device.
echo 'Disabled: codec experiment needs reboot-cause investigation before another run' >&2
exit 2
umask 077
BASE=/run/np1-encoder-smoke
mkdir -p "$BASE"
for unit in app-@DriveRec app-@ImageRecog qmmf-server; do systemctl show "$unit.service" -p MainPID -p ActiveState; done > "$BASE/before.txt"
python3 - <<'PY'
import os
import signal
import subprocess
import time
command = ['gst-launch-1.0', '-q', 'videotestsrc', 'num-buffers=30', '!',
           'video/x-raw,format=NV12,width=640,height=360,framerate=5/1', '!',
           'v4l2h264enc', '!', 'video/x-h264,profile=baseline', '!',
           'h264parse', '!', 'mp4mux', 'fragment-duration=1000', 'streamable=true', '!',
           'filesink', 'location=/run/np1-encoder-smoke/test.mp4']
with open('/run/np1-encoder-smoke/encoder.log','w') as output:
    process = subprocess.Popen(command, stdout=output, stderr=subprocess.STDOUT)
    try:
        result = process.wait(timeout=20)
    except subprocess.TimeoutExpired:
        process.terminate()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()
        raise RuntimeError('Encoder timeout; stopped only our synthetic pipeline')
    if result != 0:
        raise RuntimeError('Encoder did not accept the synthetic pipeline; inspect encoder.log')
print('Synthetic H.264 MP4 bytes:', os.stat('/run/np1-encoder-smoke/test.mp4').st_size)
PY
for unit in app-@DriveRec app-@ImageRecog qmmf-server; do systemctl show "$unit.service" -p MainPID -p ActiveState; done > "$BASE/after.txt"
cmp "$BASE/before.txt" "$BASE/after.txt"
echo 'Stock camera service PIDs/state unchanged'
