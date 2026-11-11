"""One bounded installed-AP/STA-loss diagnostic; never restarts AP/Web units."""
import argparse
import json
import pathlib
import re
import shlex
import subprocess
import time
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--serial', required=True)
    parser.add_argument('--out', required=True, type=pathlib.Path)
    args = parser.parse_args()
    root = pathlib.Path(__file__).resolve().parent.parent
    out = args.out.resolve()
    if not re.fullmatch(r'[A-Za-z0-9._:-]+', args.serial) or root / 'tmp' not in out.parents or out.exists():
        parser.error('valid serial and fresh private tmp/ directory required')
    out.mkdir(parents=True)
    adb = ['adb', '-s', args.serial]
    stage = '/run/ncc-ap-sharing-' + uuid.uuid4().hex[:12]
    monitor = loss = observer = None
    result = {'restored': False}

    def shell(command):
        response = subprocess.run(adb + ['shell', command], capture_output=True, timeout=15)
        return response.stdout.replace(b'\r', b'').decode()

    def save(name, value):
        (out / name).write_text(json.dumps(value, ensure_ascii=False, indent=2), encoding='utf-8')

    units = 'systemctl show np1-control.service np1-control-broker.service np1-control-ap.service -p Id -p MainPID -p ActiveState -p SubState'
    try:
        config = json.loads(shell('cat /overlay/owner-apps/np1-control/private/ap/config.json'))
        save('client-access.json', {'ssid': config['ssid'], 'password': config['passphrase']})
        before = shell(units)
        (out / 'units-before.txt').write_text(before, encoding='utf-8')
        shell('mkdir -m 0700 ' + stage)
        for name in ('ap-upstream-loss.py', 'ap-upstream-loss.sh', 'ap-link-observe.sh', 'ap-http-probe.py'):
            pushed = subprocess.run(adb + ['push', str(root / 'tools/device' / name), stage + '/' + name], capture_output=True)
            if pushed.returncode:
                raise RuntimeError('diagnostic transfer failed')
        monitor = subprocess.Popen(['powershell', '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', str(root / 'tools/verify-ap-continuity.ps1'),
                                    '-HoldSeconds', '45', '-ReadyFile', str(out / 'ready'), '-MonitorPath', str(out / 'client.jsonl'),
                                    '-AccessFile', str(out / 'client-access.json')], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        deadline = time.monotonic() + 45
        while not (out / 'ready').exists() and monitor.poll() is None and time.monotonic() < deadline:
            time.sleep(0.5)
        if not (out / 'ready').exists():
            raise RuntimeError('AP client readiness not confirmed')
        observer = subprocess.Popen(adb + ['shell', 'sh ' + stage + '/ap-link-observe.sh ' + stage], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        time.sleep(2)
        loss = subprocess.Popen(adb + ['shell', 'sh ' + stage + '/ap-upstream-loss.sh ' + stage], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        stdout, stderr = loss.communicate(timeout=55)
        (out / 'loss-process.txt').write_bytes(stdout + stderr)
        result['staRestored'] = b'NCC_STA_LOSS_RESTORED' in stdout
        loss = None
        stdout, stderr = monitor.communicate(timeout=80)
        (out / 'client-process.txt').write_bytes(stdout + stderr)
        result['clientContinuity'] = monitor.returncode == 0
        monitor = None
        observer.communicate(timeout=60)
        observer = None
        (out / 'device-observations.txt').write_text(shell('cat ' + stage + '/link-observations.txt'), encoding='utf-8')
        after = shell(units)
        (out / 'units-after.txt').write_text(after, encoding='utf-8')
        result['unitPIDsUnchanged'] = before == after
    except Exception as error:
        result['error'] = type(error).__name__
    finally:
        for process in (loss, monitor, observer):
            if process:
                try:
                    process.communicate(timeout=65)
                except Exception:
                    result['cleanupNeedsReview'] = True
        # This separate non-switching check reconnects only the temporary PC
        # station and confirms installed AP DNS/NAT/standard TLS have returned.
        verified = subprocess.run(['python', str(root / 'tools/verify-hotspot-internet.py'), '--serial', args.serial,
                                   '--out', str(out / 'restored-internet')], capture_output=True, timeout=135)
        (out / 'restored-internet-process.txt').write_bytes(verified.stdout + verified.stderr)
        result['restored'] = verified.returncode == 0
        save('summary.json', result)
    print(json.dumps(result))
    return 0 if result['restored'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
