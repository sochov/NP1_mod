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
import threading

from wifi_sta_phase import STAPhaseFiles

root = pathlib.Path(__file__).resolve().parents[1]
p = argparse.ArgumentParser()
p.add_argument('--serial', required=True)
p.add_argument('--mode', choices=('preflight', 'get', 'roundtrip'), default='preflight')
p.add_argument('--records', type=pathlib.Path, required=True)
p.add_argument('--hold-seconds', type=int, choices=(3, 30, 45), default=3)
p.add_argument('--off-ready-file', type=pathlib.Path)
p.add_argument('--complete-file', type=pathlib.Path)
p.add_argument('--cm-get-mode', choices=('none', 'nas', 'profiles'), default='none')
a = p.parse_args()
if not re.fullmatch(r'[A-Za-z0-9._:-]+', a.serial):
    raise SystemExit('Invalid serial')
records = a.records.resolve()
if not any(records.is_relative_to(root / name) for name in ('tmp', 'resources', 'docs_private')):
    raise SystemExit('Private records must stay in tmp/resources/docs_private')
records.mkdir(parents=True, exist_ok=True)
if a.mode != 'roundtrip' and (a.hold_seconds != 3 or a.off_ready_file is not None or a.complete_file is not None or a.cm_get_mode != 'none'):
    raise SystemExit('Bounded OFF and phase files require roundtrip mode')
phases = STAPhaseFiles(root, a.hold_seconds, a.off_ready_file, a.complete_file)
bundle = root / 'artifacts/ncc-native-wifi-sta'
if not (bundle / 'SHA256SUMS').is_file():
    raise SystemExit('Build the standalone STA probe first')
adb = ['adb', '-s', a.serial]
subprocess.run(adb + ['get-state'], check=True, stdout=subprocess.DEVNULL)
if b'uid=0(root)' not in subprocess.check_output(adb + ['shell', 'id']):
    raise SystemExit('Root ADB required')
stage = '/tmp/ncc-native-wifi-sta-stage'
subprocess.run(adb + ['push', '--sync', str(bundle) + '/.', stage], check=True, stdout=subprocess.DEVNULL)
arguments = adb + ['shell', '/bin/sh', stage + '/tools/native-wifi-sta-trial.sh', a.mode]
if a.mode == 'roundtrip':
    arguments += [str(a.hold_seconds), a.cm_get_mode]
process = subprocess.Popen(arguments, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
chunks, phase_errors = [], []
def receive():
    total = 0
    while True:
        line = process.stdout.readline(65537)
        if not line:
            return
        total += len(line)
        if total <= 1024 * 1024:
            chunks.append(line)
        elif not phase_errors:
            phase_errors.append('STA_PHASE_OUTPUT_OVERSIZE')
        try:
            phases.accept(line)
        except Exception:
            phase_errors.append('STA_PHASE_GATE_UNVERIFIED')
reader = threading.Thread(target=receive, daemon=True)
reader.start()
try:
    process.wait(timeout=240)
    reader.join(timeout=5)
    if reader.is_alive():
        phase_errors.append('STA_PHASE_READER_INCOMPLETE')
    output = b''.join(chunks)
except (subprocess.TimeoutExpired, KeyboardInterrupt):
    # Closing the PTY delivers HUP to the checked-in launcher; its trap performs
    # the fixed ON recovery. Never start a second OFF or an unrelated installer.
    process.kill()
    process.wait(timeout=10)
    reader.join(timeout=5)
    output = b''.join(chunks)
    (records / 'launcher-private.txt').write_bytes(output)
    phases.finish(False)
    raise SystemExit('STA trial interrupted; verify its device cleanup before another network action')
(records / 'launcher-private.txt').write_bytes(output)
text = output.decode('utf-8', 'replace')
match = re.search(r'Private STA trial records: (/run/np1-native-wifi-sta\.[A-Za-z0-9]+)', text)
if match:
    subprocess.run(adb + ['pull', match.group(1), str(records / 'device')], check=True, stdout=subprocess.DEVNULL)
restored = (not process.returncode and not phase_errors and bool(re.search(r'NCC_EXIT_STATUS=0\s*$', text)) and
            (a.mode != 'roundtrip' or (phases.off_confirmed and 'STA owner cleanup verified; stock, workers, original profile and AP preserved' in text)))
phases.finish(restored)
if not restored:
    raise SystemExit('STA owner trial is unverified; private records retained')
print('STA owner ' + a.mode + ' completed; private read-back records retained')
