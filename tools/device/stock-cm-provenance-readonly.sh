#!/bin/sh
# Fixed /proc/sysfs/executable inspection only. Never opens the modem device,
# changes STA/AP, or starts/stops any own or stock service.
set -eu
umask 077
test "$#" = 0
test "$(id -u)" = 0
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$STAGE"
sha256sum -c SHA256SUMS >/dev/null
test -f bin/ncc && test ! -L bin/ncc
BASE=$(mktemp -d /run/np1-stock-cm-proof.XXXXXX)
chmod 0700 "$BASE"
cp bin/ncc "$BASE/ncc"
chmod 0700 "$BASE/ncc"
finish() {
 result=$?
 trap - 0
 trap '' HUP INT TERM
 rm -f "$BASE/ncc"
 printf 'Private CM provenance records: %s\n' "$BASE"
 printf 'NCC_EXIT_STATUS=%s\n' "$result"
 exit "$result"
}
trap finish 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
timeout -t 15 -s TERM "$BASE/ncc" --mode stock-cm-ownership-readonly >"$BASE/proof.json" 2>"$BASE/error-private.txt"
cat "$BASE/proof.json"
