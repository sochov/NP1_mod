"""Restore only the exact missing wlan0 default saved before the fixed trial."""
import os
import re
import subprocess
import socket

BASE = '/run/np1-native-apn-same-profile'
RECOVERY = '/run/np1-native-apn-network-recovery'
UNITS = ('app-@Communication', 'app-@DriveRec', 'app-@ImageRecog', 'app-@Voice', 'qmmf-server', 'NEventWatcherCom', 'app-@Setting')

def run(args):
    return subprocess.check_output(args, stderr=subprocess.DEVNULL, timeout=5).decode('utf-8').strip()

def private(path):
    st = os.lstat(path)
    if st.st_uid != 0 or st.st_mode & 0o170000 != 0o100000 or st.st_size > 16384:
        raise RuntimeError('Private recovery input invalid')
    with open(path, encoding='utf-8') as source:
        return source.read()

def ipv4(value):
    if not re.fullmatch(r'[0-9]{1,3}(\.[0-9]{1,3}){3}', value) or any(int(x) > 255 for x in value.split('.')):
        raise RuntimeError('Original IPv4 evidence invalid')
    return value

def status():
    # The vendor supplicant must traverse the parent to send its datagram.
    # Keep private evidence in 0700 RECOVERY, and this short-lived reply socket
    # beside the existing, proven Go VendorStatus sockets.
    local = '/run/np1-control/wpa-route-recovery'
    try:
        os.unlink(local)
    except FileNotFoundError:
        pass
    with open('/etc/group') as groups:
        gids = [int(line.split(':')[2]) for line in groups if line.startswith('NEventWatcherCom:')]
    if len(gids) != 1:
        raise RuntimeError('Vendor WiFi reply group unavailable')
    channel = socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM)
    try:
        channel.bind(local)
        os.chown(local, 0, gids[0]); os.chmod(local, 0o660)
        channel.settimeout(2)
        channel.connect('/run/wifi/wpa_supplicant/wlan0')
        channel.send(b'STATUS')
        raw = channel.recv(32769)
        if len(raw) > 32768:
            raise RuntimeError('Vendor WiFi status exceeds bound')
    finally:
        channel.close()
        os.unlink(local)
    values = {}
    for line in raw.decode('utf-8').splitlines():
        if '=' in line:
            key, value = line.split('=', 1)
            if key in values:
                raise RuntimeError('Duplicate WiFi state')
            values[key] = value
    return values

def decode_ssid(value):
    data = bytearray()
    i = 0
    while i < len(value):
        if value[i] == '\\':
            if value[i:i+2] == '\\x' and re.fullmatch('[0-9a-fA-F]{2}', value[i+2:i+4]):
                data.append(int(value[i+2:i+4], 16)); i += 4; continue
            if value[i:i+2] in ('\\\\', '\\"'):
                data.extend(value[i+1].encode('utf-8')); i += 2; continue
            raise RuntimeError('WiFi SSID encoding unsupported')
        data.extend(value[i].encode('utf-8')); i += 1
    return bytes(data).hex()

def stock():
    return ''.join(run(['systemctl', 'show', unit+'.service', '-p', 'MainPID', '-p', 'ActiveState'])+'\n' for unit in UNITS)

if os.geteuid() != 0:
    raise SystemExit('Root required')
expected = private(RECOVERY+'/expected-wifi.txt').splitlines()
if len(expected) != 4 or expected[0] != 'NCCWIFI1' or not re.fullmatch('[0-9a-f]{2,64}', expected[1]) or not re.fullmatch('[0-9]{1,6}', expected[2]):
    raise SystemExit('Original WiFi evidence invalid')
expected_ip = ipv4(expected[3])
baseline_stock = private(BASE+'/before.txt')
if stock() != baseline_stock:
    raise SystemExit('Stock service identity differs from trial baseline')
before_addresses = [line.split()[1] for line in private(BASE+'/before-addresses.txt').splitlines() if line.split()[0] == 'wlan0']
if len(before_addresses) != 1 or before_addresses[0].split('/')[0] != expected_ip:
    raise SystemExit('Original WiFi IPv4 evidence differs')
current_addresses = run(['/sbin/ip', '-o', '-4', 'address', 'show', 'dev', 'wlan0']).splitlines()
if len(current_addresses) != 1 or current_addresses[0].split()[3] != before_addresses[0]:
    raise SystemExit('Current WiFi address differs from original')

def same_wifi():
    current = status()
    return current.get('wpa_state') == 'COMPLETED' and current.get('id') == expected[2] and current.get('ip_address') == expected_ip and decode_ssid(current.get('ssid', '')) == expected[1]

if not same_wifi():
    raise SystemExit('Current WiFi profile differs from verified baseline')
defaults = [line.split() for line in private(BASE+'/before-routes.txt').splitlines() if line.startswith('default ')]
if len(defaults) != 1:
    raise SystemExit('Original default route is not unique')
route = defaults[0]
if len(route) < 5 or route[:2] != ['default', 'via'] or route[3:5] != ['dev', 'wlan0']:
    raise SystemExit('Original default route is unsupported')
ipv4(route[2])
tail = route[5:]
while tail:
    if len(tail) < 2:
        raise SystemExit('Original route option invalid')
    key, value = tail[:2]; tail = tail[2:]
    if key == 'proto' and value in ('dhcp', 'static', 'boot'):
        continue
    if key == 'metric' and re.fullmatch('[0-9]{1,9}', value):
        continue
    if key == 'src' and ipv4(value) == expected_ip:
        continue
    raise SystemExit('Original route option unsupported')
present = run(['/sbin/ip', '-4', 'route', 'show', 'default']).splitlines()
if present and [line.split() for line in present] != [route]:
    raise SystemExit('Competing default route exists; no mutation')
if not present:
    subprocess.run(['/sbin/ip', '-4', 'route', 'add']+route, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
if [line.split() for line in run(['/sbin/ip', '-4', 'route', 'show', 'default']).splitlines()] != [route] or not same_wifi() or stock() != baseline_stock:
    raise SystemExit('Default route recovery read-back failed')
print('Original WiFi default route restored; same profile, IPv4 and stock services verified')
