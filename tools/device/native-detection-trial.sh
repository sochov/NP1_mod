#!/bin/sh
# Fixed detector enum trial. The launcher compares and restores the original value.
set -eu
HERE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
sh "$HERE/native-preflight.sh"
exec /bin/sh "$HERE/native-setting-trial.sh" roundtrip tailgating-detection 1
