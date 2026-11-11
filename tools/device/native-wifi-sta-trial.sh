#!/bin/sh
# Fixed stock STA owner trial. The stable Alexa/native worker is not replaced.
set -eu
umask 077
MODE=${1:-}
case "$MODE:$#" in preflight:1|get:1|roundtrip:1) ;; *) echo 'Usage: native-wifi-sta-trial.sh preflight|get|roundtrip' >&2; exit 64;; esac
test "$(id -u)" = 0
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$STAGE"
sha256sum -c SHA256SUMS >/dev/null
test -f bin/ncc-native-wifi-sta-helper && test ! -L bin/ncc-native-wifi-sta-helper
mkdir -p /run/np1-control-native
exec 8>/run/np1-control-native/control.lock
flock -n 8 || exit 75
exec 9>/run/np1-native-wifi-sta-trial.lock
flock -n 9 || exit 75
BASE=$(mktemp -d /run/np1-native-wifi-sta.XXXXXX)
chmod 0700 "$BASE"
cp bin/ncc-native-wifi-sta-helper "$BASE/helper"
chmod 0700 "$BASE/helper"
finish() {
 result=$?
 trap - 0
 trap '' HUP INT TERM
 set +e
 python3 "$STAGE/tools/native-wifi-sta-trial.py" cleanup "$BASE" || result=1
 rm -f "$BASE/helper"
 printf 'Private STA trial records: %s\n' "$BASE"
 printf 'NCC_EXIT_STATUS=%s\n' "$result"
 exit "$result"
}
trap finish 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
python3 "$STAGE/tools/native-wifi-sta-trial.py" "$MODE" "$BASE"
