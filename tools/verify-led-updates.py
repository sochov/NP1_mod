"""Verify rapid LED changes without service restarts; restore saved settings."""
import http.cookiejar
import json
import pathlib
import ssl
import shutil
import subprocess
import urllib.request
import uuid

ROOT = pathlib.Path(__file__).resolve().parents[1]
BASE = json.loads((ROOT / 'tmp/device-access.json').read_text())['url']
OPENER = urllib.request.build_opener(
    urllib.request.HTTPSHandler(context=ssl._create_unverified_context()),
    urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
CSRF = ''


def call(path, method='GET', data=None):
    request = urllib.request.Request(BASE + '/api/v1' + path, method=method,
        data=None if data is None else json.dumps(data).encode(),
        headers={'Origin': BASE, 'X-CSRF-Token': CSRF,
                 'X-Request-ID': str(uuid.uuid4()), 'Content-Type': 'application/json'})
    with OPENER.open(request, timeout=20) as response:
        return json.load(response)


def service():
    args = [ADB, '-s', SERIAL, 'shell',
            'systemctl show np1-control-led.service -p MainPID -p Result -p ActiveState']
    value = subprocess.check_output(args, text=True).replace('\r', '')
    return dict(line.split('=', 1) for line in value.splitlines() if '=' in line)


if __name__ == '__main__':
    import argparse
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--serial', required=True)
    parser.add_argument('--adb', default=shutil.which('adb'))
    arguments = parser.parse_args()
    SERIAL, ADB = arguments.serial, arguments.adb
    if not ADB: raise SystemExit('adb not found; pass --adb')
    CSRF = call('/auth/session')['csrf']
    original = call('/device/led')['data']['settings']
    evidence = {'changes': []}
    try:
        for index in range(9):
            current = call('/device/led')['data']['settings']
            desired = dict(original, mode=['solid', 'rainbow', 'off'][index % 3],
                           rgb=[32 + index, 55, 80], brightness=70 + index,
                           autostart=False)
            result = call('/device/led', 'PUT',
                          {'expectedRevision': current['revision'], 'settings': desired})['data']
            assert result['settings']['mode'] == desired['mode']
            assert result['lastCommand']['revision'] == result['settings']['revision']
            observed = service()
            assert observed['ActiveState'] == 'active' and observed['Result'] == 'success'
            assert int(observed['MainPID']) > 0
            evidence['changes'].append(observed)
        assert len({item['MainPID'] for item in evidence['changes']}) == 1
    finally:
        current = call('/device/led')['data']['settings']
        restored = call('/device/led', 'PUT',
                        {'expectedRevision': current['revision'], 'settings': original})['data']
        evidence['restored'] = all(restored['settings'][key] == value
                                   for key, value in original.items() if key != 'revision')
        (ROOT / 'tmp/led-updates.json').write_text(json.dumps(evidence, indent=2) + '\n')
        assert evidence['restored']
    print('PASS: nine rapid LED edits including off/on; same active PID; saved settings restored')
