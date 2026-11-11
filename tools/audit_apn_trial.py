"""Compare private APN trial captures without printing APN or credential values.

Input files must already be in the ignored private workspace. This is an audit,
not an apply/restore authorization and never executes a device command.
"""
import argparse
import base64
import datetime
import json
import pathlib
import struct

VERSION = 'Quectel_Linux&Android_GobiNet_Driver_V1.6.2.14'
HASH = 'cbe36e78b6a174283c9757c2e1fe6e1da242e8cc2c465f19160b5a1205f6db72'

def instant(value):
    return datetime.datetime.fromisoformat(value.replace('Z', '+00:00'))

def capture(path):
    if path.stat().st_size > 16384:
        raise ValueError('Capture exceeds size bound')
    data = json.loads(path.read_text(encoding='utf-8'))
    if data['schemaVersion'] != 1 or data['driverVersion'] != VERSION or data['moduleSHA256'] != HASH:
        raise ValueError('Capture provenance differs')
    profiles = data['profiles']
    if len(profiles) != 2 or profiles[0]['index'] != 1 or profiles[1]['index'] != 2 or profiles[0]['status'] != 'available':
        raise ValueError('Capture profiles incomplete')
    raw = base64.b64decode(profiles[0]['frame'], validate=True)
    if not 7 <= len(raw) <= 4096 or raw[0] != 2 or struct.unpack_from('<HHH', raw, 1) != (5, 0x2b, len(raw)-7):
        raise ValueError('Capture response header invalid')
    fields = {}
    offset = 7
    while offset < len(raw):
        if offset+3 > len(raw):
            raise ValueError('Capture TLV header incomplete')
        kind, size = struct.unpack_from('<BH', raw, offset)
        offset += 3
        if kind in fields or offset+size > len(raw):
            raise ValueError('Capture TLV bounds invalid')
        fields[kind] = raw[offset:offset+size]
        offset += size
    if fields.get(2) != b'\0\0\0\0' or any(kind not in fields for kind in (0x11, 0x14, 0x1b, 0x1c, 0x1d)):
        raise ValueError('Capture required fields missing')
    return instant(data['observedAt']), fields

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--trial', type=pathlib.Path, required=True)
    parser.add_argument('--recovery', type=pathlib.Path, required=True)
    args = parser.parse_args()
    trial = json.loads(args.trial.read_text(encoding='utf-8-sig'))
    before = instant(trial['before']['observedAt'])
    captures = []
    paths = list(args.recovery.glob('profiles-*.json'))
    if not paths or len(paths) > 512:
        raise ValueError('Capture count invalid')
    for path in paths:
        captures.append(capture(path))
    captures.sort(key=lambda row: row[0])
    originals = [row for row in captures if row[0] <= before]
    subsequent = [row for row in captures if row[0] > before]
    if not originals or not subsequent:
        raise ValueError('Before/after captures not available')
    original, current = originals[-1][1], subsequent[-1][1]
    changed = sorted(kind for kind in set(original) | set(current) if original.get(kind) != current.get(kind))
    print(json.dumps({
        'profile1AllTLVsMatched': not changed,
        'originalFieldCount': len(original),
        'readbackFieldCount': len(current),
        'changedFieldIDs': [hex(kind) for kind in changed],
        'nativeInputFieldsMatched': all(original.get(kind) == current.get(kind) for kind in (0x11, 0x14, 0x1b, 0x1c, 0x1d)),
    }))

if __name__ == '__main__':
    try:
        main()
    except (ValueError, KeyError, IndexError, TypeError, OSError, struct.error):
        raise SystemExit('Private trial audit could not validate input; no values printed')
