#!/bin/sh
# A distinct, firmware-proven enum trial; launcher conditionally restores the original setting.
set -eu
HERE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
sh "$HERE/native-preflight.sh"
exec /bin/sh "$HERE/native-setting-trial.sh" roundtrip tailgating-sensitivity 0
