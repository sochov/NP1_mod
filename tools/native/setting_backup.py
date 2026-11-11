"""Read only the two proven settings from the pinned stock backup format.

Native ordinal 50/51 maps to persistent key 506/507. The disk records are
sorted by persistent key, so ordinal indexing is incorrect. Never write this
file or expose unrelated settings. Host tool; no vendor ABI is executed.
"""
import argparse
import pathlib
import struct

PERSISTENT_IDS = (
    465, 466, 467, 490, 491, 492, 511, 512, 529, 30, 31, 32, 51, 53,
    351, 352, 353, 354, 355, 357, 364, 365, 468, 469, 470, 471, 472,
    473, 474, 475, 476, 479, 480, 481, 482, 483, 484, 493, 494, 495,
    496, 497, 498, 499, 500, 501, 502, 503, 504, 505, 506, 507, 508,
    509, 510, 513, 514, 515, 516, 517, 518, 519, 520, 521, 522, 523,
    524, 525, 526, 527, 528, 530, 531, 532, 533, 534, 535, 536, 537,
    538, 539, 540, 541, 542, 543, 544,
)


def _records(data):
    if len(data) != 704:
        raise ValueError('unsupported backup size')
    marker, offset, revision, count = struct.unpack_from('<4I', data)
    if marker != 0 or offset != 16 or count != 86:
        raise ValueError('unsupported backup header')
    records = tuple(struct.iter_unpack('<Ii', data[16:]))
    if tuple(key for key, _ in records) != tuple(sorted(PERSISTENT_IDS)):
        raise ValueError('unsupported or repeated backup keys')
    return dict(records)


def parse_backup(data):
    allowed = _records(data)
    if allowed[506] not in (0, 1) or allowed[507] not in (0, 1, 2):
        raise ValueError('unsupported native setting enum')
    return {50: allowed[506], 51: allowed[507]}


def parse_alexa_backup(data):
    records = _records(data)
    if records[544] not in (0, 1):
        raise ValueError('unsupported Alexa storage enum')
    return {85: records[544]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('private_backup', type=pathlib.Path)
    parser.add_argument('--alexa', action='store_true')
    args = parser.parse_args()
    try:
        with args.private_backup.open('rb') as source:
            values = (parse_alexa_backup if args.alexa else parse_backup)(source.read(705))
    except (OSError, ValueError) as error:
        raise SystemExit('backup unavailable or unsupported') from error
    for key, value in values.items():
        print(f'{key}={value}')


if __name__ == '__main__':
    main()
