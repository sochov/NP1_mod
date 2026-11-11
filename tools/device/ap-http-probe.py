#!/usr/bin/env python3
"""Bounded fixed HTTP probe using only modules in the NP1 minimal Python."""
import socket
import sys

if len(sys.argv) != 2 or sys.argv[1] not in ('loopback', 'ap'):
    sys.exit(64)
address = '127.0.0.1' if sys.argv[1] == 'loopback' else '192.168.226.1'
connection = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
try:
    connection.settimeout(0.7)
    connection.connect((address, 80))
    connection.sendall(('GET / HTTP/1.1\r\nHost: ' + address + '\r\nConnection: close\r\n\r\n').encode())
    response = connection.recv(2048).split(b'\r\n', 1)[0].split(b' ')
    if len(response) >= 2 and response[1] == b'200':
        print('200')
    else:
        print('000')
except Exception:
    print('000')
finally:
    connection.close()
