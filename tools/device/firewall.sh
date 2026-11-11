#!/bin/sh
# Own INPUT chain only. Do not change vendor policy, NAT, forwarding, DNS or unrelated rules.
set -eu
BASE=/overlay/owner-apps/np1-control
CHAIN=NCC_WEB_V1
case "${1:-}" in
apply)
    if ! iptables -w 5 -S "$CHAIN" >/dev/null 2>&1; then
        iptables-save > "$BASE/recovery/firewall-before.txt"
        iptables -w 5 -N "$CHAIN"
        touch "$BASE/firewall-owned"
    else
        test -f "$BASE/firewall-owned" || { echo 'Unmanaged chain; refusing replacement' >&2; exit 1; }
    fi
    iptables -w 5 -F "$CHAIN"
    for iface in wlan0 wlan1 bridge0 nccap0 tailscale0; do
        iptables -w 5 -A "$CHAIN" -i "$iface" -p tcp -m multiport --dports 80,443 -j ACCEPT
    done
    iptables -w 5 -A "$CHAIN" -i nccap0 -p udp --dport 67 -j ACCEPT
    # Existing recovery adbd; expose it only on the authenticated tailnet interface.
    iptables -w 5 -A "$CHAIN" -i tailscale0 -p tcp --dport 5556 -j ACCEPT
    if ! iptables -w 5 -C INPUT -j "$CHAIN" 2>/dev/null; then iptables -w 5 -I INPUT 1 -j "$CHAIN"; fi
    ;;
remove)
    if test -f "$BASE/firewall-owned"; then
        while iptables -w 5 -C INPUT -j "$CHAIN" 2>/dev/null; do iptables -w 5 -D INPUT -j "$CHAIN"; done
        if iptables -w 5 -S "$CHAIN" >/dev/null 2>&1; then iptables -w 5 -F "$CHAIN"; iptables -w 5 -X "$CHAIN"; fi
        rm -f "$BASE/firewall-owned"
    fi
    ;;
*) echo 'Expected apply or remove' >&2; exit 2;;
esac
