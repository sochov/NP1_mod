#!/bin/sh
set -eu
HERE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
sh "$HERE/native-alexa-preflight.sh"
exec /bin/sh "$HERE/native-alexa-trial.sh" get
