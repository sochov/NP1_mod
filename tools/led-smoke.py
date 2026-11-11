"""Bounded owner LED test over the management API; always ends with all zones off.
Requires an initially stopped NCC LED controller. Does not claim physical readback.
"""
import http.cookiejar
import json
import pathlib
import time
import urllib.request
import uuid

url = 'http://127.0.0.1'
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
csrf = ''

def call(path, body=None):
    request = urllib.request.Request(url + '/api/v1' + path,
        data=json.dumps(body).encode() if body is not None else None,
        headers={'Origin': url, 'Content-Type': 'application/json', 'X-CSRF-Token': csrf, 'X-Request-ID': str(uuid.uuid4())},
        method='PUT' if body is not None else 'GET')
    with opener.open(request, timeout=15) as response:
        return json.load(response)

csrf = call('/auth/session')['csrf']
initial = call('/device/led')['data']
settings = initial['settings']
if settings['mode'] != 'off' or initial['serviceState'] == 'active':
    raise SystemExit('Existing LED mode must be stopped explicitly before this test')
results = []
try:
    for mode in ('solid', 'rainbow'):
        current = call('/device/led')['data']['settings']
        next_settings = dict(settings, mode=mode, brightness=32, fps=10, autostart=False)
        result = call('/device/led', {'expectedRevision': current['revision'], 'settings': next_settings})['data']
        assert result['lastCommand']['mode'] == mode and result['lastCommand']['running']
        time.sleep(2)
        state = call('/device/led')['data']
        assert state['serviceState'] == 'active' and not state['lastCommand'].get('error')
        results.append({'mode': mode, 'command': 'accepted', 'frames': state['lastCommand']['frames']})
finally:
    current = call('/device/led')['data']['settings']
    result = call('/device/led', {'expectedRevision': current['revision'], 'settings': dict(settings, mode='off', autostart=False)})['data']
    assert result['lastCommand']['mode'] == 'off' and not result['lastCommand']['running']
report = {'results': results, 'finalMode': 'off', 'physicalReadback': 'unsupported'}
destination = pathlib.Path(__file__).resolve().parents[1] / 'tmp/led-smoke.json'
destination.write_text(json.dumps(report, indent=2), encoding='utf8')
print(json.dumps(report, indent=2))
