#!/bin/sh
# Exact, bounded WPA disconnection. Reconnect the original same-id/SSID on exit.
set -eu
stage=${1:?trial stage required}
case "$stage" in /run/ncc-ap-sharing-????????????) ;; *) exit 64 ;; esac
token=${stage#/run/ncc-ap-sharing-}
case "$token" in *[!0-9a-f]*) exit 64 ;; esac
helper=$stage/ap-upstream-loss.py
snapshot=$stage/sta-before.status
python3 "$helper" snapshot "$snapshot"
restore() {
    trap - EXIT HUP INT TERM
    if python3 "$helper" restore "$snapshot"; then
        printf 'NCC_STA_LOSS_RESTORED\n'
    else
        printf 'NCC_STA_LOSS_RESTORE_FAILED\n'
        exit 71
    fi
}
trap restore EXIT HUP INT TERM
python3 "$helper" disconnect "$snapshot"
printf 'holding\n' > "$stage/sta-loss-phase"
sleep 18
printf 'restoring\n' > "$stage/sta-loss-phase"
