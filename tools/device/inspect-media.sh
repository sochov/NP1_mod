#!/bin/sh
# Read-only camera/audio/recording inventory. No new QMMF session or exclusive capture.
set -u
printf '=== export metadata ===\n'
for camera in frontcamera incamera; do
    for key in rows cols size; do printf '%s %s: ' "$camera" "$key"; head -c 8 "/tmp/application/imagerecog/imagedata/$camera/imagedata/$key/value"; printf '\n'; done
done
printf '\n=== latest recordings ===\n'
ls -lt /mnt/sdcard/DRIVERECDATA/Video | head -n 9
printf '\n=== available encoders/sinks ===\n'
gst-inspect-1.0 | grep -Ei '264|265|qti|qmmf|rtsp|rtp|opus|aac|pulse|alsa|appsink|interpipe|shm' || true
printf '\n=== audio ===\n'
cat /proc/asound/cards
command -v pactl
command -v parec
printf '\n=== recording descriptors ===\n'
PID=$(systemctl show app-@DriveRec.service -p MainPID --value)
ls -l /proc/"$PID"/fd | grep -Ei 'mp4|sdcard|socket|pipe' | head -n 25 || true
