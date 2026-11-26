#!/bin/sh
# Keep the newest plate-engine recovery snapshot and the installer's recovery target.
set -eu

RECOVERY=/overlay/owner-apps/np1-control/recovery
test "$(id -u)" = 0
test -d "$RECOVERY" && test "$(readlink -f "$RECOVERY")" = "$RECOVERY"
exec 9>"$RECOVERY/.plate-engine-update.lock"
flock -n 9

keep=${1:?Specify the newest plate recovery directory to retain}
shift
test "$#" -gt 0
latest=$(cat "$RECOVERY/latest")
for name in "$keep" "$@"; do
    case "$name" in plate-update-????????) ;; *) exit 1 ;; esac
    suffix=${name#plate-update-}
    case "$suffix" in *[!A-Za-z0-9]*) exit 1 ;; esac
    path=$RECOVERY/$name
    test -d "$path" && test ! -L "$path" && test "$(readlink -f "$path")" = "$path"
    test -f "$path/plate-engine-update.sh" && test -f "$path/SHA256SUMS"
done
for name do
    path=$RECOVERY/$name
    test "$name" != "$keep" && test "$path" != "$latest"
    test -z "$(find "$path" -maxdepth 0 -newer "$RECOVERY/$keep" -print)"
done
for name do
    rm -rf -- "$RECOVERY/$name"
    test ! -e "$RECOVERY/$name"
    printf 'Removed plate recovery snapshot: %s\n' "$name"
done
df -k /overlay
