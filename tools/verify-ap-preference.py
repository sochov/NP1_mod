"""Web-only AP preference wifi -> cellular -> original, with CAS restoration.

Requires an already associated AP client with a DHCP IPv4 lease. Does not join
Wi-Fi, restart a service, change STA/CM, or accept arbitrary device commands.
All device identities/config/leases are recorded only beneath repository tmp/.
"""
import argparse
import http.cookiejar
import ipaddress
import json
import pathlib
import re
import subprocess
import urllib.request
import urllib.error
import uuid

UNITS = ('np1-control.service', 'np1-control-broker.service', 'np1-control-ap.service')

def require(condition, code):
    if not condition:
        raise RuntimeError(code)

def client_addresses(ap):
    require(ap.get('clientsAvailable') is True, 'AP_PREFERENCE_CLIENTS_UNKNOWN')
    clients = {}
    for item in ap.get('clients') or []:
        if not item.get('authorized'):
            continue
        mac, address = item.get('mac', '').lower(), item.get('ipv4', '')
        require(re.fullmatch(r'(?:[0-9a-f]{2}:){5}[0-9a-f]{2}', mac), 'AP_PREFERENCE_CLIENT_ID_INVALID')
        # A second associated device may not have requested DHCP yet. Prove
        # positive leases for known IPv4 clients, and track all associations
        # separately instead of inventing an address for that other device.
        if not address:
            continue
        try:
            require(ipaddress.ip_address(address).version == 4, 'AP_PREFERENCE_CLIENT_IPV4_UNKNOWN')
        except ValueError:
            raise RuntimeError('AP_PREFERENCE_CLIENT_IPV4_UNKNOWN')
        require(mac not in clients, 'AP_PREFERENCE_CLIENT_DUPLICATE')
        clients[mac] = address
    require(bool(clients), 'AP_PREFERENCE_ASSOCIATED_DHCP_CLIENT_REQUIRED')
    return clients

def client_associations(ap):
    require(ap.get('clientsAvailable') is True, 'AP_PREFERENCE_CLIENTS_UNKNOWN')
    result = set()
    for item in ap.get('clients') or []:
        if item.get('authorized'):
            mac = item.get('mac', '').lower()
            require(bool(re.fullmatch(r'(?:[0-9a-f]{2}:){5}[0-9a-f]{2}', mac)) and mac not in result, 'AP_PREFERENCE_CLIENT_ID_INVALID')
            result.add(mac)
    return result

def lease_addresses(raw):
    leases = {}
    for line in raw.splitlines():
        parts = line.split()
        if len(parts) < 3 or not re.fullmatch(r'(?:[0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}', parts[1]):
            continue
        mac, address = parts[1].lower(), parts[2]
        require(mac not in leases, 'AP_PREFERENCE_LEASE_DUPLICATE')
        try:
            require(ipaddress.ip_address(address).version == 4, 'AP_PREFERENCE_LEASE_INVALID')
        except ValueError:
            raise RuntimeError('AP_PREFERENCE_LEASE_INVALID')
        leases[mac] = address
    return leases

def radio_tuple(config):
    return (config.get('enabled'), config.get('ssid'), config.get('passphrase'), config.get('standaloneFallback', True))

def call_http_json(opener, request, save):
    try:
        with opener.open(request, timeout=25) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        # HTTP failures are useful private evidence too. Never emit response
        # bodies, request headers, credentials or device identities to stdout.
        raw = error.read(8193)
        diagnostic = {'status': error.code, 'reason': str(error.reason)[:256],
                      'body': raw[:8192].decode('utf-8', errors='replace'),
                      'truncated': len(raw) > 8192}
        try:
            payload = json.loads(raw) if len(raw) <= 8192 else None
            code = payload.get('code', payload.get('error')) if isinstance(payload, dict) else None
            if isinstance(code, dict):
                code = code.get('code')
            if isinstance(code, str) and re.fullmatch(r'[A-Z][A-Z0-9_]{0,191}', code):
                diagnostic['errorCode'] = code
            if isinstance(payload, dict) and isinstance(payload.get('reason'), str):
                diagnostic['responseReason'] = payload['reason'][:512]
        except (ValueError, UnicodeError):
            pass
        save('http-error-' + uuid.uuid4().hex + '.json', diagnostic)
        error.close()
        raise

