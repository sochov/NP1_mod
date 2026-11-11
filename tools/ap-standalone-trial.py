"""Controlled no-STA AP trial with original settings/unit/profile restoration.

Requires an already enabled NCC AP and a disconnected Windows Wi-Fi adapter.
Uses an independent /run unit/binary; does not replace installed software or
modify stock supplicant files. Device details/credentials stay under tmp/.
"""
import argparse
import http.cookiejar
import json
import pathlib
import re
import shlex
import subprocess
import time
import urllib.request
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--serial', required=True)
    parser.add_argument('--binary', required=True, type=pathlib.Path)
    parser.add_argument('--out', required=True, type=pathlib.Path)
    parser.add_argument('--interface', default='Wi-Fi')
    parser.add_argument('--url', default='http://127.0.0.1')
    parser.add_argument('--allow-windows-wifi-changes', action='store_true', help='Requires fresh explicit user authorization; disabled by default')
    args = parser.parse_args()
    if not args.allow_windows_wifi_changes:
        parser.error('Windows Wi-Fi changes are disabled; explicit user authorization and opt-in are required')
    root = pathlib.Path(__file__).resolve().parent.parent
    out = args.out.resolve()
    if not re.fullmatch(r'[A-Za-z0-9._:-]+', args.serial) or not args.binary.is_file():
        parser.error('invalid serial or binary')
    if root / 'tmp' not in out.parents or out.exists():
        parser.error('use a fresh private output directory beneath tmp/')
    if args.url not in ('http://127.0.0.1', 'http://192.168.226.1'):
        parser.error('use the local ADB-forward or fixed management AP URL')
    out.mkdir(parents=True)
    adb = ['adb', '-s', args.serial]
    nonce = uuid.uuid4().hex[:12]
    stage = '/run/ncc-ap-standalone-' + nonce
    unit_name = 'np1-control-ap-trial-' + nonce + '.service'
    unit_path = '/run/systemd/system/' + unit_name
    config_path = '/overlay/owner-apps/np1-control/private/ap/config.json'
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    csrf = ''
    monitor = None
    pending = None
    replaced = False
    unit_installed = False
    result = {'passed': False}

    def save(name, value):
        (out / name).write_text(json.dumps(value, ensure_ascii=False, indent=2), encoding='utf-8')

    def shell(command):
        script = command + '\ntrial_code=$?\nprintf "\\nNCC_STA_TRIAL_EXIT=%s\\n" "$trial_code"\n'
        response = subprocess.run(adb + ['shell', 'sh -c ' + shlex.quote(script)], capture_output=True, timeout=45)
        raw = response.stdout.replace(b'\r', b'')
        match = re.search(rb'\nNCC_STA_TRIAL_EXIT=(\d+)\n?$', raw)
        if not match or int(match.group(1)):
            (out / 'last-device-error.txt').write_bytes(response.stdout + response.stderr)
            raise RuntimeError('device operation failed; private diagnostic saved')
        return raw[:match.start()].decode('utf-8')

    def push(local, remote):
        response = subprocess.run(adb + ['push', str(local), remote], capture_output=True, timeout=30)
        if response.returncode:
            raise RuntimeError('trial file transfer failed')

    def api(path, body=None):
        headers = {'Origin': args.url}
        if body is not None:
            headers.update({'Content-Type': 'application/json', 'X-CSRF-Token': csrf, 'X-Request-ID': str(uuid.uuid4())})
        request = urllib.request.Request(args.url + '/api/v1' + path, data=json.dumps(body).encode() if body is not None else None, headers=headers)
        with opener.open(request, timeout=20) as response:
            return json.load(response)

    def wait_until(fn, seconds, description):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            if fn():
                return
            time.sleep(1)
        raise RuntimeError(description)

    def ap_ready():
        data = api('/network/hotspot')['data']
        return data.get('enabled') is True and data.get('dhcpReady') is True and 1 <= data.get('channel', 0) <= 13

    def old_connected():
        view = api('/network/wifi/networks')
        return view['status']['state'] == 'COMPLETED' and view['status']['networkID'] == before['status']['networkID'] and view['status']['ssid'] == before['status']['ssid'] and (view.get('transaction') or {}).get('state') != 'pending'

    def absent_connection():
        return api('/network/wifi/connect-new', {'ssid': 'NCC-unavailable-' + uuid.uuid4().hex[:12], 'security': 'wpa2', 'password': uuid.uuid4().hex})['id']

    def start_monitor(name, seconds):
        process = subprocess.Popen(['powershell', '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', str(root / 'tools/verify-ap-continuity.ps1'), '-AllowWindowsWiFiChanges', '-Interface', args.interface, '-HoldSeconds', str(seconds), '-ReadyFile', str(out / (name + '.ready')), '-MonitorPath', str(out / (name + '.jsonl')), '-AccessFile', str(out / 'client-access.json')], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        wait_until(lambda: (out / (name + '.ready')).exists() or process.poll() is not None, 50, 'AP client did not become ready')
        if process.poll() is not None:
            stdout, stderr = process.communicate()
            (out / (name + '-error.txt')).write_bytes(stdout + stderr)
            raise RuntimeError('AP client association failed')
        return process

    try:
        csrf = api('/auth/session')['csrf']
        before = api('/network/wifi/networks')
        if before['status']['state'] != 'COMPLETED' or before['status']['networkID'] is None:
            raise RuntimeError('trial requires an existing verified STA connection')
        save('wifi-before.json', before)
        stock_command = 'systemctl show app-@Communication.service app-@DriveRec.service app-@ImageRecog.service app-@Voice.service qmmf-server.service NEventWatcherCom.service QCMAP_ConnectionManagerd.service -p Id -p MainPID -p ActiveState -p SubState'
        stock_before = shell(stock_command)
        (out / 'stock-before.txt').write_text(stock_before, encoding='utf-8')
        original = shell('cat ' + config_path)
        original_config = json.loads(original)
        if not original_config.get('enabled') or shell('systemctl is-active np1-control-ap.service').strip() != 'active':
            raise RuntimeError('original NCC AP must be enabled and active')
        (out / 'config-original.json').write_text(original, encoding='utf-8')
        trial_config = {**original_config, 'standaloneFallback': True}
        save('config-trial.json', trial_config)
        save('client-access.json', {'ssid': original_config['ssid'], 'password': original_config['passphrase']})
        shell('mkdir -p ' + stage)
        push(args.binary, stage + '/ncc')
        push(root / 'tools/device/ap-channel.py', stage + '/ap-channel.py')
        unit = (root / 'systemd/np1-control-ap.service').read_text(encoding='utf-8')
        unit = '\n'.join(line for line in unit.splitlines() if not line.startswith('ExecStartPre=')) + '\n'
        unit = re.sub(r'^ExecStart=.*$', 'ExecStart=' + stage + '/ncc --mode ap-worker', unit, flags=re.MULTILINE)
        unit = re.sub(r'^ExecStopPost=.*$', 'ExecStopPost=' + stage + '/ncc --mode ap-cleanup', unit, flags=re.MULTILINE)
        (out / unit_name).write_text(unit, encoding='utf-8')
        push(out / unit_name, unit_path)
        unit_installed = True
        shell('chmod 0700 ' + stage + '/ncc')
        shell('systemctl stop np1-control-ap.service')
        replaced = True
        push(out / 'config-trial.json', config_path)
        shell('chmod 0600 ' + config_path)
        shell('systemctl daemon-reload')
        shell('systemctl start ' + unit_name)
        wait_until(ap_ready, 25, 'trial AP readiness failed')
        monitor = start_monitor('continuity', 125)
        pending = absent_connection()
        time.sleep(8)
        save('wifi-during-absence.json', api('/network/wifi/networks'))
        for label, command in [('ipv4-addresses', 'ip -4 -o address show'), ('ipv4-routes', 'ip -4 route show table all'), ('ipv6-routes', 'ip -6 route show table all')]:
            (out / ('absence-' + label + '.txt')).write_text(shell(command), encoding='utf-8')
        wait_until(old_connected, 112, 'automatic STA rollback failed')
        pending = None
        save('wifi-after-auto.json', api('/network/wifi/networks'))
        pending = absent_connection()
        time.sleep(5)
        api('/network/wifi/rollback', {'transactionID': pending})
        pending = None
        wait_until(old_connected, 20, 'manual STA rollback failed')
        stdout, stderr = monitor.communicate(timeout=150)
        (out / 'continuity-process.txt').write_bytes(stdout + stderr)
        if monitor.returncode:
            raise RuntimeError('continuous AP client verification failed')
        monitor = None
        pending = absent_connection()
        shell('systemctl stop ' + unit_name)
        shell('systemctl start ' + unit_name)
        wait_until(ap_ready, 25, 'standalone startup without connected STA failed')
        if api('/network/wifi/networks')['status']['state'] == 'COMPLETED':
            raise RuntimeError('startup test did not observe STA absence')
        monitor = start_monitor('startup', 8)
        api('/network/wifi/rollback', {'transactionID': pending})
        pending = None
        wait_until(old_connected, 20, 'startup test STA restoration failed')
        stdout, stderr = monitor.communicate(timeout=35)
        (out / 'startup-process.txt').write_bytes(stdout + stderr)
        if monitor.returncode:
            raise RuntimeError('standalone AP client verification failed')
        monitor = None
        after = api('/network/wifi/networks')
        save('wifi-after.json', after)
        stock_after = shell(stock_command)
        (out / 'stock-after.txt').write_text(stock_after, encoding='utf-8')
        if stock_after != stock_before:
            raise RuntimeError('stock service PID/state changed')
        if after['networks'] != before['networks'] or after['saved'] != before['saved']:
            raise RuntimeError('STA network/profile set changed')
        samples = [json.loads(line) for line in (out / 'continuity.jsonl').read_text().splitlines()]
        result.update(passed=True, samples=len(samples), observedSTAStates=sorted({row['staState'] for row in samples}), allHTTP=all(row['http'] for row in samples), allAPEnabled=all(row['apEnabled'] for row in samples), stockUnitsUnchanged=True)
    except Exception as error:
        (out / 'failure.txt').write_text(type(error).__name__ + ': ' + str(error), encoding='utf-8')
    finally:
        if pending:
            try:
                api('/network/wifi/rollback', {'transactionID': pending})
            except Exception:
                result['staRollbackNeedsReview'] = True
        if monitor:
            try:
                monitor.communicate(timeout=150)
            except Exception:
                result['clientCleanupNeedsReview'] = True
        if replaced:
            try:
                shell('systemctl stop ' + unit_name)
                push(out / 'config-original.json', config_path)
                shell('chmod 0600 ' + config_path)
                shell('systemctl start np1-control-ap.service')
                wait_until(ap_ready, 25, 'original AP did not return')
                result['originalAPRestored'] = True
            except Exception:
                result['originalAPRestored'] = False
                result['passed'] = False
        if unit_installed:
            try:
                shell('systemctl reset-failed ' + unit_name)
                shell('rm ' + unit_path)
                shell('systemctl daemon-reload')
            except Exception:
                result['trialUnitCleanupNeedsReview'] = True
        save('summary.json', result)
    print(json.dumps(result))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
