#!/bin/sh
# Run the fixed-query Go probe; raw responses remain private.
set -eu
umask 077
mkdir -p /run/np1-modem-observer
if test -f /tmp/ncc-at-probe; then chmod 0755 /tmp/ncc-at-probe; PROBE=/tmp/ncc-at-probe; else PROBE=/run/np1-control/ncc; fi
"$PROBE" --mode at-probe > /run/np1-modem-observer/raw.json
echo 'Read-only AT queries completed; responses kept in private runtime file'
