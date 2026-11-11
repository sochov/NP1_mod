"""Control one fixed stock service through the same typed Web API as the UI.

Examples: python tools/stock-services.py list
          python tools/stock-services.py restart --id voice
          python tools/stock-services.py verify --id opening

verify performs stop/start/restart and restores the original active state in
finally. It refuses concurrent changes and saves private evidence under tmp.
No unit file, enablement, arbitrary command or forced kill is exposed.
"""
import argparse
import hashlib
import http.cookiejar
import json
import pathlib
import socket
import ssl
import time
import urllib.request
import urllib.error
import uuid

ROOT = pathlib.Path(__file__).resolve().parents[1]


class ServiceClient:
    def __init__(self, base_url=None):
        access = json.loads((ROOT / 'tmp/device-access.json').read_text(encoding='utf8'))
        self.base = base_url or access['url']
        from urllib.parse import urlparse
        address = urlparse(self.base)
        if address.scheme != 'https' or not address.hostname or address.username or address.password or address.path not in ('','/') or address.query or address.fragment:
            raise ValueError('Explicit HTTPS management origin required')
        with socket.create_connection((address.hostname, address.port or 443), timeout=10) as conn:
            with ssl._create_unverified_context().wrap_socket(conn, server_hostname=address.hostname) as tls:
                if hashlib.sha256(tls.getpeercert(binary_form=True)).hexdigest() != access['fingerprint']:
                    raise RuntimeError('TLS fingerprint changed; inspect via tools/connect.py')
        self.opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=ssl._create_unverified_context()), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        self.csrf = ''
        self.csrf = self.call('/auth/session')['csrf']

    def call(self, path, method='GET', body=None):
        request = urllib.request.Request(self.base + '/api/v1' + path, method=method,
            data=None if body is None else json.dumps(body).encode(),
            headers={'Content-Type': 'application/json', 'Origin': self.base, 'X-CSRF-Token': self.csrf, 'X-Request-ID': str(uuid.uuid4())})
        try:
            with self.opener.open(request, timeout=25) as response:
                return json.load(response)
        except urllib.error.HTTPError as error:
            try:
                detail = json.loads(error.read(8192))
                raise RuntimeError('{}: {} ({})'.format(error.code, detail.get('reason', 'Service operation failed'), detail.get('code', ''))) from None
            finally:
                error.close()

    def services(self):
        value = self.call('/system/services')
        if value['status'] != 'available' or not isinstance(value['data'], list):
            raise RuntimeError('Stock service observation unavailable')
        return value['data']

    def action(self, service, action):
        from urllib.parse import quote
        result = self.call('/system/services/' + quote(service['id'], safe='') + '/actions', 'POST',
            {'action': action, 'expectedState': service['state'], 'expectedInvocationID': service['invocationID']})
        after = result['service']
        if result['status'] != 'applied' or result['action'] != action or after['id'] != service['id']:
            raise RuntimeError('Service operation not verified')
        if action == 'stop':
            assert after['state'] == 'inactive' and after['mainPID'] == 0
        else:
            assert after['state'] == 'active' and after['mainPID'] > 0 and after['invocationID']
            if action == 'restart':
                assert after['invocationID'] != service['invocationID']
        return after


def verify(client, target, before):
    if target['state'] != 'active' or not target['canStop'] or not target['canStart'] or not target['canRestart']:
        raise RuntimeError('Roundtrip requires an originally active, controllable service')
    evidence = {'before': before, 'operations': [], 'restored': False}
    current = target
    owned_invocations = {target['invocationID']}
    unchanged = lambda values: {s['id']: (s['state'], s['mainPID'], s['invocationID']) for s in values if s['id'] != target['id']}
    baseline = unchanged(before)
    try:
        for action in ('stop', 'start', 'restart'):
            current = client.action(current, action)
            owned_invocations.add(current['invocationID'])
            observed = client.services()
            assert unchanged(observed) == baseline, 'Unrelated service state changed'
            evidence['operations'].append({'action': action, 'service': current, 'othersPreserved': True})
    finally:
        observed = client.services()
        found = next(s for s in observed if s['id'] == target['id'])
        # Preserve an independent user's change; never blindly restart a new instance.
        if found['state'] != 'active':
            if found['invocationID'] not in owned_invocations:
                raise RuntimeError('Concurrent service change; refused blind restoration')
            found = client.action(found, 'start')
        assert found['state'] == 'active' and found['mainPID'] > 0
        evidence['after'] = client.services()
        evidence['restored'] = True
        folder = ROOT / 'tmp/stock-service-web'
        folder.mkdir(parents=True, exist_ok=True)
        (folder / (time.strftime('%Y%m%d-%H%M%S') + '-' + uuid.uuid4().hex[:6] + '.json')).write_text(json.dumps(evidence, indent=2) + '\n', encoding='utf8')
    assert unchanged(evidence['after']) == baseline
    print('PASS: individual Web stop/start/restart and original active state; unrelated services preserved')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('list', 'start', 'stop', 'restart', 'verify'))
    parser.add_argument('--id', help='Opaque service ID shown by list')
    args = parser.parse_args()
    if args.action != 'list' and not args.id:
        parser.error('--id is required')
    client = ServiceClient()
    services = client.services()
    if args.action == 'list':
        for service in services:
            print('{:<20} {:<10} {}'.format(service['id'], service['state'], service['name']))
        return
    target = next((s for s in services if s['id'] == args.id), None)
    if target is None:
        parser.error('Unknown service ID; use list')
    if args.action == 'verify':
        verify(client, target, services)
    else:
        after = client.action(target, args.action)
        print('Verified: {} {}'.format(after['id'], after['state']))


if __name__ == '__main__':
    main()
