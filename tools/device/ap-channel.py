"""Read-only STA status using only modules available in the device's minimal Python.

Default output is a channel. --snapshot is a private comparison record containing
connection identity/IP, never a passphrase. Do not publish that output.
"""
import os
import socket
import sys
import time


def read_status():
    control = '/run/wifi/wpa_supplicant/wlan0'
    # Linux abstract UNIX sockets avoid filesystem writes outside the unit's
    # own ReadWritePaths, and let the vendor reply without traversing our 0750
    # runtime directory. Access to the vendor socket still requires its group.
    stream = socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM)
    try:
        stream.settimeout(3)
        prefix = '\0ncc-ap-status-{0}-{1}-'.format(os.getpid(), int(time.time() * 1000000))
        for attempt in range(100):
            try:
                stream.bind(prefix + str(attempt))
                break
            except OSError as error:
                if error.errno != 98:  # EADDRINUSE, no optional errno module needed.
                    raise
        else:
            raise RuntimeError('Unique reply address unavailable')
        stream.connect(control)
        stream.send(b'STATUS')
        lines = stream.recv(16384).decode('utf8', 'replace').splitlines()
        return dict(line.split('=', 1) for line in lines if '=' in line)
    finally:
        stream.close()


def channel(state):
    try:
        frequency = int(state.get('freq', '0'))
    except ValueError:
        raise RuntimeError('Associated 2.4GHz STA channel unavailable')
    if state.get('wpa_state') != 'COMPLETED' or not 2412 <= frequency <= 2472 or (frequency - 2407) % 5:
        raise RuntimeError('Associated 2.4GHz STA channel unavailable')
    return (frequency - 2407) // 5


def snapshot(state):
    # ASCII escaping handles non-ASCII SSIDs even with an ASCII-only device stdout.
    keys = ('wpa_state', 'freq', 'bssid', 'ssid', 'id', 'ip_address')
    return '\n'.join(key + '=' + ascii(state.get(key)) for key in sorted(keys))


def main():
    args = sys.argv[1:]
    if args in (['--help'], ['-h']):
        print('Usage: ap-channel.py [--snapshot] (read-only associated 2.4GHz STA status)')
        return
    if args not in ([], ['--snapshot'], ['--ap-state']):
        sys.stderr.write('Usage: ap-channel.py [--snapshot]\n')
        raise SystemExit(2)
    try:
        state = read_status()
        if args == ['--snapshot']:
            print(snapshot(state))
        elif args == ['--ap-state']:
            if state.get('wpa_state') in ('DISCONNECTED', 'INACTIVE'):
                print('disconnected')
            elif state.get('wpa_state') == 'COMPLETED':
                print('associated {0}'.format(channel(state)))
            else:
                print('unavailable')
        else:
            print(channel(state))
    except (OSError, RuntimeError) as error:
        # No raw STATUS, identifiers or credential values on errors.
        sys.stderr.write('STA status unavailable: {0}\n'.format(type(error).__name__))
        raise SystemExit(1)


if __name__ == '__main__':
    main()
