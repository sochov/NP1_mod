#!/bin/sh
# Read-only inventory. Sent on SSH stdin; creates no file on the analysis PC.
set -u
section(){ printf '\n=== %s ===\n' "$1"; }
section identity
id
uname -a
section disk
df -Pk / /tmp "$HOME"
section interfaces
ip -br link
ip -4 route show default
section wifi-hardware
if command -v iw >/dev/null 2>&1; then iw dev; fi
for item in /sys/class/net/*; do
    test -e "$item/phy80211" || continue
    printf 'interface=%s\ndevice=' "${item##*/}"
    readlink -f "$item/device" || true
    printf 'driver='
    readlink -f "$item/device/driver" || true
done
section network-manager
if command -v nmcli >/dev/null 2>&1; then
    nmcli -t -f DEVICE,TYPE,STATE,CONNECTION device status
    nmcli -t -f NAME,UUID,TYPE,DEVICE connection show --active
    nmcli general permissions
fi
section radio-state
if command -v rfkill >/dev/null 2>&1; then rfkill list; fi
section bluetooth
if command -v bluetoothctl >/dev/null 2>&1; then bluetoothctl list; fi
section usb
if command -v lsusb >/dev/null 2>&1; then lsusb; fi
section wireless-processes
ps -eo pid,user,args | awk '/[h]ostapd|[w]pa_supplicant|[i]wd|[a]irodump|[a]ireplay|[w]ash|[t]cpdump|[t]shark|[d]umpcap|[w]ifite|[b]ettercap|[h]cxdumptool|[n]mcli|[i]w dev/ {print}'
section network-services
systemctl is-active NetworkManager wpa_supplicant iwd bluetooth 2>/dev/null || true
section network-devices
if command -v networkctl >/dev/null 2>&1; then networkctl list --no-pager; fi
section privileges
sudo -n true 2>/dev/null && printf 'passwordless_sudo=yes\n' || printf 'passwordless_sudo=no\n'