class PreferenceVerification:
    def __init__(self, call, read, save):
        self.call, self.read, self.save = call, read, save
        self.baseline = None
        self.last = None
        self.owned = set()
        self.attempts = 0
        self.result = {'passed': False, 'originalRestored': False}

    def snapshot(self, name):
        ap = self.call('/network/hotspot')['data']
        cfg = json.loads(self.read(['cat', '/overlay/owner-apps/np1-control/private/ap/config.json']))
        ownership = json.loads(self.read(['cat', '/run/np1-control-ap/private/ownership.json']))
        units = self.read(['systemctl', 'show'] + list(UNITS) + ['-p', 'Id', '-p', 'MainPID', '-p', 'ActiveState', '-p', 'SubState', '-p', 'InvocationID'])
        raw_leases = self.read(['cat', '/run/np1-control-ap/private/leases'])
        record = {'ap': ap, 'config': cfg, 'ownership': ownership, 'units': units, 'leases': raw_leases}
        self.save(name + '.json', record)
        return record

    @staticmethod
    def pair(record):
        settings = record['ap'].get('settings', {})
        return settings.get('revision'), settings.get('preference')

    def check(self, record, expected=None):
        ap, cfg = record['ap'], record['config']
        pair = self.pair(record)
        require(isinstance(pair[0], int) and pair[0] >= 0 and pair[1] in ('wifi', 'cellular'), 'AP_PREFERENCE_FEATURE_NOT_DEPLOYED')
        require((cfg.get('revision'), cfg.get('preference', 'cellular')) == pair, 'AP_PREFERENCE_CONFIG_VIEW_MISMATCH')
        if expected is not None:
            require(pair == expected, 'AP_PREFERENCE_REVISION_READBACK_FAILED')
        sharing = ap.get('sharing') or {}
        require(ap.get('enabled') is True and ap.get('dhcpReady') is True, 'AP_PREFERENCE_AP_NOT_READY')
        require(sharing.get('preference') == pair[1], 'AP_PREFERENCE_WORKER_READBACK_FAILED')
        require(sharing.get('ready') is True and (sharing.get('upstream') or {}).get('kind') == 'wifi', 'AP_PREFERENCE_EXPECTED_WIFI_FALLBACK_UNAVAILABLE')
        require(record['units'].count('ActiveState=active') == 3 and len(re.findall(r'^MainPID=[1-9][0-9]*$', record['units'], re.M)) == 3, 'AP_PREFERENCE_SERVICE_NOT_READY')
        clients, leases = client_addresses(ap), lease_addresses(record['leases'])
        require(all(leases.get(mac) == address for mac, address in clients.items()), 'AP_PREFERENCE_DHCP_MAPPING_UNVERIFIED')
        if self.baseline is not None:
            before = self.baseline
            require(radio_tuple(cfg) == radio_tuple(before['config']) and ap.get('settings', {}).get('ssid') == before['ap']['settings']['ssid'], 'AP_PREFERENCE_RADIO_OR_SECRET_CHANGED')
            require(record['units'] == before['units'], 'AP_PREFERENCE_SERVICE_RESTARTED')
            require(record['ownership'] == before['ownership'], 'AP_PREFERENCE_HOSTAPD_DHCP_OR_INTERFACE_CHANGED')
            require(client_associations(ap) == client_associations(before['ap']), 'AP_PREFERENCE_CLIENT_ASSOCIATION_CHANGED')
            require(all(clients.get(mac) == address for mac, address in client_addresses(before['ap']).items()), 'AP_PREFERENCE_CLIENT_DHCP_CHANGED')
        return pair

    def change(self, target, name, restoring=False):
        current = self.snapshot(name + '-before')
        pair = self.pair(current)
        if restoring:
            require(radio_tuple(current['config']) == radio_tuple(self.baseline['config']), 'AP_PREFERENCE_FOREIGN_RADIO_CHANGE_NO_RESTORE')
        else:
            self.check(current)
        require(pair == self.last, 'AP_PREFERENCE_CONCURRENT_CHANGE')
        expected = (pair[0] + 1, target)
        self.owned.add(expected)  # Also identify a committed PUT whose reply is lost.
        self.attempts += 1
        reply = self.call('/network/hotspot', 'PUT', {'expectedRevision': pair[0], 'settings': {'preference': target}})
        self.save(name + '-response.json', reply)
        after = self.snapshot(name + '-after')
        self.last = self.check(after, expected)

    def restore(self):
        current = self.snapshot('restore-before')
        pair = self.pair(current)
        require(pair in self.owned and radio_tuple(current['config']) == radio_tuple(self.baseline['config']), 'AP_PREFERENCE_FOREIGN_CHANGE_NO_BLIND_RESTORE')
        if pair == self.pair(self.baseline):
            # A rejected PUT may already have atomically restored the exact
            # baseline revision. Confirm it rather than issuing another PUT.
            self.check(current, pair)
            self.last = pair
            self.result['originalRestored'] = True
            return
        self.last = pair
        self.change(self.baseline['ap']['settings']['preference'], 'original', restoring=True)
        self.result['originalRestored'] = True

    def run(self):
        before = self.snapshot('baseline')
        self.last = self.check(before)
        self.baseline = before
        self.owned.add(self.last)
        completed = False
        try:
            self.change('wifi', 'wifi')
            self.change('cellular', 'cellular')
            completed = True
        finally:
            if self.attempts:
                try:
                    self.restore()
                finally:
                    self.result['webPUTs'] = self.attempts
        self.result.update(passed=completed and self.result['originalRestored'], webPUTs=self.attempts,
                           servicesUnchanged=True, hostapdDHCPUnchanged=True, radioAndSecretPreserved=True,
                           clientDHCPUnchanged=True, clientCount=len(client_addresses(before['ap'])), actualUpstream='wifi')
        return self.result

