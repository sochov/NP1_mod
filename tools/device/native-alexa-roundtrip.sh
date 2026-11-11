#!/bin/sh
# Fixed Voice wake-word OFF trial, followed by conditional restoration to ON.
set -eu
HERE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
sh "$HERE/native-alexa-preflight.sh"
exec /bin/sh "$HERE/native-alexa-trial.sh" roundtrip 1
