"""Verify the installed hotspot using a temporary Windows Wi-Fi client.

Does not restart NP1 services, change AP/STA settings, or change host routes.
Device settings/counters and credentials are saved under tmp/. With
--preference-roundtrip, also verify and restore the hot preference setting.
"""
import argparse
import json
import pathlib
import re
import subprocess
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--serial', required=True)
    parser.add_argument('--out', type=pathlib.Path, required=True)
    parser.add_argument('--interface', default='Wi-Fi')
    parser.add_argument('--preference-roundtrip', action='store_true')
    parser.add_argument('--allow-windows-wifi-changes', action='store_true', help='Requires fresh explicit user authorization; disabled by default')
    args = parser.parse_args()
    if not args.allow_windows_wifi_changes:
        parser.error('Windows Wi-Fi changes are disabled; explicit user authorization and opt-in are required')
    root = pathlib.Path(__file__).resolve().parent.parent
    out = args.out.resolve()
    if not re.fullmatch(r'[A-Za-z0-9._:-]+', args.serial) or root / 'tmp' not in out.parents or out.exists():
        parser.error('valid serial and fresh tmp/ output directory required')
    out.mkdir(parents=True)
    result = {'passed': False}

    def read(command):
        response = subprocess.run(['adb', '-s', args.serial, 'shell', command], capture_output=True, timeout=20)
        if response.returncode:
            raise RuntimeError('read-only device command failed')
        return response.stdout.replace(b'\r', b'').decode()

    def save(name, value):
        (out / name).write_text(json.dumps(value, ensure_ascii=False, indent=2), encoding='utf-8')

    def ap():
        with urllib.request.urlopen('http://127.0.0.1/api/v1/network/hotspot', timeout=10) as response:
            value = json.load(response)['data']
        if not value.get('enabled') or not value.get('sharing', {}).get('ready'):
            raise RuntimeError('installed AP sharing is not ready')
        return value

    def count(raw):
        value = 0
        for line in raw.splitlines():
            fields = line.split()
            if len(fields) > 2 and fields[0].isdigit() and fields[2] == 'MASQUERADE':
                value += int(fields[0])
        return value

    try:
        before = ap()
        save('hotspot-before.json', before)
        config = json.loads(read('cat /overlay/owner-apps/np1-control/private/ap/config.json'))
        save('client-access.json', {'ssid': config['ssid'], 'password': config['passphrase'],
                                    'upstream': before['sharing']['upstream']['kind']})
        counter_before = read('iptables -w 2 -t nat -L NCC_AP_NAT_V1 -v -n -x')
        (out / 'nat-before.txt').write_text(counter_before, encoding='utf-8')
        units_command = 'systemctl show np1-control.service np1-control-broker.service np1-control-ap.service -p Id -p MainPID -p ActiveState -p SubState'
        units_before = read(units_command)
        (out / 'units-before.txt').write_text(units_before, encoding='utf-8')
        command = ['powershell', '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File',
                                   str(root / 'tools/verify-ap-internet.ps1'), '-AllowWindowsWiFiChanges', '-Interface', args.interface,
                                   '-AccessFile', str(out / 'client-access.json'), '-ResultFile', str(out / 'internet.json'), '-DeviceSerial', args.serial]
        if args.preference_roundtrip:
            command.append('-PreferenceRoundtrip')
        response = subprocess.run(command, capture_output=True, timeout=180)
        (out / 'client-process.txt').write_bytes(response.stdout + response.stderr)
        if response.returncode:
            raise RuntimeError('client Internet verification failed; see private result')
        after = ap()
        save('hotspot-after.json', after)
        counter_after = read('iptables -w 2 -t nat -L NCC_AP_NAT_V1 -v -n -x')
        (out / 'nat-after.txt').write_text(counter_after, encoding='utf-8')
        units_after = read(units_command)
        (out / 'units-after.txt').write_text(units_after, encoding='utf-8')
        if units_before != units_after or after['sharing']['upstream']['kind'] != before['sharing']['upstream']['kind']:
            raise RuntimeError('service identity or selected upstream changed during verification')
        delta = count(counter_after) - count(counter_before)
        if delta <= 0:
            raise RuntimeError('no AP MASQUERADE packet increase was observed')
        result.update(passed=True, upstream=after['sharing']['upstream']['kind'], natPacketDelta=delta,
                      standardTLSHTTPS=True, dnsUDP=True, dnsTCP=True, dhcpGatewayDNS=True, servicesUnchanged=True)
        internet = json.loads((out / 'internet.json').read_text(encoding='utf-8-sig'))
        if args.preference_roundtrip:
            result['preferenceRoundtrip'] = internet.get('preferenceRoundtrip') is True
            if not result['preferenceRoundtrip']:
                raise RuntimeError('preference roundtrip was not verified')
        counters = internet.get('clientCounters', {})
        result['clientCountersStatus'] = counters.get('status', 'unknown')
        result['workerFreshAfterTraffic'] = internet.get('workerFreshAfterTraffic', False)
        result['clientCountersPositive'] = (counters.get('status') == 'available' and
                                            int(counters.get('receivedBytes') or '0') > 0 and
                                            int(counters.get('sentBytes') or '0') > 0)
        own_path, iw_path = out / 'own-client-counters.json', out / 'iw-associated-counters.txt'
        if own_path.exists() and iw_path.exists():
            own = json.loads(own_path.read_text(encoding='utf-8-sig')) or {}
            raw = iw_path.read_text(encoding='utf-8-sig')
            block = next((part for part in raw.split('Station ')[1:] if part.split()[0].lower() == own.get('mac', '').lower()), '')
            result['driverByteAttributesPresent'] = bool(re.search(r'\b(?:rx|tx) bytes:', block))
    except Exception as error:
        result['error'] = type(error).__name__ + ': ' + str(error)
    save('summary.json', result)
    print(json.dumps(result))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
