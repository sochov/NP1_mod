#!/usr/bin/python3
"""Read-only bounded recording/container inventory. stdout is private evidence."""
import os
import sys
import time

ROOT = '/mnt/sdcard'
LIMIT = 12000
entries = []
extensions = {}
walk_count = 0
truncated = False
for directory, dirs, files in os.walk(ROOT):
    depth = directory[len(ROOT):].count('/')
    if depth >= 4:
        dirs[:] = []
    dirs.sort()
    for name in files:
        walk_count += 1
        if walk_count > LIMIT:
            truncated = True
            break
        path = directory + '/' + name
        try:
            info = os.lstat(path)
            if info.st_mode & 0o170000 != 0o100000:
                continue
            extension = os.path.splitext(name)[1].lower()
            extensions[extension] = extensions.get(extension, 0) + 1
            entries.append((info.st_mtime, path, info.st_size))
        except OSError:
            pass
    if truncated:
        break

def bounded(path, offset, size):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        os.lseek(fd, offset, 0)
        return os.read(fd, size)
    finally:
        os.close(fd)

def number(data):
    out = 0
    for value in bytearray(data):
        out = (out << 8) | value
    return out

def atoms(path, size):
    result = []
    at = 0
    for index in range(64):
        if at + 8 > size:
            break
        head = bounded(path, at, min(16, size-at))
        length = number(head[:4])
        kind = head[4:8].decode('ascii', 'replace')
        header = 8
        if length == 1:
            if len(head) < 16:
                break
            length = number(head[8:16])
            header = 16
        result.append((at, length, kind, head.hex()))
        if length == 0 or length < header or at + length > size:
            break
        at += length
    return result

entries.sort(reverse=True)
mp4 = [entry for entry in entries if entry[1].lower().endswith('.mp4')][:6]
selected = []
for modified, path, size in mp4:
    try:
        selected.append({'path': path, 'size': size, 'modified': modified,
            'header': bounded(path, 0, min(512,size)).hex(),
            'tail': bounded(path, max(0,size-512),min(512,size)).hex(),
            'atoms': atoms(path,size)})
    except OSError:
        pass
recent = [entry for entry in entries if not entry[1].lower().endswith('.mp4')][:48]
processes = []
for name in os.listdir('/proc'):
    if not name.isdigit():
        continue
    try:
        if os.readlink('/proc/'+name+'/exe') != '/usr/local/bin/DriveRec':
            continue
        fds = []
        for descriptor in os.listdir('/proc/'+name+'/fd'):
            try:
                target = os.readlink('/proc/'+name+'/fd/'+descriptor)
                if target.startswith(ROOT) or any(word in target.lower() for word in ('moov','idx','h264','rtsp','rtp','record')):
                    fds.append((descriptor,target))
            except OSError:
                pass
        processes.append({'pid':name,'fds':fds})
    except OSError:
        pass
time.sleep(1)
for selected_file in selected:
    try:
        after = os.stat(selected_file['path'])
        selected_file['sizeAfter'] = after.st_size
        selected_file['modifiedAfter'] = after.st_mtime
        selected_file['atomsAfter'] = atoms(selected_file['path'],after.st_size)
    except OSError:
        selected_file['afterUnavailable'] = True
print(repr({'capturedAt':time.time(),'scanned':walk_count,'truncated':truncated,
    'extensions':extensions,'recentNonMP4':recent,'mp4':selected,'processes':processes}))
