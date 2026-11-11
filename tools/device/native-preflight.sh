#!/bin/sh
# Stage our own experimental helper only, then perform its vendor-free preflight.
set -eu
umask 077
BASE=/overlay/owner-apps/np1-control
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
test "$(id -u)" = 0
test -d "$BASE" && test ! -L "$BASE" && test ! -L "$BASE/experimental"
cd "$STAGE"
sha256sum -c SHA256SUMS >/dev/null
test -f experimental/ncc-native-setting-helper && test ! -L experimental/ncc-native-setting-helper
mkdir -p "$BASE/experimental"
chmod 0700 "$BASE/experimental"
cp experimental/ncc-native-setting-helper "$BASE/experimental/ncc-native-setting-helper.new"
chmod 0600 "$BASE/experimental/ncc-native-setting-helper.new"
mv -f "$BASE/experimental/ncc-native-setting-helper.new" "$BASE/experimental/ncc-native-setting-helper"
exec /bin/sh tools/native-setting-trial.sh preflight
