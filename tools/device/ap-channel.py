"""Read-only STA status using only modules available in the device's minimal Python.

Default output is a channel. --snapshot is a private comparison record containing
connection identity/IP, never a passphrase. Do not publish that output.
"""
import os
import socket
import sys
import time


def private_directory():
    # mkdir is atomic: an existing path belongs to another caller and is never reused.
    prefix = '/run/np1-ap-status-{0}-{1}-'.format(os.getpid(), int(time.time() * 1000000))
    for attempt in range(100):
        directory = prefix + str(attempt)
        try:
            os.mkdir(directory, 0o700)
            return directory
        except FileExistsError:
            continue
    raise RuntimeError('Unique reply directory unavailable')


def read_status():
    control = '/run/wifi/wpa_supplicant/wlan0'
    group = os.stat(control).st_gid
    directory = private_directory()
    local = directory + '/reply'
    stream = None
    bound = False
    try:
        os.chown(directory, -1, group)
        os.chmod(directory, 0o710)
        stream = socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM)
        stream.settimeout(3)
        stream.bind(local)
        bound = True
        os.chown(local, -1, group)
        os.chmod(local, 0o660)
        stream.connect(control)
        stream.send(b'STATUS')
        lines = stream.recv(16384).decode('utf8', 'replace').splitlines()
        return dict(line.split('=', 1) for line in lines if '=' in line)
    finally:
        if stream is not None:
            stream.close()
        try:
            if bound:
                os.unlink(local)
        finally:
            os.rmdir(directory)


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
    if args not in ([], ['--snapshot']):
        sys.stderr.write('Usage: ap-channel.py [--snapshot]\n')
        raise SystemExit(2)
    try:
        state = read_status()
        active_channel = channel(state)
        print(snapshot(state) if args else active_channel)
    except (OSError, RuntimeError) as error:
        # No raw STATUS, identifiers or credential values on errors.
        sys.stderr.write('STA status unavailable: {0}\n'.format(type(error).__name__))
        raise SystemExit(1)


if __name__ == '__main__':
    main()
