#!/bin/sh
# Stage/verify our helper, then exercise only pinned vendor GET and IPC connection.
set -eu
HERE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
sh "$HERE/native-preflight.sh"
exec /bin/sh "$HERE/native-setting-trial.sh" vendor-read
