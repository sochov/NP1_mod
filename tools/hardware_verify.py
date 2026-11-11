"""Authenticated, read-only hardware smoke test. Explicit camera test opt-in via --camera.
Credentials and outputs stay in ignored tmp. No images or coordinates in the result report.
"""
import argparse
import hashlib
import http.cookiejar
import json
import pathlib
import ssl
import urllib.error
import urllib.parse
import urllib.request
import uuid
parser = argparse.ArgumentParser()
parser.add_argument('--camera', action='store_true')
args = parser.parse_args()
root = pathlib.Path(__file__).resolve().parent.parent
access = json.loads((root / 'tmp/device-access.json').read_text())
url = access['url']
host = urllib.parse.urlparse(url)
import socket
with socket.create_connection((host.hostname, host.port or 443), timeout=10) as sock:
    with ssl._create_unverified_context().wrap_socket(sock, server_hostname=host.hostname) as conn:
        if hashlib.sha256(conn.getpeercert(binary_form=True)).hexdigest() != access['fingerprint']:
            raise RuntimeError('TLS fingerprint changed; inspect over ADB')
jar = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=ssl._create_unverified_context()), urllib.request.HTTPCookieProcessor(jar))
csrf = ''
def call(path, method='GET', data=None, headers=None):
    hdr = {'Origin': url, 'X-Request-ID': str(uuid.uuid4()), 'X-CSRF-Token': csrf}
    if headers:
        hdr.update(headers)
    if data is not None:
        hdr['Content-Type'] = 'application/json'
    req = urllib.request.Request(url + '/api/v1' + path, data=json.dumps(data).encode() if data is not None else None, method=method, headers=hdr)
    return opener.open(req, timeout=30)
def get(path):
    with call(path) as response:
        return json.load(response)
report = {}
with call('/auth/session') as response:
    csrf = json.load(response)['csrf']
report['network_authentication'] = 'PASS: no Web password'
try:
    call('/camera/sessions/missing/stream')
    raise RuntimeError('Unknown camera session accepted')
except urllib.error.HTTPError as error:
    assert error.code == 403
summary = get('/system/summary')
report['summary'] = {k: v['status'] for k, v in summary.items()}
services = summary['services']['data']
assert all(s['state'] == 'active' for s in services), services
report['stock_services'] = 'all selected services active'
files = get('/storage/files?path=DRIVERECDATA/Video')
candidate = next(f for f in files if f['name'].lower().endswith('.mp4') and int(f['size']) > 100)
with call('/storage/file?path=' + urllib.parse.quote(candidate['path']), headers={'Range': 'bytes=0-99'}) as response:
    content = response.read()
    assert response.status == 206 and len(content) == 100
    assert content[4:8] == b'ftyp'
    assert response.headers['Content-Type'] == 'video/mp4'
    report['sd_range'] = 'PASS: verified MP4, 206, 100 bytes'
for bad in ('../etc/passwd', '/etc/passwd', 'DRIVERECDATA/../../etc/passwd'):
    try:
        call('/storage/files?path=' + urllib.parse.quote(bad))
        raise RuntimeError('Traversal accepted')
    except urllib.error.HTTPError as error:
        assert error.code == 400
report['traversal'] = 'PASS'
if args.camera:
    for source in ('front', 'in'):
        with call('/camera/sessions', 'POST', {'source': source}) as response:
            session = json.load(response)['id']
        try:
            with call('/camera/sessions/' + session + '/stream') as response:
                image = response.read()
                assert image[:2] == b'\xff\xd8'
                report['camera_' + source] = 'PASS: JPEG ' + str(len(image)) + ' bytes'
        finally:
            call('/camera/sessions/' + session, 'DELETE').close()
with call('/backup/export', 'POST') as response:
    backup = response.read()
    assert b'passwordHash' not in backup
report['backup_redaction'] = 'PASS'
with call('/auth/logout', 'POST'):
    pass
(root / 'tmp/hardware-smoke.json').write_text(json.dumps(report, indent=2, ensure_ascii=False), encoding='utf8')
print(json.dumps(report, indent=2, ensure_ascii=False))
