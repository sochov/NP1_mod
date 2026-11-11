"""Pin target explicitly. All device writes are implemented by checked-in device scripts.

Usage: python tools/deploy.py --serial SERIAL --password-file tmp/owner-password.txt
The password file is stdin-only; it is never part of argv or tool output.
"""
import argparse
import pathlib
import re
import subprocess
import sys
parser = argparse.ArgumentParser()
parser.add_argument('--serial', required=True)
parser.add_argument('--password-file', type=pathlib.Path)
parser.add_argument('--action', choices=('install', 'install-vpn', 'verify', 'rollback', 'uninstall', 'ap-smoke', 'ap-dhcp-smoke', 'gobinet-readonly', 'gobinet-status-readonly', 'gobinet-profiles-readonly', 'gobinet-profile-list-readonly', 'apn-editor-seed', 'native-preflight', 'native-alexa-preflight', 'native-alexa-get', 'native-alexa-roundtrip', 'native-vendor-read', 'native-sensitivity-trial', 'native-detection-trial', 'firewall-update'), default='install')
args = parser.parse_args()
if not re.fullmatch(r'[A-Za-z0-9._:-]+', args.serial):
    raise SystemExit('Invalid serial')
root = pathlib.Path(__file__).resolve().parent.parent
profile_action = args.action in ('gobinet-profiles-readonly', 'gobinet-profile-list-readonly', 'apn-editor-seed')
bundle = root / 'artifacts' / ('ncc-profiles-readonly' if profile_action else 'ncc')
adb = ['adb', '-s', args.serial]
stage = '/tmp/ncc-profiles-stage' if profile_action else '/tmp/ncc-stage'
def run(*cmd, **kwargs):
    return subprocess.run(cmd, check=True, **kwargs)
run(*adb, 'get-state')
identity = subprocess.check_output(adb + ['shell', 'id']).decode()
if 'uid=0(root)' not in identity:
    raise SystemExit('Root ADB required')
if not (bundle / 'SHA256SUMS').is_file():
    raise SystemExit('Build artifact first')
run(*adb, 'push', '--sync', str(bundle) + '/.', stage)
data = b''
if args.password_file:
    data = args.password_file.read_bytes().strip() + b'\n'
process = subprocess.Popen(adb + ['shell', '/bin/sh', stage + '/tools/run.sh', args.action], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
prefix = b''
while True:
    line = process.stdout.readline()
    prefix += line
    if b'NCC_STDIN_READY' in line:
        break
    if not line:
        raise SystemExit('Device could not prepare a non-echoing input channel')
remaining, _ = process.communicate(data, timeout=180)
output = (prefix + remaining).decode('utf-8', errors='replace')
if data.strip():
    output = output.replace(data.strip().decode('utf-8'), '[REDACTED]')
print(output)
if process.returncode or not re.search(r'NCC_EXIT_STATUS=0\s*$', output):
    raise SystemExit('Device action failed; inspect sanitized output above')
