"""Owner-authorized Web roundtrip; fresh-CAS restore only, no blind fallback.

Call after native+Web worker updates and while other native operations are idle.
Runtime URL/cookies/CSRF and observations remain private; no master-volume PUT.
"""
import argparse
import http.cookiejar
import json
import pathlib
import ssl
import urllib.request
import uuid

ROOT = pathlib.Path(__file__).resolve().parents[1]
IDS = ('mute', 'volume-correction')


def inspect(observed):
    data = observed.get('data', {})
    mute, correction = data.get('mute', {}), data.get('correction', {})
    if (observed.get('status') != 'available' or type(data.get('masterVolume')) is not int or data['masterVolume'] < 0
            or type(mute.get('value')) is not int or mute['value'] not in (0, 1)
            or type(mute.get('enabled')) is not bool or mute['enabled'] != (mute['value'] == 0)
            or type(correction.get('value')) is not int or correction['value'] not in (0, 1, 2)
            or correction.get('label') != ('OFF', '弱', '強')[correction['value']]
            or data.get('writable') is not True):
        raise ValueError('verified writable audio state unavailable')
    return data


def value(data, setting):
    return data['mute' if setting == 'mute' else 'correction']['value']


def roundtrip(call, setting, requested, evidence):
    if setting not in IDS:
        raise ValueError('unknown audio setting')
    before = inspect(call('/device/native-audio'))
    original = value(before, setting)
    target = requested if requested is not None else (1-original if setting == 'mute' else (2 if original == 1 else 1))
    maximum = 1 if setting == 'mute' else 2
    if type(target) is not int or target < 0 or target > maximum or target == original:
        raise ValueError('distinct proven target required')
    evidence.update({'setting': setting, 'before': before, 'requested': target})
    try:
        applied = inspect(call('/device/native-audio/' + setting, 'PUT', {'expectedValue': original, 'value': target}))
        evidence['applied'] = applied
        if value(applied, setting) != target:
            raise ValueError('requested audio value not confirmed')
    finally:
        current = inspect(call('/device/native-audio'))
        actual = value(current, setting)
        if actual == target:
            inspect(call('/device/native-audio/' + setting, 'PUT', {'expectedValue': target, 'value': original}))
        elif actual != original:
            evidence['competingValue'] = actual
            raise ValueError('competing audio value; no blind restore')
        after = inspect(call('/device/native-audio'))
        evidence['restored'] = after
        if after != before:
            raise ValueError('original complete audio observation not restored')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--setting', choices=IDS, required=True)
    parser.add_argument('--serial', required=True)
    parser.add_argument('--next', type=int, choices=(0, 1, 2))
    args = parser.parse_args()
    if args.setting == 'mute' and args.next == 2:
        parser.error('mute accepts native0/1 only')
    access = json.loads((ROOT/'tmp/device-access.json').read_text(encoding='utf8'))
    base = access['url']
    opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=ssl._create_unverified_context()),
                                        urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    csrf = ''

    def call(path, method='GET', data=None):
        request = urllib.request.Request(base+'/api/v1'+path, method=method,
            data=None if data is None else json.dumps(data).encode(),
            headers={'Origin': base, 'X-CSRF-Token': csrf, 'X-Request-ID': str(uuid.uuid4()), 'Content-Type': 'application/json'})
        with opener.open(request, timeout=45) as reply:
            return json.load(reply)

    csrf = call('/auth/session')['csrf']
    evidence = {}
    try:
        from native_web_baseline import baseline
        with baseline(args.serial, ROOT/'tmp'/('native-audio-web-baseline-'+uuid.uuid4().hex)):
            roundtrip(call, args.setting, args.next, evidence)
    except Exception:
        raise SystemExit('Audio Web roundtrip failed or is inconclusive; inspect private evidence')
    finally:
        (ROOT/('tmp/native-audio-api-'+args.setting+'-roundtrip.json')).write_text(json.dumps(evidence, indent=2, ensure_ascii=False), encoding='utf8')
    print('PASS: native audio Web apply, owner/persistent read-back and original restoration')


if __name__ == '__main__':
    main()
