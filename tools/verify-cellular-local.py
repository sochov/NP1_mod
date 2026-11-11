"""Check AP management and NP1-bound cellular TLS during one fixed STA roundtrip.

No PC network interface, AP configuration or APN is changed. The checked-in
native STA launcher owns the OFF deadline and restores the original ON state.
This tests NP1-origin traffic; it does not claim AP-client/NAT coverage.
"""
import argparse
import datetime as dt
import json
import pathlib
import re
import subprocess
import sys
import time

ROOT = pathlib.Path(__file__).resolve().parents[1]


def validate_network(value):
    return (value.get('ready') is True and value.get('managementWiFiCompleted') is False
            and value.get('managementHotspotVerified') is True
            and value.get('managementPath') == 'hotspot'
            and value.get('restorableDefaultCaptured') is False)


def validate_internet(value):
    return (value.get('status') == 'passed' and value.get('source') == 'np1-origin-bound-cellular'
            and all(value.get(key) is True for key in ('dnsVerified', 'tlsVerified', 'httpVerified', 'identityUnchanged')))


def read_json_line(raw):
    for line in raw.replace(b'\r', b'').decode('utf8', 'replace').splitlines():
        if line.startswith('{'):
            return json.loads(line)
    raise ValueError('Fixed probe did not return JSON')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--serial', required=True)
    parser.add_argument('--out', type=pathlib.Path, required=True)
    args = parser.parse_args()
    if not re.fullmatch(r'[A-Za-z0-9._:-]+', args.serial):
        parser.error('Invalid serial')
    out = args.out.resolve()
    if not out.is_relative_to(ROOT / 'tmp') or out.exists():
        parser.error('Fresh ignored tmp directory required')
    out.mkdir(parents=True)
    adb = ['adb', '-s', args.serial]
    bundle = ROOT / 'artifacts/ncc-native-apn-readonly'
    if not (bundle / 'SHA256SUMS').is_file():
        parser.error('Build the native APN read-only probe first')
    stage = '/tmp/ncc-native-apn-stage'
    subprocess.run(adb + ['push', '--sync', str(bundle) + '/.', stage], check=True, capture_output=True, timeout=40)
    result = {'passed': False, 'restored': False, 'hostNetworkChanged': False}
    off, complete = out / 'off.json', out / 'complete.json'
    with (out / 'sta-launcher.private').open('wb') as log:
        child = subprocess.Popen([sys.executable, str(ROOT / 'tools/trial_wifi_sta.py'), '--serial', args.serial,
                                  '--mode', 'roundtrip', '--hold-seconds', '45', '--off-ready-file', str(off),
                                  '--complete-file', str(complete), '--records', str(out / 'sta')], stdout=log, stderr=log)
        try:
            until = time.monotonic() + 35
            while not off.exists():
                if child.poll() is not None or time.monotonic() >= until:
                    raise ValueError('Correlated STA OFF phase not confirmed')
                time.sleep(.1)
            phase = json.loads(off.read_text())
            deadline = dt.datetime.fromisoformat(phase['restoreDeadline'])
            if phase.get('offConfirmed') is not True or phase.get('holdSeconds') != 45:
                raise ValueError('Invalid STA OFF phase')
            for mode, validator, budget in (('network', validate_network, 22), ('internet', validate_internet, 12)):
                if (deadline - dt.datetime.now(dt.timezone.utc)).total_seconds() < budget:
                    raise ValueError('Insufficient fixed OFF budget')
                reply = subprocess.run(adb + ['shell', '/bin/sh', stage + '/tools/native-apn-status-readonly.sh', mode],
                                       capture_output=True, timeout=budget)
                (out / (mode + '.private')).write_bytes(reply.stdout + reply.stderr)
                if reply.returncode:
                    raise ValueError('Fixed ' + mode + ' probe failed')
                value = read_json_line(reply.stdout)
                (out / (mode + '.json')).write_text(json.dumps(value, indent=2) + '\n')
                if not validator(value):
                    raise ValueError('Fixed ' + mode + ' read-back not verified')
                result[mode + 'Verified'] = True
        except Exception as error:
            result['error'] = str(error) if isinstance(error, ValueError) else type(error).__name__
        finally:
            # Let the device-owned deadline/trap complete; never cancel it to
            # save host wait time or start a second network writer.
            try:
                child.wait(timeout=180)
                if complete.exists():
                    restoration = json.loads(complete.read_text())
                    result['restored'] = child.returncode == 0 and restoration.get('restored') is True
            except subprocess.TimeoutExpired:
                result['error'] = 'STA launcher still running; inspect private cleanup before another network action'
            result['passed'] = (result['restored'] and result.get('networkVerified') is True
                                and result.get('internetVerified') is True)
            (out / 'summary.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
