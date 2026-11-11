"""Stage/preflight or one controlled roundtrip, using only checked-in scripts.

Requires an explicit device serial; never selects or embeds an arbitrary serial.
All raw ADB output and device readbacks stay in ignored tmp evidence.
"""
import argparse
import hashlib
import json
import pathlib
import re
import shutil
import subprocess
import struct
import time
import uuid

ROOT = pathlib.Path(__file__).resolve().parents[1]
AUDIO_KEYS = {'mute': (1, 466), 'volume-correction': (22, 468)}
IDS = tuple(AUDIO_KEYS)


def verify_readback(directory, setting_id=None, next_value=None):
    """Independently inspect the actual pulled before/applied/restored files."""
    before = (directory / 'before-values.txt').read_bytes()
    if before != (directory / 'after-values.txt').read_bytes():
        raise ValueError('native values/semaphores not restored')
    stock = (directory / 'stock-before.txt').read_bytes()
    if stock != (directory / 'stock-after.txt').read_bytes():
        raise ValueError('stock/consumer PID/state changed')
    if stock.count(b'ActiveState=active') != 10 or len(re.findall(rb'MainPID=[1-9][0-9]*\b', stock)) != 10:
        raise ValueError('all ten consumers were not active')
    if (directory / 'saved-before.dat').read_bytes() != (directory / 'saved-after.dat').read_bytes():
        raise ValueError('complete persistent backup did not return to baseline')
    if setting_id is None:
        get = directory / 'get.txt'
        if get.exists():
            actual = dict(line.split('=', 1) for line in get.read_text().splitlines() if '=' in line)
            initial = dict(line.split('=', 1) for line in (directory / 'before-values.txt').read_text().splitlines() if '=' in line)
            for wire, native in (('audio-volume', 'native-volume'), ('audio-mute', '01'), ('audio-correction', '22')):
                if actual.get(wire) != initial.get(native):
                    raise ValueError('owner GET and native preflight disagree')
            if 'cleanup=matched; own-receiver-removed' not in get.read_text():
                raise ValueError('owner GET cleanup proof missing')
        return
    index, persistent = AUDIO_KEYS[setting_id]
    maximum = 1 if index == 1 else 2
    sequence, saved, snapshots = [], [], []
    for phase in ('before', 'applied', 'after'):
        values = dict(line.split('=', 1) for line in (directory / (phase + '-values.txt')).read_text().splitlines() if '=' in line)
        sequence.append(int(values[f'{index:02d}']))
        data = (directory / ('saved-' + phase + '.dat')).read_bytes()
        snapshots.append(data)
        if len(data) != 704 or struct.unpack_from('<4I', data)[0:2] != (0, 16) or struct.unpack_from('<I', data, 12)[0] != 86:
            raise ValueError('unsupported persistent backup')
        pairs = tuple(struct.iter_unpack('<Ii', data[16:]))
        if tuple(key for key, _ in pairs) != tuple(sorted(set(key for key, _ in pairs))):
            raise ValueError('unknown/duplicate persistent keys')
        saved.append(dict(pairs)[persistent])
    if any(value < 0 or value > maximum for value in sequence + saved):
        raise ValueError('unsupported audio enum in independent read-back')
    if sequence[1] != int(next_value) or sequence[0] == sequence[1] or sequence[2] != sequence[0] or sequence != saved:
        raise ValueError('expected target/persistent roundtrip was not observed')
    expected_applied = bytearray(snapshots[0])
    for offset in range(16, 704, 8):
        if struct.unpack_from('<I', expected_applied, offset)[0] == persistent:
            struct.pack_into('<i', expected_applied, offset + 4, int(next_value))
            break
    if snapshots[1] != expected_applied:
        raise ValueError('an unrelated persistent setting changed during apply')
    marker = 'cleanup=matched; own-receiver-removed'
    if not all(marker in (directory / (phase + '.txt')).read_text() for phase in ('apply', 'restore')):
        raise ValueError('apply/restoration cleanup was not confirmed')


