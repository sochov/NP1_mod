#!/bin/sh
# Old vendor adbd may not propagate shell exit codes; emit a machine-readable result.
set -u
case "${1:-}" in install|install-vpn|verify|rollback|uninstall|stock-start-install|nas-backup-install|web-worker-update|native-worker-update|modem-worker-update|ap-worker-update|ap-smoke|ap-dhcp-smoke|gobinet-readonly|gobinet-status-readonly|gobinet-profiles-readonly|gobinet-profile-list-readonly|native-apn-status-readonly|native-apn-network-readonly|native-apn-same-profile-trial|native-apn-route-recover|apn-editor-seed|native-preflight|native-alexa-preflight|native-alexa-get|native-alexa-roundtrip|native-vendor-read|native-sensitivity-trial|native-detection-trial|firewall-update) ;; *) exit 2;; esac
# This firmware exposes only the legacy PTY shell. Disable echo BEFORE the host sends credentials.
stty -echo || exit 3
printf 'NCC_STDIN_READY\n'
/bin/sh "$(dirname "$0")/$1.sh"
NCC_RESULT=$?
printf '\nNCC_EXIT_STATUS=%s\n' "$NCC_RESULT"
exit "$NCC_RESULT"
