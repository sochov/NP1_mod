"""Run a bounded, isolated NCC AP/DHCP smoke; never replace an installed binary.

All device mutations are performed by tools/device/ap-dhcp-smoke.sh / Go's owned
AP controller, with stock/STA/routes baseline and finally cleanup read-back.
The output directory is private/ignored because device diagnostics may identify it.
"""
import argparse
import json
import pathlib
import re
import subprocess
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--serial', required=True)
    parser.add_argument('--binary', type=pathlib.Path, required=True)
    parser.add_argument('--out', type=pathlib.Path, required=True)
    args = parser.parse_args()
    if not re.fullmatch(r'[A-Za-z0-9._:-]+', args.serial) or not args.binary.is_file():
        parser.error('invalid serial or binary')
    root = pathlib.Path(__file__).resolve().parent.parent
    out = args.out.resolve()
    if root / 'tmp' not in out.parents:
        parser.error('private trial outputs must be beneath repository tmp/')
    out.mkdir(parents=True, exist_ok=True)
    adb = ['adb', '-s', args.serial]
    stage = '/run/ncc-ap-agent-trial-' + uuid.uuid4().hex[:12]
    subprocess.run(adb + ['shell', 'mkdir -p ' + stage + '/bin ' + stage + '/scripts'], check=True, capture_output=True)
    for source, target in [(args.binary, '/bin/ncc'), (root / 'tools/device/ap-channel.py', '/scripts/ap-channel.py'), (root / 'tools/device/ap-dhcp-smoke.sh', '/scripts/ap-dhcp-smoke.sh'), (root / 'tools/device/ap-dhcp-smoke-reported.sh', '/scripts/ap-dhcp-smoke-reported.sh')]:
        subprocess.run(adb + ['push', str(source), stage + target], check=True, capture_output=True)
    subprocess.run(adb + ['shell', 'chmod 0700 ' + stage + ' ' + stage + '/bin/ncc ' + stage + '/scripts/ap-dhcp-smoke.sh'], check=True, capture_output=True)
    result = subprocess.run(adb + ['shell', 'sh ' + stage + '/scripts/ap-dhcp-smoke-reported.sh ' + stage + '/scripts/ap-dhcp-smoke.sh'], capture_output=True, timeout=240)
    (out / 'stdout.txt').write_bytes(result.stdout)
    (out / 'stderr.txt').write_bytes(result.stderr)
    match = re.search(rb'Private DHCP/AP smoke read-back: (/run/np1-ap-dhcp-smoke\.[A-Za-z0-9]+)', result.stdout)
    remote = match.group(1).decode() if match else None
    if remote:
        for name in ['before.json', 'during.json', 'after.json', 'runtime/status.json', 'runtime/private/startup.jsonl', 'runtime/private/hostapd.log', 'runtime/private/dnsmasq.log']:
            local = out / name
            local.parent.mkdir(parents=True, exist_ok=True)
            subprocess.run(adb + ['pull', remote + '/' + name, str(local)], capture_output=True)
    check = subprocess.run(adb + ['shell', 'test ! -e /sys/class/net/nccap0 && echo interface-absent'], capture_output=True)
    reported = re.findall(rb'^NCC_AP_TRIAL_EXIT=(\d+)\r?$', result.stdout.replace(b'\r\r\n', b'\n'), re.MULTILINE)
    remote_code = int(reported[-1]) if len(reported) == 1 else 125
    summary = {'returncode': remote_code, 'adbReturncode': result.returncode, 'interfaceAbsent': check.stdout.strip() == b'interface-absent', 'remotePrivateResult': remote}
    (out / 'summary.json').write_text(json.dumps(summary, indent=2), encoding='utf-8')
    print(json.dumps(summary))
    return remote_code


if __name__ == '__main__':
    raise SystemExit(main())
