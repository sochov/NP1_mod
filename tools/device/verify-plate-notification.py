"""Verify plate notification effects through the same typed API as the UI.

Creates one disabled synthetic condition, tests LED-only effects and optionally
one spoken preview, then restores the original settings with revision checks.
Device observations stay under ignored tmp/. No stock service is changed.
Requires an existing pinned connection from tools/connect.py.
"""
import argparse
import concurrent.futures
import copy
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import time
import uuid

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'tools'))
import native_web_baseline as baseline


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--serial', required=True)
    parser.add_argument('--url', required=True)
    parser.add_argument('--speech', action='store_true')
    parser.add_argument('--front-led-only', action='store_true')
    args = parser.parse_args()
    spec = importlib.util.spec_from_file_location('stock', ROOT / 'tools/stock-services.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    client = module.ServiceClient(args.url)
    out = ROOT / 'tmp' / ('plate-notification-' + time.strftime('%Y%m%d-%H%M%S'))
    out.mkdir(exist_ok=False)
    before = baseline.capture(args.serial, out, 'before')
    original = client.call('/plate/settings')
    (out / 'original.private.json').write_text(json.dumps(original), encoding='utf8')
    current = original
    test_id = 'led-test-' + uuid.uuid4().hex[:12]
    results = []

    def state():
        # This firmware's old adbd does not reliably support exec-out.
        # Use its supported PTY shell, normalize CRs, and tolerate startup.
        for _ in range(6):
            result = subprocess.run(['adb', '-s', args.serial, 'shell', 'cat /run/np1-control/led-state.json'], capture_output=True, timeout=10, check=True)
            try:
                return json.loads(result.stdout.replace(b'\r', b''))
            except json.JSONDecodeError:
                time.sleep(.1)
        raise RuntimeError('LED worker state unavailable after startup')

    def save(value):
        nonlocal current
        observed = client.call('/plate/settings')
        if observed['revision'] != current['revision']:
            raise RuntimeError('Concurrent settings change: refused to overwrite it')
        current = client.call('/plate/settings', 'PUT', {'expectedRevision': current['revision'], 'settings': value})

    if args.front_led_only:
        saved = client.call('/device/led')['data']['settings']
        payload = {'settings': {'mode': 'rainbow', 'effect': 'steady', 'rgb': [64, 160, 255], 'brightness': 180}, 'durationMs': 1000}
        try:
            assert client.call('/device/led/test', 'POST', payload)['accepted']
            deadline = time.monotonic() + 2
            while state()['mode'] != 'screen':
                assert time.monotonic() < deadline, 'Test light never reached the worker'
                time.sleep(.1)
            time.sleep(1.5)
            assert state()['mode'] == 'off', 'Timed test did not turn off'
            payload['durationMs'] = 5000
            client.call('/device/led/test', 'POST', payload)
            time.sleep(.3)
            assert state()['mode'] == 'screen'
            client.call('/device/led/test/stop', 'POST', {})
            time.sleep(.4)
            assert state()['mode'] == 'off', 'Explicit stop did not turn off'
            assert client.call('/device/led')['data']['settings'] == saved, 'Temporary test rewrote the saved LED preset'
        finally:
            client.call('/device/led/test/stop', 'POST', {})
            after = baseline.capture(args.serial, out, 'after')
            baseline.unchanged(before, after)
        print('PASS: timed front LED, explicit stop, saved preset and stock services preserved')
        return

    try:
        rule = {'id': test_id, 'name': 'LED notification test', 'enabled': False,
                'region': '', 'class': '', 'kana': '', 'number': '42',
                'saveImage': False, 'speak': False, 'message': '', 'messageAudio': '',
                'led': {'mode': 'solid', 'effect': 'steady', 'rgb': [64, 160, 255], 'brightness': 180}}
        value = copy.deepcopy(original)
        value['rules'].append(rule)
        for mode, effect in [('solid', 'steady'), ('solid', 'breathe'), ('solid', 'flow'), ('rainbow', 'steady')]:
            rule['led'].update(mode=mode, effect=effect)
            value['rules'][-1] = copy.deepcopy(rule)
            save(value)
            samples = []
            with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
                future = pool.submit(client.call, '/plate/speech/preview', 'POST', {'ruleId': test_id})
                limit = time.monotonic() + 12
                while time.monotonic() < limit:
                    sample = state()
                    samples.append(sample)
                    if sample['mode'] == 'screen':
                        break
                    time.sleep(.1)
                else:
                    raise AssertionError('Notification never reached the LED worker')
                result = future.result(timeout=15)
            assert result.get('led') is True and result['spoken'] is False
            time.sleep(.5)
            samples.append(state())
            assert samples[-1]['mode'] == 'screen', 'Two-second tail ended early'
            if effect in ('breathe', 'flow') or mode == 'rainbow':
                assert samples[0].get('zones') != samples[-1].get('zones'), 'Animation was static'
            time.sleep(2)
            samples.append(state())
            assert samples[-1]['mode'] == 'off' and not samples[-1]['running'], 'LED did not turn off after notification'
            results.append({'mode': mode, 'effect': effect, 'samples': samples})

        if args.speech:
            rule['speak'] = True
            rule['led'].update(mode='rainbow')
            value['rules'][-1] = copy.deepcopy(rule)
            save(value)
            with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
                future = pool.submit(client.call, '/plate/speech/preview', 'POST', {'ruleId': test_id})
                samples = []
                deadline = time.monotonic() + 45
                while not future.done() and time.monotonic() < deadline:
                    samples.append(state())
                    time.sleep(.25)
                result = future.result(timeout=2)
            assert result.get('led') is True and result['spoken'] is True
            assert any(s['mode'] == 'screen' for s in samples)
            time.sleep(.4)
            assert state()['mode'] == 'screen'
            time.sleep(2)
            assert state()['mode'] == 'off'
            results.append({'speech': True, 'style': current['voice']['style'], 'samples': samples})
    finally:
        observed = client.call('/plate/settings')
        if observed['revision'] != current['revision']:
            raise RuntimeError('Concurrent settings change: test condition retained for manual review')
        save(original)
        after = baseline.capture(args.serial, out, 'after')
        # Notification setup may start the idle owner LED worker. All stock
        # services and already-running owner workers must keep their identities.
        unit = 'np1-control-led.service'
        if before['units'][unit]['ActiveState'] == 'inactive':
            assert after['units'][unit]['ActiveState'] == 'active'
            before['units'][unit] = after['units'][unit]
        baseline.unchanged(before, after)
        (out / 'results.private.json').write_text(json.dumps(results, indent=2), encoding='utf8')
    print('PASS: four LED effects, two-second tail then off, saved settings and stock services preserved')


if __name__ == '__main__':
    main()
