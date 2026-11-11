#!/bin/sh
# Read-only inspection; does not create a camera session, capture audio or open ALSA exclusively.
set -u
printf '=== codec nodes ===\n'
for entry in /sys/class/video4linux/*; do printf '%s ' "$entry"; cat "$entry/name"; done
printf '\n=== H264 encoder ===\n'
gst-inspect-1.0 v4l2h264enc
printf '\n=== audio source ===\n'
pactl --server=unix:/var/run/pulse/native list short sources
printf '\n=== AAC/Opus/mux ===\n'
for plugin in avenc_aac voaacenc opusenc oggmux mp4mux rawvideoparse; do gst-inspect-1.0 "$plugin" | head -n 15; done
printf '\n=== owner LED service ===\n'
systemctl is-active np1-front-led-rainbow.service || true
printf '\n=== LTE API ===\n'
ls -l /usr/lib/liblteapi.so* /usr/local/lib/liblteapi.so* 2>/dev/null
printf '\n=== shared telemetry filenames ===\n'
find /tmp/system -maxdepth 3 -type f 2>/dev/null | head -n 80