def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--serial', required=True)
    p.add_argument('--out', type=pathlib.Path, required=True)
    args = p.parse_args()
    root = pathlib.Path(__file__).resolve().parents[1]
    out = args.out.resolve()
    require(bool(re.fullmatch(r'[A-Za-z0-9._:-]+', args.serial)) and root / 'tmp' in out.parents and not out.exists(), 'AP_PREFERENCE_VALID_SERIAL_AND_FRESH_PRIVATE_OUTPUT_REQUIRED')
    out.mkdir(parents=True)
    adb = ['adb', '-s', args.serial]
    forwarded = None
    verification = None
    result = {'passed': False, 'originalRestored': False}
    try:
        require(b'uid=0(root)' in subprocess.check_output(adb + ['shell', 'id'], timeout=10), 'AP_PREFERENCE_ROOT_ADB_REQUIRED')
        port = subprocess.check_output(adb + ['forward', 'tcp:0', 'tcp:80'], timeout=10).decode().strip()
        require(port.isdigit() and 0 < int(port) < 65536, 'AP_PREFERENCE_PRIVATE_FORWARD_FAILED')
        forwarded = 'tcp:' + port
        base = 'http://127.0.0.1:' + port
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        csrf = ''
        def call(path, method='GET', body=None):
            request = urllib.request.Request(base + '/api/v1' + path, method=method,
                data=None if body is None else json.dumps(body).encode(),
                headers={'Origin': base, 'Content-Type': 'application/json', 'X-CSRF-Token': csrf, 'X-Request-ID': str(uuid.uuid4())})
            return call_http_json(opener, request, save)
        def read(command):
            raw = subprocess.check_output(adb + ['shell'] + command, timeout=10, stderr=subprocess.DEVNULL)
            require(len(raw) <= 65536, 'AP_PREFERENCE_PRIVATE_READ_OVERSIZE')
            return raw.replace(b'\r', b'').decode('utf-8')
        def save(name, value):
            (out / name).write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
        csrf = call('/auth/session')['csrf']
        verification = PreferenceVerification(call, read, save)
        result = verification.run()
    except Exception as error:
        if verification is not None:
            result.update(verification.result)
        code = str(error)
        result['error'] = code if re.fullmatch(r'AP_PREFERENCE_[A-Z0-9_]+', code) else type(error).__name__
    finally:
        if forwarded:
            subprocess.run(adb + ['forward', '--remove', forwarded], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
        (out / 'summary.json').write_text(json.dumps(result, indent=2) + '\n', encoding='utf-8')
    print(json.dumps(result))
    return 0 if result['passed'] else 1

if __name__ == '__main__':
    raise SystemExit(main())
