#!/bin/sh
# Only the separate owned audio helper is staged; no service/config changes.
set -eu
umask 077
case "${1:-}:$#" in preflight:1|get:1) ;; roundtrip:3) case "$2:$3" in mute:[0-1]|volume-correction:[0-2]) ;; *) exit 64;; esac;; *) exit 64;; esac
stty -echo
printf 'NCC_STDIN_READY\n'
TEMP=
finish(){ result=$?; trap - 0; if test -n "$TEMP"; then rm -f "$TEMP"; fi; printf '\nNCC_EXIT_STATUS=%s\n' "$result"; exit "$result"; }
trap finish 0
test "$(id -u)" = 0
exec 8>/run/np1-native-audio-stage.lock
flock -n 8 || exit 75
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
BASE=/overlay/owner-apps/np1-control
test -d "$BASE" && test ! -L "$BASE" && test ! -L "$BASE/experimental"
cd "$STAGE"
sha256sum -c SHA256SUMS >/dev/null
test -f bin/ncc-native-audio-helper && test ! -L bin/ncc-native-audio-helper
mkdir -p "$BASE/experimental"
chmod 0700 "$BASE/experimental"
DEST=$BASE/experimental/ncc-native-audio-helper
MARKER=$BASE/experimental/.ncc-native-audio-owned
test ! -L "$DEST" && test ! -L "$MARKER"
if test -e "$MARKER"; then
 test -f "$MARKER" && test "$(cat "$MARKER")" = NCC_NATIVE_AUDIO_1
else
 test ! -e "$DEST" || exit 78
 ( set -C; printf 'NCC_NATIVE_AUDIO_1\n' > "$MARKER" )
 chmod 0600 "$MARKER"
fi
TEMP=$(mktemp "$BASE/experimental/.native-audio.XXXXXX")
cp bin/ncc-native-audio-helper "$TEMP"
chmod 0600 "$TEMP"
mv -f "$TEMP" "$DEST"
TEMP=
/bin/sh tools/native-audio-trial.sh "$@"