def parse_arguments(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--serial', required=True)
    parser.add_argument('--action', choices=('preflight', 'get', 'roundtrip'), default='preflight')
    parser.add_argument('--id', choices=IDS)
    parser.add_argument('--next', choices=tuple(str(value) for value in range(3)))
    args = parser.parse_args(argv)
    if not re.fullmatch(r'[A-Za-z0-9._:-]+', args.serial):
        parser.error('invalid serial')
    if (args.action == 'roundtrip') != bool(args.id and args.next is not None):
        parser.error('roundtrip requires --id and --next; preflight accepts neither')
    if args.action != 'roundtrip' and (args.id is not None or args.next is not None):
        parser.error('preflight/get accept no setting values')
    if args.id == 'mute' and args.next == '2':
        parser.error('mute accepts only native0/1')
    return args


def main():
    args = parse_arguments()
    material = ROOT / 'artifacts/native-audio-helper'
    helper = material / 'ncc-native-audio-helper'
    info = json.loads((material / 'build-info.json').read_text())
    if hashlib.sha256(helper.read_bytes()).hexdigest() != info['sha256']:
        raise SystemExit('Build the audio helper first; binary hash mismatch')
    bundle = ROOT / 'artifacts/native-audio-stage'
    files = {
        'bin/ncc-native-audio-helper': helper,
        'build-info.json': material / 'build-info.json',
        'tools/native-audio-stage.sh': ROOT / 'tools/device/native-audio-stage.sh',
        'tools/native-audio-trial.sh': ROOT / 'tools/device/native-audio-trial.sh',
    }
    for relative, source in files.items():
        target = bundle / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, target)
    allowed = set(files) | {'SHA256SUMS'}
    if any(p.is_file() and p.relative_to(bundle).as_posix() not in allowed for p in bundle.rglob('*')):
        raise SystemExit('Unexpected stage files; inspect the dedicated artifact directory')
    manifest = ''.join(hashlib.sha256((bundle / name).read_bytes()).hexdigest() + '  ' + name + '\n' for name in files)
    (bundle / 'SHA256SUMS').write_text(manifest, encoding='ascii', newline='\n')
    evidence = ROOT / 'tmp/native-audio-evidence' / (time.strftime('%Y%m%d-%H%M%S') + '-' + uuid.uuid4().hex[:8])
    evidence.mkdir(parents=True, mode=0o700)
    log = evidence / 'transport.txt'
    adb = ['adb', '-s', args.serial]

    def transport(arguments):
        result = subprocess.run(adb + arguments, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        with log.open('ab') as target:
            target.write(result.stdout)
        if result.returncode:
            raise SystemExit('ADB transport failed; inspect private evidence: ' + str(evidence))
        return result.stdout

    transport(['get-state'])
    if b'uid=0(root)' not in transport(['shell', 'id']):
        raise SystemExit('Root ADB required')
    stage = '/tmp/np1-native-audio-stage.' + uuid.uuid4().hex
    transport(['push', str(bundle) + '/.', stage])
    selector = [args.action] + ([args.id, args.next] if args.action == 'roundtrip' else [])
    process = subprocess.Popen(adb + ['shell', '/bin/sh', stage + '/tools/native-audio-stage.sh'] + selector,
                               stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    prefix = bytearray()
    while True:
        line = process.stdout.readline()
        prefix.extend(line)
        if b'NCC_STDIN_READY' in line:
            break
        if not line:
            with log.open('ab') as target:
                target.write(prefix)
            raise SystemExit('Device could not prepare the trial; inspect private evidence: ' + str(evidence))
    first = True
    while True:
        try:
            remaining, _ = process.communicate(b'' if first else None, timeout=30)
            break
        except subprocess.TimeoutExpired:
            first = False
            print('Controlled trial still running; waiting without killing vendor code.', flush=True)
    output = bytes(prefix) + remaining
    with log.open('ab') as target:
        target.write(output)
    match = re.search(rb'Private audio native trial records: (/run/np1-native-audio-trial\.[A-Za-z0-9]+)', output)
    if match:
        transport(['pull', match.group(1).decode('ascii'), str(evidence)])
        actual = evidence / pathlib.PurePosixPath(match.group(1).decode('ascii')).name / 'helper-sha256.txt'
        if not actual.is_file() or actual.read_text().split()[0] != info['sha256']:
            raise SystemExit('Executed helper version mismatch; inspect private evidence: ' + str(evidence))
        try:
            if args.action == 'get' and not (actual.parent / 'get.txt').is_file():
                raise ValueError('owner GET proof missing')
            verify_readback(actual.parent, args.id, args.next)
        except (ValueError, KeyError, OSError, StopIteration) as error:
            raise SystemExit('Independent read-back failed; inspect private evidence: ' + str(evidence)) from error
    else:
        raise SystemExit('Trial read-back records were not located; private evidence: ' + str(evidence))
    if process.returncode or not re.search(rb'NCC_EXIT_STATUS=0\s*$', output):
        raise SystemExit('Controlled action failed or is inconclusive; private evidence: ' + str(evidence))
    print('Audio ' + args.action + ' passed; private evidence: ' + str(evidence))


if __name__ == '__main__':
    main()
