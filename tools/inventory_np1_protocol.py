"""Inventory embedded protobuf descriptors without executing the NP1 binary.

Requires google.protobuf on the analysis host. Output defaults to ignored tmp/.
No device connection, packet transmission, credential extraction or setter calls.
"""
import argparse
import hashlib
import json
import pathlib
import re

from google.protobuf import descriptor_pb2
from google.protobuf.message import DecodeError

MAX_DESCRIPTOR = 256 * 1024
NAMES = re.compile(rb'\x0a([\x01-\x7f])([A-Za-z0-9_./-]{1,125}\.proto)')


def varint(data, offset, limit):
    value = 0
    for shift in range(0, 70, 7):
        if offset >= limit:
            raise ValueError('truncated varint')
        byte = data[offset]
        offset += 1
        value |= (byte & 127) << shift
        if not byte & 128:
            if value > (1 << 64) - 1:
                raise ValueError('overflow varint')
            return value, offset
    raise ValueError('unbounded varint')


def descriptor_end(data, start):
    limit = min(len(data), start + MAX_DESCRIPTOR)
    offset, previous = start, 0
    while offset < limit:
        begin = offset
        key, offset = varint(data, offset, limit)
        field, wire = key >> 3, key & 7
        if not 1 <= field <= 14 or field < previous or wire not in (0, 2):
            return begin
        previous = field
        if wire == 0:
            _, offset = varint(data, offset, limit)
        else:
            size, offset = varint(data, offset, limit)
            if size > MAX_DESCRIPTOR or offset + size > limit:
                raise ValueError('invalid descriptor boundary')
            offset += size
    if offset == len(data):
        return offset
    raise ValueError('descriptor exceeds bound')


def enum_summary(enum):
    return {'name': enum.name, 'values': [{'name': v.name, 'number': v.number} for v in enum.value]}


def message_summary(message):
    return {'name': message.name,
            'fields': [{'name': f.name, 'number': f.number,
                        'type': descriptor_pb2.FieldDescriptorProto.Type.Name(f.type),
                        'typeName': f.type_name or None,
                        'label': descriptor_pb2.FieldDescriptorProto.Label.Name(f.label)}
                       for f in message.field],
            'enums': [enum_summary(e) for e in message.enum_type],
            'messages': [message_summary(m) for m in message.nested_type]}


def extract(data):
    results, seen = [], set()
    for match in NAMES.finditer(data):
        if match.group(1)[0] != len(match.group(2)):
            continue
        try:
            end = descriptor_end(data, match.start())
            descriptor = descriptor_pb2.FileDescriptorProto()
            descriptor.ParseFromString(data[match.start():end])
            if descriptor.name != match.group(2).decode('ascii'):
                continue
            if not descriptor.message_type and not descriptor.enum_type:
                continue
            if descriptor.name in seen:
                continue
            seen.add(descriptor.name)
            results.append({'name': descriptor.name, 'package': descriptor.package,
                            'offset': hex(match.start()), 'length': end - match.start(),
                            'messages': [message_summary(m) for m in descriptor.message_type],
                            'enums': [enum_summary(e) for e in descriptor.enum_type]})
        except (ValueError, DecodeError):
            continue
    return results


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('binaries', nargs='+', type=pathlib.Path)
    parser.add_argument('--out', type=pathlib.Path, default=pathlib.Path('tmp/companion-protocol-inventory.json'))
    args = parser.parse_args()
    results = []
    for path in args.binaries:
        if path.stat().st_size > 64 * 1024 * 1024:
            parser.error('input exceeds 64 MiB')
        data = path.read_bytes()
        descriptors = extract(data)
        results.append({'binary': path.name, 'sha256': hashlib.sha256(data).hexdigest(), 'descriptors': descriptors})
        print('{0}: {1} embedded descriptors'.format(path.name, len(descriptors)))
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding='utf-8')
    print('Private analysis output:', args.out)


if __name__ == '__main__':
    main()
