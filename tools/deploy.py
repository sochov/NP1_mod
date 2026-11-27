"""Pin target explicitly. All device writes are implemented by checked-in device scripts.

Usage: python tools/deploy.py --serial SERIAL --password-file tmp/owner-password.txt
The password file is stdin-only; it is never part of argv or tool output.
"""
import argparse
import pathlib
import re
import subprocess
import sys
from full_install_contract import missing_full_install_files
parser = argparse.ArgumentParser()
parser.add_argument('--serial', required=True)
parser.add_argument('--password-file', type=pathlib.Path)
parser.add_argument('--bundle', type=pathlib.Path, help='Minimal bundle for web-worker-update (must be below artifacts/)')
parser.add_argument('--action', choices=('install', 'install-owner-recovery', 'rollback-owner-recovery', 'install-vpn', 'verify', 'rollback', 'uninstall', 'stock-start-install', 'nas-backup-install', 'web-worker-update', 'update-install', 'recorder-hook-update', 'recorder-hook-prune', 'journey-update', 'native-worker-update', 'modem-worker-update', 'ap-worker-update', 'ap-smoke', 'ap-dhcp-smoke', 'gobinet-readonly', 'gobinet-status-readonly', 'gobinet-profiles-readonly', 'gobinet-profile-list-readonly', 'native-apn-status-readonly', 'native-apn-network-readonly', 'native-apn-same-profile-trial', 'native-apn-route-recover', 'apn-editor-seed', 'native-alexa-preflight', 'native-alexa-get', 'native-alexa-roundtrip', 'native-preflight', 'native-vendor-read', 'native-sensitivity-trial', 'native-detection-trial', 'firewall-update'), default='install')
parser.add_argument('--update-key-file', type=pathlib.Path, help='Trusted 64-character hex Ed25519 public key for update-install')
parser.add_argument('--update-repository', default='sochov/NP1_mod', help='Public GitHub OWNER/REPO for update-install')
args = parser.parse_args()
if not re.fullmatch(r'[A-Za-z0-9._:-]+', args.serial):
    raise SystemExit('Invalid serial')
enrollment = b''
if args.bundle and args.action != 'web-worker-update':
    parser.error('--bundle is only supported for web-worker-update')
if args.action == 'update-install':
    if not args.update_key_file or args.password_file or not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', args.update_repository):
        parser.error('update-install requires --update-key-file and a valid --update-repository')
    public_key = args.update_key_file.read_bytes().strip().lower()
    if not re.fullmatch(rb'[0-9a-f]{64}', public_key):
        parser.error('invalid Ed25519 update public key')
    enrollment = public_key + b'\n' + args.update_repository.encode('ascii') + b'\n'
elif args.update_key_file:
    parser.error('--update-key-file is only used by update-install')
root = pathlib.Path(__file__).resolve().parent.parent
profile_action = args.action in ('gobinet-profiles-readonly', 'gobinet-profile-list-readonly', 'native-apn-status-readonly', 'apn-editor-seed')
native_apn_action = args.action in ('native-apn-status-readonly', 'native-apn-network-readonly', 'native-apn-same-profile-trial', 'native-apn-route-recover')
recorder_action = args.action in ('recorder-hook-update', 'recorder-hook-prune')
bundle = root / 'artifacts' / ('ncc-recorder-hook' if recorder_action else 'ncc-native-apn-readonly' if native_apn_action else 'ncc-profiles-readonly' if profile_action else 'ncc')
if args.bundle:
    bundle = args.bundle.resolve()
    try:
        bundle.relative_to((root / 'artifacts').resolve())
    except ValueError:
        parser.error('--bundle must be below artifacts/')
adb = ['adb', '-s', args.serial]
stage = '/tmp/ncc-recorder-hook-stage' if recorder_action else '/tmp/ncc-native-apn-stage' if native_apn_action else '/tmp/ncc-profiles-stage' if profile_action else '/tmp/ncc-stage'
def run(*cmd, **kwargs):
    return subprocess.run(cmd, check=True, **kwargs)
run(*adb, 'get-state')
identity = subprocess.check_output(adb + ['shell', 'id']).decode()
if 'uid=0(root)' not in identity:
    raise SystemExit('Root ADB required')
if not (bundle / 'SHA256SUMS').is_file():
    raise SystemExit('Build artifact first')
if args.action in ('install', 'install-owner-recovery'):
    missing = missing_full_install_files(bundle)
    if missing:
        raise SystemExit('Full installation bundle is incomplete; device was not changed:\n - ' + '\n - '.join(missing))
run(*adb, 'push', '--sync', str(bundle) + '/.', stage)
data = enrollment
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
if args.password_file and data.strip():
    output = output.replace(data.strip().decode('utf-8'), '[REDACTED]')
print(output)
if process.returncode or not re.search(r'NCC_EXIT_STATUS=0\s*$', output):
    raise SystemExit('Device action failed; inspect sanitized output above')
