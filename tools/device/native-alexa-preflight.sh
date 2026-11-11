#!/bin/sh
# Stage only our separate Alexa ELF and execute its vendor-free preflight.
set -eu
umask 077
BASE=/overlay/owner-apps/np1-control
STAGE=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
test "$(id -u)" = 0
cd "$STAGE"
sha256sum -c SHA256SUMS >/dev/null
test ! -L "$BASE" && test ! -L "$BASE/experimental"
test -f experimental/ncc-native-alexa-helper && test ! -L experimental/ncc-native-alexa-helper
mkdir -p "$BASE/experimental"
chmod 0700 "$BASE/experimental"
cp experimental/ncc-native-alexa-helper "$BASE/experimental/ncc-native-alexa-helper.new"
chmod 0600 "$BASE/experimental/ncc-native-alexa-helper.new"
mv -f "$BASE/experimental/ncc-native-alexa-helper.new" "$BASE/experimental/ncc-native-alexa-helper"
exec /bin/sh tools/native-alexa-trial.sh preflight
