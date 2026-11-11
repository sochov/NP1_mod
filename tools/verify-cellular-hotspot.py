"""Coordinate a prepared Windows AP client with the fixed bounded STA OFF trial.

Disabled by default; Windows Wi-Fi changes require fresh explicit authorization.
The NP1 network writer is the fixed trial_wifi_sta.py roundtrip, whose trap
restores the original STA. Its child PC helper changes Windows Wi-Fi temporarily.
No AP configuration, main route, host route, APN or credential is changed here.
"""
import argparse
import datetime as dt
import json
import pathlib
import re
import subprocess
import sys
import time
import urllib.request


def timestamp(value):
    if not isinstance(value, str):
        raise ValueError('missing phase timestamp')
    result = dt.datetime.fromisoformat(value.replace('Z', '+00:00'))
    if result.tzinfo is None:
        raise ValueError('phase timestamp is not UTC-qualified')
    return result.astimezone(dt.timezone.utc)


def cellular_interface(value):
    data = value.get('data') or {}
    share = data.get('sharing') or {}
    upstream = share.get('upstream') or {}
    interface = upstream.get('interface', '')
    if value.get('status') != 'available' or data.get('enabled') is not True or data.get('dhcpReady') is not True or share.get('ready') is not True or upstream.get('kind') != 'cellular' or not re.fullmatch(r'usb0(?:\.[12])?', interface):
        return None
    return interface


def nat_packets(raw, interface):
    found = []
    for line in raw.splitlines():
        fields = line.split()
        if len(fields) >= 9 and fields[0].isdigit() and fields[2] == 'MASQUERADE' and fields[6] == interface and fields[7] == '192.168.226.0/24':
            found.append(int(fields[0]))
    if len(found) != 1:
        raise ValueError('verified cellular NAT rule is missing or ambiguous')
    return found[0]


def coordinate(io):
    result = {'passed': False, 'restored': False, 'pcPreparedBeforeSTAOff': False}
    native_started = False
    try:
        io.start_pc()
        until = io.now() + dt.timedelta(seconds=55)
        while io.now() < until:
            prepared = io.phase('prepared')
            if prepared:
                if not all(prepared.get(key) is True for key in ('prepared', 'association', 'dhcpGateway', 'dhcpDNS', 'httpUI', 'clientObserved')):
                    raise ValueError('PC preparation was not proven')
                break
            if io.phase('traffic') or io.pc_finished():
                raise ValueError('PC failed before the STA writer started')
            io.sleep(.1)
        else:
            raise ValueError('PC preparation timed out; STA writer was not started')
        result['pcPreparedBeforeSTAOff'] = True
        io.start_sta()
        native_started = True
        until = io.now() + dt.timedelta(seconds=30)
        while io.now() < until:
            off = io.phase('off')
            if off:
                break
            if io.sta_finished():
                raise ValueError('STA trial ended without correlated OFF proof')
            io.sleep(.1)
        else:
            raise ValueError('correlated STA OFF proof timed out')
        off_at, deadline = timestamp(off.get('offAt')), timestamp(off.get('restoreDeadline'))
        if off.get('offConfirmed') is not True or off.get('holdSeconds') != io.hold_seconds or not io.hold_seconds - 2 <= (deadline - off_at).total_seconds() <= io.hold_seconds + 2 or off_at > io.now() + dt.timedelta(seconds=2) or deadline <= io.now():
            raise ValueError('invalid bounded OFF phase proof')
        result['correlatedSTAOff'] = True
        interface = None
        # Reserve enough of the fixed OFF window for actual DNS/TLS traffic.
        while io.now() < deadline - dt.timedelta(seconds=12):
            value = io.ap()
            interface = cellular_interface(value)
            if interface:
                io.save('cellular-before.json', value)
                break
            io.sleep(.4)
        if not interface:
            raise ValueError('no verified cellular sharing path within the bounded window')
        baseline = nat_packets(io.nat('before'), interface)
        io.write_phase('proceed', {'proceed': True, 'expectedUpstream': 'cellular', 'expiresAt': (deadline - dt.timedelta(seconds=1)).isoformat()})
        traffic = None
        stable = True
        while io.now() < deadline:
            if cellular_interface(io.ap()) != interface:
                stable = False
            traffic = io.phase('traffic')
            if traffic:
                break
            io.sleep(.3)
        if not traffic or not stable or not all(traffic.get(key) is True for key in ('passed', 'dnsUDP', 'dnsTCP', 'standardTLSHTTPS', 'interfaceBound')) or traffic.get('upstream') != 'cellular':
            raise ValueError('4G DNS/TLS traffic was not proven on one stable cellular path')
        if not off_at <= timestamp(traffic.get('trafficStartedAt')) <= timestamp(traffic.get('observedAt')) < deadline:
            raise ValueError('traffic proof is outside the OFF window')
        after = io.ap()
        if cellular_interface(after) != interface or io.now() >= deadline:
            raise ValueError('cellular path expired before NAT read-back')
        io.save('cellular-after.json', after)
        delta = nat_packets(io.nat('after'), interface) - baseline
        if delta <= 0:
            raise ValueError('no matching cellular MASQUERADE packet increase')
        result.update(cellularTrafficVerified=True, standardTLSHTTPS=True, dnsUDP=True, dnsTCP=True, interfaceBound=True, cellularNATPacketDelta=delta, cellularPathStable=True)
    except Exception as error:
        result['error'] = type(error).__name__ + ': ' + str(error)
    finally:
        # Release a PC still waiting for the measurement gate on every failure.
        if not io.phase('proceed'):
            io.write_phase('proceed', {'proceed': False, 'expectedUpstream': 'cellular'})
        try:
            restoration = io.finish_sta() if native_started else {'restored': True, 'notStarted': True, 'returnCode': 0}
        except Exception:
            restoration = {'restored': False, 'returnCode': -1}
        result['restored'] = restoration.get('restored') is True
        io.write_phase('cleanup', {'restored': result['restored']})
        try:
            pc = io.finish_pc()
        except Exception:
            pc = {'returnCode': -1}
        result['pcCleaned'] = pc.get('profileCleaned') is True
        if native_started and restoration.get('returnCode') == 0 and result['restored'] and result.get('cellularTrafficVerified') and pc.get('returnCode') == 0 and result['pcCleaned']:
            try:
                preserved = io.check_preserved()
                result.update(preserved)
                result['passed'] = all(preserved.values())
            except Exception:
                result['error'] = 'restored preservation read-back was not completed'
    return result


