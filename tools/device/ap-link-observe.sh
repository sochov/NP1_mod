#!/bin/sh
# Read-only AP/link/listener observations. All raw metadata stays in private stage.
set -eu
stage=${1:?trial stage required}
case "$stage" in /run/ncc-ap-sharing-????????????) ;; *) exit 64 ;; esac
token=${stage#/run/ncc-ap-sharing-}
case "$token" in *[!0-9a-f]*) exit 64 ;; esac
i=0
while test "$i" -lt 36; do
    printf '\nSAMPLE %s ' "$i"
    date -u +%Y-%m-%dT%H:%M:%SZ
    for interface in nccap0 wlan0; do
        for field in ifindex carrier operstate flags; do
            printf '%s.%s=' "$interface" "$field"
            cat "/sys/class/net/$interface/$field" 2>/dev/null || true
        done
    done
    awk '$2 == "01E2A8C0:0050" && $4 == "0A" {print "AP_HTTP_LISTEN=1"}' /proc/net/tcp
    printf 'LOOPBACK_HTTP='
    python3 "$stage/ap-http-probe.py" loopback || true
    printf '\nLOCAL_AP_HTTP='
    python3 "$stage/ap-http-probe.py" ap || true
    printf '\nHOSTAPD_STATUS\n'
    hostapd_cli -s /run/np1-control-ap/private -p /run/np1-control-ap/private/ctrl -i nccap0 status || true
    printf 'HOSTAPD_STATIONS\n'
    hostapd_cli -s /run/np1-control-ap/private -p /run/np1-control-ap/private/ctrl -i nccap0 all_sta || true
    if test "$((i % 3))" -eq 0; then
        iw dev nccap0 info || true
        iw dev wlan0 info || true
        iw dev wlan0 get power_save || true
        iptables -w 2 -t filter -S || true
    fi
    i=$((i + 1))
    sleep 1
done > "$stage/link-observations.txt" 2>&1
