#!/bin/sh
set -eu
umask 077
test "$(id -u)" = 0
BASE=/run/np1-native-apn-network-recovery
test ! -L "$BASE"
mkdir -p "$BASE"
test "$(stat -c %u "$BASE")" = 0
chmod 0700 "$BASE"
case "${1:-}" in
prepare) exit 0;;
restore)
    test -f "$BASE/expected-wifi.txt" && test ! -L "$BASE/expected-wifi.txt"
    chmod 0600 "$BASE/expected-wifi.txt"
    python3 "$(dirname "$0")/recover-apn-trial-default.py"
    ;;
*) exit 2;;
esac
