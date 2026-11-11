#!/bin/sh
# Thirty-second DHCP/AP experiment. Only nccap0 and private child processes may be changed.
# The Go worker captures stock PIDs/STA/routes before, during and after, and cleans on signals.
set -eu
umask 077
HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
STAGE=$(CDPATH= cd -- "$HERE/.." && pwd)
test "$(id -u)" = 0 || { echo 'Run as root' >&2; exit 1; }
test ! -e /sys/class/net/nccap0 || { echo 'nccap0 already exists; refusing to touch it' >&2; exit 1; }
for binary in iw ip hostapd hostapd_cli dnsmasq python3 systemctl mktemp cp chmod; do
    command -v "$binary" >/dev/null || { echo 'An AP/DHCP prerequisite is unavailable' >&2; exit 1; }
done
test -f "$STAGE/bin/ncc"
test -f "$HERE/ap-channel.py"
BASE=$(mktemp -d /run/np1-ap-dhcp-smoke.XXXXXX)
cp "$STAGE/bin/ncc" "$BASE/ncc"
cp "$HERE/ap-channel.py" "$BASE/ap-channel.py"
chmod 0700 "$BASE/ncc" "$BASE/ap-channel.py"
printf 'Private DHCP/AP smoke read-back: %s\n' "$BASE"
# Exec lets SIGTERM reach the worker directly; no daemon/PID guessing or shell echo of a PSK.
exec "$BASE/ncc" --mode ap-smoke
