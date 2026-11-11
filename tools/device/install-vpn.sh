#!/bin/sh
# Optional network change: adds tailscale0. No login, DNS override, routes or LAN binding yet.
set -eu
umask 077
BASE=/overlay/owner-apps/np1-control
STAGE=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
test -c /dev/net/tun
test -f "$BASE/enabled"
test -f "$STAGE/vpn/bin/tailscaled"
cd "$STAGE"
sha256sum -c SHA256SUMS
chmod 0755 vpn/bin/tailscale vpn/bin/tailscaled
vpn/bin/tailscale version
systemctl stop np1-control-tailscale.service || true
mkdir -p "$BASE/vpn" "$BASE/private/tailscale"
chmod 0700 "$BASE/vpn" "$BASE/private" "$BASE/private/tailscale"
if test -f "$BASE/state/tailscale/tailscaled.state" && test ! -f "$BASE/private/tailscale/tailscaled.state"; then
    cp -p "$BASE/state/tailscale/tailscaled.state" "$BASE/private/tailscale/tailscaled.state"
fi
cp vpn/bin/tailscale "$BASE/vpn/tailscale"
cp vpn/bin/tailscaled "$BASE/vpn/tailscaled"
cp systemd/np1-control-tailscale.service /etc/systemd/system/np1-control-tailscale.service
if test ! -f "$BASE/recovery/pre-vpn-postboot.sh"; then cp -p /etc/init.post_boot.sh "$BASE/recovery/pre-vpn-postboot.sh"; fi
python3 tools/postboot.py add-vpn
sh -n /etc/init.post_boot.sh
touch "$BASE/vpn-enabled"
systemctl daemon-reload
systemctl enable np1-control-tailscale.service
systemctl restart np1-control-tailscale.service
systemctl is-active np1-control-tailscale.service
/run/np1-tailscale/tailscale --socket=/run/np1-tailscale/tailscaled.sock status --json > /run/np1-tailscale/status.json || true
grep '"BackendState"' /run/np1-tailscale/status.json