class RealIO:
    def __init__(self, root, serial, out, interface, hold_seconds, allow_windows_wifi_changes=False):
        if not allow_windows_wifi_changes:
            raise PermissionError("Windows Wi-Fi mutation requires explicit authorization")
        self.root, self.serial, self.out, self.interface, self.hold_seconds = root, serial, out, interface, hold_seconds
        self.paths = {key: out / name for key, name in {'prepared': 'pc-prepared.json', 'proceed': 'pc-proceed.json', 'traffic': 'traffic-complete.json', 'cleanup': 'pc-cleanup.json', 'off': 'off-ready.json', 'complete': 'sta-complete.json'}.items()}
        self.pc = self.sta = None
        self.pc_log = self.sta_log = None
        self.units_command = 'systemctl show np1-control.service np1-control-broker.service np1-control-ap.service -p Id -p MainPID -p ActiveState -p SubState'
        self.units_before = self.read(self.units_command)
        self.config_before = self.read('cat /overlay/owner-apps/np1-control/private/ap/config.json')
        self.profile_command = "if test -f /overlay/owner-apps/np1-control/private/wifi/profiles.json; then sha256sum /overlay/owner-apps/np1-control/private/wifi/profiles.json; else printf 'absent\\n'; fi"
        self.profiles_before = self.read(self.profile_command)
        self.save('units-before.json', self.units_before)
        config = json.loads(self.config_before)
        self.save('client-access.json', {'ssid': config['ssid'], 'password': config['passphrase']})

    def read(self, command):
        response = subprocess.run(['adb', '-s', self.serial, 'shell', command], capture_output=True, timeout=15)
        if response.returncode:
            raise RuntimeError('bounded read-only device operation failed')
        return response.stdout.replace(b'\r', b'').decode()

    def save(self, name, value):
        (self.out / name).write_text(json.dumps(value, ensure_ascii=False, indent=2), encoding='utf-8')

    def now(self):
        return dt.datetime.now(dt.timezone.utc)

    def sleep(self, seconds):
        time.sleep(seconds)

    def phase(self, name):
        path = self.paths[name]
        return json.loads(path.read_text(encoding='utf-8-sig')) if path.exists() else None

    def write_phase(self, name, value):
        path = self.paths[name]
        temporary = path.with_suffix('.next')
        temporary.write_text(json.dumps(value), encoding='utf-8')
        temporary.replace(path)

    def start_pc(self):
        self.pc_log = (self.out / 'pc-process.txt').open('wb')
        args = ['powershell', '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', str(self.root / 'tools/verify-ap-internet.ps1'), '-AllowWindowsWiFiChanges', '-Interface', self.interface, '-AccessFile', str(self.out / 'client-access.json'), '-ResultFile', str(self.out / 'internet.json'), '-DeviceSerial', self.serial, '-PreparedFile', str(self.paths['prepared']), '-ProceedFile', str(self.paths['proceed']), '-TrafficCompleteFile', str(self.paths['traffic']), '-CleanupGateFile', str(self.paths['cleanup']), '-ExpectedUpstream', 'cellular', '-SkipCounterWait']
        self.pc = subprocess.Popen(args, stdout=self.pc_log, stderr=subprocess.STDOUT)

    def start_sta(self):
        self.sta_log = (self.out / 'sta-process.txt').open('wb')
        args = [sys.executable, str(self.root / 'tools/trial_wifi_sta.py'), '--serial', self.serial, '--mode', 'roundtrip', '--records', str(self.out / 'sta'), '--hold-seconds', str(self.hold_seconds), '--off-ready-file', str(self.paths['off']), '--complete-file', str(self.paths['complete'])]
        self.sta = subprocess.Popen(args, stdout=self.sta_log, stderr=subprocess.STDOUT)

    def pc_finished(self):
        return self.pc is not None and self.pc.poll() is not None

    def sta_finished(self):
        return self.sta is not None and self.sta.poll() is not None

    def ap(self):
        with urllib.request.urlopen('http://127.0.0.1/api/v1/network/hotspot', timeout=3) as response:
            return json.load(response)

    def nat(self, name):
        raw = self.read('iptables -w 2 -t nat -L NCC_AP_NAT_V1 -v -n -x')
        (self.out / ('nat-' + name + '.txt')).write_text(raw, encoding='utf-8')
        return raw

    def finish_sta(self):
        if self.sta is None:
            return {'restored': False, 'returnCode': -1}
        try:
            code = self.sta.wait(timeout=210)
        except subprocess.TimeoutExpired:
            # Never kill an in-flight OFF controller or replace its recovery.
            return {'restored': False, 'returnCode': -1}
        finally:
            self.sta_log.close()
        proof = self.phase('complete') or {}
        return {'restored': proof.get('restored') is True, 'returnCode': code}

    def finish_pc(self):
        if self.pc is None:
            return {'returnCode': -1}
        try:
            code = self.pc.wait(timeout=130)
        except subprocess.TimeoutExpired:
            return {'returnCode': -1}
        finally:
            self.pc_log.close()
        result = json.loads((self.out / 'internet.json').read_text(encoding='utf-8-sig')) if (self.out / 'internet.json').exists() else {}
        return {'returnCode': code, 'profileCleaned': result.get('profileCleaned') is True}

    def check_preserved(self):
        units = self.read(self.units_command)
        self.save('units-after.json', units)
        return {'ownServicesUnchanged': units == self.units_before, 'APCredentialsAndSettingsUnchanged': self.read('cat /overlay/owner-apps/np1-control/private/ap/config.json') == self.config_before, 'savedSTAPSKUnchanged': self.read(self.profile_command) == self.profiles_before, 'originalSTAAndStockPIDsRestored': self.phase('complete').get('restored') is True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--serial', required=True)
    parser.add_argument('--out', type=pathlib.Path, required=True)
    parser.add_argument('--interface', default='Wi-Fi')
    parser.add_argument('--hold-seconds', type=int, choices=[30, 45], default=45)
    parser.add_argument('--allow-windows-wifi-changes', action='store_true', help='Requires fresh explicit user authorization; disabled by default')
    args = parser.parse_args()
    if not args.allow_windows_wifi_changes:
        parser.error('Windows Wi-Fi changes are disabled; explicit user authorization and opt-in are required')
    root = pathlib.Path(__file__).resolve().parent.parent
    out = args.out.resolve()
    if not re.fullmatch(r'[A-Za-z0-9._:-]+', args.serial) or root / 'tmp' not in out.parents or out.exists():
        parser.error('explicit serial and fresh private tmp/ output required')
    out.mkdir(parents=True)
    result = coordinate(RealIO(root, args.serial, out, args.interface, args.hold_seconds, args.allow_windows_wifi_changes))
    (out / 'summary.json').write_text(json.dumps(result, indent=2), encoding='utf-8')
    print(json.dumps(result))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
