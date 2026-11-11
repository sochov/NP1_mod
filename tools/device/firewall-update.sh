#!/bin/sh
# Reconcile only our dedicated INPUT chain. Existing recovery services are unchanged.
set -eu
BASE=/overlay/owner-apps/np1-control
HERE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
test "$(id -u)" = 0 && test -f "$BASE/firewall-owned"
cp "$BASE/tools/firewall.sh" "$BASE/recovery/firewall-before-tailnet-adb.sh"
cp "$HERE/firewall.sh" "$BASE/tools/firewall.sh"
chmod 0700 "$BASE/tools/firewall.sh"
sh "$BASE/tools/firewall.sh" apply
iptables -w 5 -C NCC_WEB_V1 -i tailscale0 -p tcp --dport 5556 -j ACCEPT
