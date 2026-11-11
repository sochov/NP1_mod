#!/bin/sh
# Read-only investigation. No QMI open, AP start, config mutation or secret-file dump.
set -u
printf '=== control-related libraries ===\n'
find /usr/lib /usr/local/lib -maxdepth 1 -type f | grep -Ei 'wlan|mcom|sensor|comcommon|event|nipc|location' || true
printf '\n=== Wi-Fi capability helpers ===\n'
for binary in iw iwconfig iwpriv hostapd_cli busctl dbus-send gdbus; do command -v "$binary" || true; done
printf '\n=== active UNIX sockets ===\n'
cat /proc/net/unix
printf '\n=== system bus ===\n'
busctl --no-pager list || true
printf '\n=== config filenames (contents excluded) ===\n'
find /usr/local/etc/config /data -maxdepth 3 -type f 2>/dev/null | grep -Ei 'wifi|qcmap|hostap|modem|gps|apn' || true
printf '\n=== interfaces and ownership ===\n'
ls -l /sys/class/net/wlan*/phy80211
ps -ef | grep -E 'NEventWatcherCom|QCMAP|hostapd|dnsmasq|wpa_supplicant' || true
printf '\n=== TUN config ===\n'
zcat /proc/config.gz 2>/dev/null | grep -E 'CONFIG_TUN=|CONFIG_NETFILTER=|CONFIG_USB_NET|CONFIG_CFG80211=' || true
