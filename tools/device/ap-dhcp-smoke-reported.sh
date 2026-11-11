#!/bin/sh
# ADB shells on older firmware may not propagate the remote exit status.
# Preserve it as an explicit result line, while forwarding signals to our child.
set -u
test "$#" = 1 || exit 64
sh "$1" &
trial_child=$!
trap 'kill -TERM "$trial_child" 2>/dev/null || true' INT TERM HUP
wait "$trial_child"
trial_code=$?
printf '\nNCC_AP_TRIAL_EXIT=%s\n' "$trial_code"
exit "$trial_code"
