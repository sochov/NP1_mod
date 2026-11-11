#!/usr/bin/env python3
"""Inspect/quarantine only Opening's proven stale single-instance lock.

Device Python 3.5 compatible; no vendor API, mutex write, unit or service change.
The default is read-only. A successful quarantine still requires a separate start
and functional/read-back verification. Original bytes remain in a private backup.
"""
import os
import sys
import time

PATH = '/tmp/victoria/victoria_Opening.map'
UNIT = 'app-@Opening.service'


def number(data, offset, size):
    value = 0
    for i in range(size):
        value |= data[offset + i] << (8 * i)
    return value


def stopped():
    pipe = os.popen('systemctl show app-@Opening.service -p MainPID -p ActiveState -p SubState')
    result = pipe.read()
    status = pipe.close()
    fields = dict(line.split('=', 1) for line in result.splitlines() if '=' in line)
    if status is not None or fields.get('MainPID') != '0' or fields.get('ActiveState') not in ('inactive', 'failed'):
        raise RuntimeError('Opening must be stopped with MainPID zero')


def fingerprint(info):
    return (info.st_dev, info.st_ino, info.st_size, info.st_uid, info.st_gid,
            info.st_mode, info.st_nlink, info.st_mtime_ns, info.st_ctime_ns)


def stale_owner(data):
    if len(data) != 128 or number(data, 0, 8) != 0:
        raise RuntimeError('Unexpected lock payload')
    owner = number(data, 24, 4)
    if (number(data, 16, 4) not in (1, 2) or number(data, 20, 4) != 1 or
            not owner or number(data, 28, 4) != 1 or number(data, 32, 4) != 129 or
            number(data, 112, 8) != 1):
        raise RuntimeError('Lock is not the proven stale single-owner format')
    return owner


def inspect():
    stopped()
    info = os.lstat(PATH)
    if (info.st_mode & 0o170000) != 0o100000 or info.st_size != 128 or info.st_nlink != 1:
        raise RuntimeError('Unexpected lock file type, size or link count')
    fd = os.open(PATH, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        if fingerprint(os.fstat(fd)) != fingerprint(info):
            raise RuntimeError('Lock file changed while opening')
        data = os.read(fd, 129)
    finally:
        os.close(fd)
    owner = stale_owner(data)
    if os.path.exists('/proc/' + str(owner)):
        raise RuntimeError('Mutex owner is alive')
    for pid in os.listdir('/proc'):
        if not pid.isdigit():
            continue
        base = '/proc/' + pid
        try:
            if os.path.basename(os.readlink(base + '/exe')) == 'Opening':
                raise RuntimeError('An Opening process still exists')
        except OSError:
            if os.path.exists(base + '/exe'):
                raise RuntimeError('Cannot inspect a live process executable')
        try:
            with open(base + '/maps', 'r', encoding='ascii', errors='replace') as stream:
                for line in stream:
                    if not line.strip():
                        continue
                    parts = line.split(None, 5)
                    address = parts[0].split('-')
                    # Linux exposes literal newlines in mapped file names. Such
                    # continuation lines have no mapping header or inode.
                    if len(address) != 2 or any(not part or any(c not in '0123456789abcdef' for c in part) for part in address):
                        continue
                    if len(parts) < 5:
                        raise RuntimeError('Malformed process mapping')
                    major, minor = parts[3].split(':')
                    same = (int(parts[4]) == info.st_ino and int(major, 16) == os.major(info.st_dev)
                            and int(minor, 16) == os.minor(info.st_dev))
                    if same or PATH in line:
                        raise RuntimeError('Lock file is still mapped')
        except OSError:
            if os.path.exists(base):
                raise RuntimeError('Cannot inspect a live process mapping')
        try:
            names = os.listdir(base + '/fd')
        except OSError:
            if os.path.exists(base):
                raise RuntimeError('Cannot inspect live process descriptors')
            continue
        for name in names:
            try:
                target = os.stat(base + '/fd/' + name)
            except OSError:
                # An individual descriptor can disappear between list and stat.
                if os.path.lexists(base + '/fd/' + name):
                    raise RuntimeError('Cannot inspect an open descriptor')
                continue
            if target.st_dev == info.st_dev and target.st_ino == info.st_ino:
                raise RuntimeError('Lock file has an open descriptor')
    stopped()
    if fingerprint(os.lstat(PATH)) != fingerprint(info):
        raise RuntimeError('Lock file changed during inspection')
    if os.path.exists('/proc/' + str(owner)):
        raise RuntimeError('Mutex owner appeared during inspection')
    return info, data


def main():
    if os.getuid() != 0 or sys.argv[1:] not in ([], ['--inspect'], ['--quarantine']):
        raise RuntimeError('Root and --inspect or --quarantine required')
    info, data = inspect()
    if sys.argv[1:] != ['--quarantine']:
        print('PASS: stopped Opening has an unreferenced stale single-instance lock')
        return
    backup = '/tmp/victoria/.ncc-opening-recovery-' + str(os.getpid()) + '-' + str(int(time.time()))
    os.mkdir(backup, 0o700)
    fd = os.open(backup + '/evidence.txt', os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        os.write(fd, ('opening-sync-recovery-v1\n' + repr(fingerprint(info)) + '\n').encode('ascii'))
        os.fsync(fd)
    finally:
        os.close(fd)
    # Repeat the complete checks immediately before atomic same-filesystem rename.
    current, current_data = inspect()
    if fingerprint(current) != fingerprint(info) or current_data != data:
        raise RuntimeError('Lock changed before quarantine')
    os.rename(PATH, backup + '/Opening.map')
    if os.path.lexists(PATH):
        raise RuntimeError('Lock unexpectedly recreated; inspect private evidence')
    print('PASS: stale Opening lock quarantined; separate start/read-back required')
    print('Private backup: ' + backup)


if __name__ == '__main__':
    try:
        main()
    except (OSError, RuntimeError, ValueError) as error:
        sys.stderr.write('Opening recovery refused: ' + str(error) + '\n')
        sys.exit(1)
