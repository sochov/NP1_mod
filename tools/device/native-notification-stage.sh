#!/bin/sh
# Copies only the separate owner helper, then invokes its fixed-selector trial.
# No stock services or the stable 07bcc/Alexa worker are stopped or replaced.
set -eu
umask 077
MODE=${1:-}
case "$MODE:$#" in preflight:1) ;; roundtrip:3) case "$2:$3" in front-car-start:[0-2]|signal-change:[0-2]) ;; *) exit 64;; esac;; *) exit 64;; esac
stty -echo
printf 'NCC_STDIN_READY\n'
TEMP=
finish(){ result=$?; trap - 0; if test -n "$TEMP"; then rm -f "$TEMP"; fi; printf '\nNCC_EXIT_STATUS=%s\n' "$result"; exit "$result"; }
trap finish 0
test "$(id -u)" = 0
exec 8>/run/np1-native-notification-stage.lock
flock -n 8 || { echo 'Another notification stage is active' >&2; exit 75; }
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
BASE=/overlay/owner-apps/np1-control
test -d "$BASE" && test ! -L "$BASE" && test ! -L "$BASE/experimental"
cd "$STAGE"
sha256sum -c SHA256SUMS >/dev/null
test -f bin/ncc-native-setting-notification-helper && test ! -L bin/ncc-native-setting-notification-helper
mkdir -p "$BASE/experimental"
chmod 0700 "$BASE/experimental"
DEST=$BASE/experimental/ncc-native-setting-notification-helper
MARKER=$BASE/experimental/.ncc-native-notification-owned
test ! -L "$DEST" && test ! -L "$MARKER"
if test -e "$MARKER"; then
 test -f "$MARKER" && test "$(cat "$MARKER")" = NCC_NATIVE_NOTIFICATION_1
else
 test ! -e "$DEST" || { echo 'Unmarked notification helper exists; refusing overwrite' >&2; exit 78; }
 ( set -C; printf 'NCC_NATIVE_NOTIFICATION_1\n' > "$MARKER" )
 chmod 0600 "$MARKER"
fi
TEMP=$(mktemp "$BASE/experimental/.native-notification.XXXXXX")
cp bin/ncc-native-setting-notification-helper "$TEMP"
chmod 0600 "$TEMP"
mv -f "$TEMP" "$DEST"
TEMP=
/bin/sh tools/native-notification-trial.sh "$@"
