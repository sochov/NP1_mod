#!/bin/sh
# Old vendor adbd may not propagate shell exit codes; emit a machine-readable result.
set -u
case "${1:-}" in install|install-vpn|verify|rollback|uninstall) ;; *) exit 2;; esac
# This firmware exposes only the legacy PTY shell. Disable echo BEFORE the host sends credentials.
stty -echo || exit 3
printf 'NCC_STDIN_READY\n'
/bin/sh "$(dirname "$0")/$1.sh"
NCC_RESULT=$?
printf '\nNCC_EXIT_STATUS=%s\n' "$NCC_RESULT"
exit "$NCC_RESULT"
