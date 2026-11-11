"""Explicitly stage and execute the fixed Opening-only recovery scripts."""
import argparse
import pathlib
import re
import subprocess
import time
import uuid

ROOT = pathlib.Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--serial', required=True)
    args = parser.parse_args()
    if not re.fullmatch(r'[A-Za-z0-9._:-]+', args.serial):
        parser.error('invalid serial')
    token = time.strftime('%Y%m%d-%H%M%S') + '-' + uuid.uuid4().hex[:8]
    remote = '/tmp/ncc-opening-tools-' + token
    evidence = ROOT / 'tmp/stock-service-evidence' / ('opening-recovery-' + token)
    evidence.mkdir(parents=True)
    commands = [['shell', 'umask 077; mkdir ' + remote]]
    for name in ('recover-opening-sync.py', 'recover-opening-service.sh'):
        commands.append(['push', str(ROOT / 'tools/device' / name), remote + '/' + name])
    commands.append(['shell', 'sh ' + remote + '/recover-opening-service.sh'])
    for index, command in enumerate(commands):
        result = subprocess.run(['adb', '-s', args.serial] + command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        (evidence / (str(index) + '.txt')).write_bytes(result.stdout)
        if result.returncode or (index == len(commands) - 1 and
                b'PASS: Opening active with one PID for five seconds;' not in result.stdout):
            raise SystemExit('Opening recovery failed; inspect private evidence ' + str(evidence))
    print('PASS: checked-in Opening recovery completed; private evidence ' + str(evidence))


if __name__ == '__main__':
    main()
