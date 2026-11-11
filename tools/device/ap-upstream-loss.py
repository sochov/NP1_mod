#!/usr/bin/env python3
"""Fixed STATUS/DISCONNECT/RECONNECT control for the bounded AP uplink trial."""
import os
import socket
import sys
import time


def command(value):
    connection = socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM)
    try:
        connection.bind('\0ncc-ap-loss-' + str(os.getpid()) + '-' + str(int(time.time() * 1000000)))
        connection.settimeout(2)
        connection.sendto(value.encode(), '/run/wifi/wpa_supplicant/wlan0')
        raw = connection.recv(16385)
        if len(raw) > 16384:
            raise RuntimeError('STATUS bound exceeded')
        return raw.decode().strip()
    finally:
        connection.close()


def fields(raw):
    result = {}
    for line in raw.splitlines():
        if '=' in line:
            key, value = line.split('=', 1)
            result[key] = value
    return result


def main():
    if len(sys.argv) != 3 or sys.argv[1] not in ('snapshot', 'disconnect', 'restore'):
        return 64
    path = sys.argv[2]
    if not path.startswith('/run/ncc-ap-sharing-') or not path.endswith('/sta-before.status'):
        return 64
    operation = sys.argv[1]
    if operation == 'snapshot':
        raw = command('STATUS')
        value = fields(raw)
        if value.get('wpa_state') != 'COMPLETED' or 'id' not in value or 'ssid' not in value:
            return 65
        descriptor = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        with os.fdopen(descriptor, 'w') as output:
            output.write(raw)
        return 0
    if operation == 'disconnect':
        if command('DISCONNECT') != 'OK':
            return 66
        deadline = time.time() + 5
        while time.time() < deadline:
            if fields(command('STATUS')).get('wpa_state') in ('DISCONNECTED', 'INACTIVE'):
                return 0
            time.sleep(0.2)
        return 67
    with open(path) as original:
        before = fields(original.read(16385))
    if command('RECONNECT') != 'OK':
        return 68
    deadline = time.time() + 25
    while time.time() < deadline:
        current = fields(command('STATUS'))
        if current.get('wpa_state') == 'COMPLETED' and current.get('id') == before.get('id') and current.get('ssid') == before.get('ssid'):
            return 0
        time.sleep(0.5)
    return 69


try:
    sys.exit(main())
except Exception:
    # Preserve no SSID/address/control response in the console.
    sys.exit(70)
