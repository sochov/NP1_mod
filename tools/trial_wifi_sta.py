"""Explicit, reproducible staging/action for the standalone fixed STA trial.

No permanent install, stock service restart, rootfs write or APN write. The
roundtrip mode briefly disables stock STA and restores its original ON state.
Run only in the coordinator's exclusive network test window.
"""
import argparse
import pathlib
import re
import subprocess
import sys

root = pathlib.Path(__file__).resolve().parents[1]
p = argparse.ArgumentParser()
p.add_argument('--serial', required=True)
p.add_argument('--mode', choices=('preflight', 'get', 'roundtrip'), default='preflight')
p.add_argument('--records', type=pathlib.Path, required=True)
a = p.parse_args()
if not re.fullmatch(r'[A-Za-z0-9._:-]+', a.serial):
    raise SystemExit('Invalid serial')
records = a.records.resolve()
if not any(records.is_relative_to(root / name) for name in ('tmp', 'resources', 'docs_private')):
    raise SystemExit('Private records must stay in tmp/resources/docs_private')
records.mkdir(parents=True, exist_ok=True)
bundle = root / 'artifacts/ncc-native-wifi-sta'
if not (bundle / 'SHA256SUMS').is_file():
    raise SystemExit('Build the standalone STA probe first')
adb = ['adb', '-s', a.serial]
subprocess.run(adb + ['get-state'], check=True, stdout=subprocess.DEVNULL)
if b'uid=0(root)' not in subprocess.check_output(adb + ['shell', 'id']):
    raise SystemExit('Root ADB required')
stage = '/tmp/ncc-native-wifi-sta-stage'
subprocess.run(adb + ['push', '--sync', str(bundle) + '/.', stage], check=True, stdout=subprocess.DEVNULL)
process = subprocess.Popen(adb + ['shell', '/bin/sh', stage + '/tools/native-wifi-sta-trial.sh', a.mode], stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
try:
    output = process.communicate(timeout=210)[0]
except (subprocess.TimeoutExpired, KeyboardInterrupt):
    # Closing the PTY delivers HUP to the checked-in launcher; its trap performs
    # the fixed ON recovery. Never start a second OFF or an unrelated installer.
    process.kill()
    output = process.communicate(timeout=10)[0]
    (records / 'launcher-private.txt').write_bytes(output)
    raise SystemExit('STA trial interrupted; verify its device cleanup before another network action')
(records / 'launcher-private.txt').write_bytes(output)
text = output.decode('utf-8', 'replace')
match = re.search(r'Private STA trial records: (/run/np1-native-wifi-sta\.[A-Za-z0-9]+)', text)
if match:
    subprocess.run(adb + ['pull', match.group(1), str(records / 'device')], check=True, stdout=subprocess.DEVNULL)
if process.returncode or not re.search(r'NCC_EXIT_STATUS=0\s*$', text):
    raise SystemExit('STA owner trial is unverified; private records retained')
print('STA owner ' + a.mode + ' completed; private read-back records retained')
