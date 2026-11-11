#!/bin/sh
# Read-only inventory. Contains private data: write output only into an ignored tmp directory.
set -eu
printf '=== identity ===\n'
id
uname -a
cat /etc/os-release
printf '\n=== mounts ===\n'
cat /proc/mounts
df -hT /overlay /run /mnt/sdcard
printf '\n=== stock services ===\n'
systemctl --failed --no-pager
for unit in app-@Communication app-@DriveRec app-@ImageRecog app-@Voice qmmf-server QCMAP_ConnectionManagerd NEventWatcherCom; do
    systemctl show "$unit.service" -p ActiveState -p MainPID -p NRestarts -p ExecMainStatus
done
printf '\n=== network ===\n'
ip -br addr
ip route
wpa_cli -p /run/wifi/wpa_supplicant -i wlan0 status
printf '\n=== camera / usb / vpn ===\n'
ls -l /tmp/application/imagerecog/imagedata/*/imagedata/value
cat /sys/bus/platform/devices/a600000.ssusb/mode
ls -l /dev/net/tun /dev/qcqmi0
printf '\n=== candidate processes ===\n'
ps -ef | grep -E 'QCMAP|NEventWatcherCom|wpa_supplicant|hostapd|dnsmasq|tailscale' || true
