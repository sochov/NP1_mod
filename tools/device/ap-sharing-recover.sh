#!/bin/sh
# Retry journal-verified NCC-only cleanup using the supplied fixed trial binary.
set -eu
stage=${1:?trial stage required}
case "$stage" in /run/ncc-ap-sharing-????????????) ;; *) exit 64 ;; esac
token=${stage#/run/ncc-ap-sharing-}
case "$token" in *[!0-9a-f]*) exit 64 ;; esac
test -f "$stage/ncc"
"$stage/ncc" --mode ap-cleanup
systemctl start np1-control-ap.service
systemctl is-active --quiet np1-control-ap.service
printf 'NCC sharing resources cleaned and original AP started\n'
