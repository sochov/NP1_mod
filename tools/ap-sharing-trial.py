"""Bounded hotspot tethering trial with a separate binary/unit and exact cleanup.

Private snapshots stay in tmp/. Stock routes/rules/firewall chains are not
replaced. The Windows check sets IP_UNICAST_IF and binds its AP source address; no
host route changes are required. It deletes its temporary Wi-Fi profile afterwards.
"""
import argparse
import http.cookiejar
import json
import pathlib
import re
import shlex
import subprocess
import time
import uuid
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--serial', required=True)
    parser.add_argument('--binary', type=pathlib.Path, required=True)
    parser.add_argument('--out', type=pathlib.Path, required=True)
    parser.add_argument('--interface', default='Wi-Fi')
    parser.add_argument('--expect-upstream', choices=['auto', 'wifi', 'cellular'], default='auto')
    parser.add_argument('--wan-recovery', action='store_true', help='90-second typed STA timeout, AP continuity and Internet recovery')
    args = parser.parse_args()
    root = pathlib.Path(__file__).resolve().parent.parent
    out = args.out.resolve()
    if not re.fullmatch(r'[A-Za-z0-9._:-]+', args.serial) or not args.binary.is_file():
        parser.error('invalid serial or binary')
    if root / 'tmp' not in out.parents or out.exists():
        parser.error('use a fresh private output directory under tmp/')
    out.mkdir(parents=True)
    adb = ['adb', '-s', args.serial]
    nonce = uuid.uuid4().hex[:12]
    stage = '/run/ncc-ap-sharing-' + nonce
    name = 'np1-control-ap-sharing-' + nonce + '.service'
    unit_path = '/run/systemd/system/' + name
    replaced = unit_installed = False
    result = {'passed': False}
    pending = None
    monitor = None
    loss_process = None
    csrf = ''
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def save(name, value):
        (out / name).write_text(json.dumps(value, ensure_ascii=False, indent=2), encoding='utf-8')

    def shell(command):
        script = command + '\ntrial_code=$?\nprintf "\\nNCC_SHARING_EXIT=%s\\n" "$trial_code"\n'
        response = subprocess.run(adb + ['shell', 'sh -c ' + shlex.quote(script)], capture_output=True, timeout=50)
        raw = response.stdout.replace(b'\r', b'')
        match = re.search(rb'\nNCC_SHARING_EXIT=(\d+)\n?$', raw)
        if not match or int(match.group(1)):
            (out / 'last-device-error.txt').write_bytes(response.stdout + response.stderr)
            raise RuntimeError('device operation failed; private diagnostic saved')
        return raw[:match.start()].decode('utf-8')

    def push(local, remote):
        response = subprocess.run(adb + ['push', str(local), remote], capture_output=True, timeout=30)
        if response.returncode:
            raise RuntimeError('trial file transfer failed')

    def status():
        # The separately staged worker publishes the new schema before the
        # installed public API binary has been upgraded. Read its bounded owner
        # status directly; the client independently verifies HTTP/API access.
        raw = shell('cat /run/np1-control-ap/status.json')
        if len(raw) > 65536:
            raise RuntimeError('worker status exceeds bound')
        return json.loads(raw)

    def api(path, body=None):
        headers = {'Origin': 'http://127.0.0.1'}
        if body is not None:
            headers.update({'Content-Type': 'application/json', 'X-CSRF-Token': csrf, 'X-Request-ID': str(uuid.uuid4())})
        request = urllib.request.Request('http://127.0.0.1/api/v1' + path,
                                        data=json.dumps(body).encode() if body is not None else None, headers=headers)
        with opener.open(request, timeout=20) as response:
            return json.load(response)

    def wait(ready, seconds, description):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            if ready():
                return
            time.sleep(1)
        raise RuntimeError(description)

    def capture(label):
        commands = {'filter': 'iptables -w 2 -t filter -S', 'nat': 'iptables -w 2 -t nat -S',
                    'routes4': 'ip -4 route show table all', 'rules4': 'ip -4 rule show',
                    'routes6': 'ip -6 route show table all', 'rules6': 'ip -6 rule show',
                    'forward': 'cat /proc/sys/net/ipv4/ip_forward',
                    'stock': 'systemctl show app-@Communication.service app-@DriveRec.service app-@ImageRecog.service app-@Voice.service qmmf-server.service NEventWatcherCom.service QCMAP_ConnectionManagerd.service -p Id -p MainPID -p ActiveState -p SubState'}
        snapshot = {key: shell(command) for key, command in commands.items()}
        save(label + '.json', snapshot)
        return snapshot

    try:
        if shell('systemctl is-active np1-control-ap.service').strip() != 'active':
            raise RuntimeError('trial requires an active original NCC AP')
        config = json.loads(shell('cat /overlay/owner-apps/np1-control/private/ap/config.json'))
        save('client-access.json', {'ssid': config['ssid'], 'password': config['passphrase']})
        original = capture('before')
        shell('mkdir -p ' + stage)
        push(args.binary, stage + '/ncc')
        push(root / 'tools/device/ap-channel.py', stage + '/ap-channel.py')
        unit = (root / 'systemd/np1-control-ap.service').read_text(encoding='utf-8')
        unit = '\n'.join(line for line in unit.splitlines() if not line.startswith('ExecStartPre=')) + '\n'
        unit = re.sub(r'^ExecStart=.*$', 'ExecStart=' + stage + '/ncc --mode ap-worker', unit, flags=re.MULTILINE)
        unit = re.sub(r'^ExecStopPost=.*$', 'ExecStopPost=' + stage + '/ncc --mode ap-cleanup', unit, flags=re.MULTILINE)
        (out / name).write_text(unit, encoding='utf-8')
        push(out / name, unit_path)
        unit_installed = True
        shell('chmod 0700 ' + stage + '/ncc')
        # The conventional lock must exist before systemd builds its read-only
        # namespace; every iptables process still uses the same global lock.
        shell('test -f /run/xtables.lock || touch /run/xtables.lock')
        shell('systemctl stop np1-control-ap.service')
        replaced = True
        shell('systemctl daemon-reload')
        shell('systemctl start ' + name)
        wait(lambda: status().get('enabled') is True and status().get('sharing', {}).get('ready') is True, 35,
             'trial hotspot/DNS/NAT readiness failed')
        save('hotspot-ready.json', status())
        ready = status()
        save('client-access.json', {'ssid': config['ssid'], 'password': config['passphrase'],
                                    'upstream': ready['sharing']['upstream']['kind']})
        capture('during')
        if args.wan_recovery:
            if ready['sharing']['upstream']['kind'] != 'wifi':
                raise RuntimeError('STA loss trial requires verified Wi-Fi as selected upstream')
            wifi_before = api('/network/wifi/networks')
            save('wifi-before.json', wifi_before)
            for helper_name in ('ap-upstream-loss.py', 'ap-upstream-loss.sh'):
                push(root / 'tools/device' / helper_name, stage + '/' + helper_name)
            monitor = subprocess.Popen(['powershell', '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File',
                                        str(root / 'tools/verify-ap-continuity.ps1'), '-Interface', args.interface,
                                        '-HoldSeconds', '45', '-ReadyFile', str(out / 'continuity.ready'),
                                        '-MonitorPath', str(out / 'continuity.jsonl'), '-AccessFile', str(out / 'client-access.json')],
                                       stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            wait(lambda: (out / 'continuity.ready').exists() or monitor.poll() is not None, 45,
                 'AP continuity client did not associate')
            if monitor.poll() is not None:
                raise RuntimeError('AP continuity client exited before STA loss')
            loss_process = subprocess.Popen(adb + ['shell', 'sh ' + stage + '/ap-upstream-loss.sh ' + stage],
                                            stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            wait(lambda: shell('test -f ' + stage + '/sta-loss-phase && cat ' + stage + '/sta-loss-phase || true').strip() == 'holding',
                 10, 'bounded STA disconnect did not start')

            def without_wifi_upstream():
                current = status()
                share = current.get('sharing', {})
                return current.get('enabled') and (share.get('state') == 'waiting-upstream' or
                                                   share.get('ready') and share.get('upstream', {}).get('kind') == 'cellular')

            wait(without_wifi_upstream, 12, 'sharing did not stop using the disconnected STA')
            absent = status()
            save('hotspot-without-wan.json', absent)
            save('wifi-during-loss.json', api('/network/wifi/networks'))
            capture('without-wan')
            stdout, stderr = loss_process.communicate(timeout=55)
            (out / 'loss-process.txt').write_bytes(stdout + stderr)
            if b'NCC_STA_LOSS_RESTORED' not in stdout or b'NCC_STA_LOSS_RESTORE_FAILED' in stdout:
                raise RuntimeError('bounded original STA restoration failed')
            loss_process = None
            wait(lambda: status().get('sharing', {}).get('ready') is True, 25, 'sharing did not recover after STA returned')
            wifi_after = api('/network/wifi/networks')
            save('wifi-after.json', wifi_after)
            if wifi_after['status']['state'] != 'COMPLETED' or wifi_after['status']['networkID'] != wifi_before['status']['networkID'] or wifi_after['status']['ssid'] != wifi_before['status']['ssid'] or wifi_after['saved'] != wifi_before['saved'] or wifi_after['networks'] != wifi_before['networks']:
                raise RuntimeError('original STA identity/network/profile set changed after recovery')
            stdout, stderr = monitor.communicate(timeout=75)
            (out / 'continuity-process.txt').write_bytes(stdout + stderr)
            if monitor.returncode:
                raise RuntimeError('AP HTTP continuity failed during upstream loss')
            monitor = None
            samples = [json.loads(line) for line in (out / 'continuity.jsonl').read_text().splitlines()]
            if not samples or not all(row['http'] and row['apEnabled'] for row in samples):
                raise RuntimeError('AP HTTP continuity samples contain a failure')
            result.update(wanLossAPContinuity=True, originalSTARecovered=True, sharingRecovered=True,
                          continuitySamples=len(samples), observedSTAStates=sorted({row['staState'] for row in samples}))
        check = subprocess.run(['powershell', '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File',
                                str(root / 'tools/verify-ap-internet.ps1'), '-Interface', args.interface,
                                '-AccessFile', str(out / 'client-access.json'), '-ResultFile', str(out / 'internet.json')],
                               capture_output=True, timeout=135)
        (out / 'client-process.txt').write_bytes(check.stdout + check.stderr)
        if check.returncode:
            raise RuntimeError('Windows tethering verification failed; private result saved')
        internet = json.loads((out / 'internet.json').read_text(encoding='utf-8-sig'))
        if args.expect_upstream != 'auto' and internet.get('upstream') != args.expect_upstream:
            raise RuntimeError('observed upstream did not match requested trial expectation')
        save('hotspot-client.json', status())
        # NAT counters after the interface-bound HTTPS transfer are independent
        # evidence of AP forwarding, in addition to Windows' explicit host route.
        (out / 'nat-counters.txt').write_text(shell('iptables -w 2 -t nat -L NCC_AP_NAT_V1 -v -n -x'), encoding='utf-8')
        result.update(passed=True, upstream=internet['upstream'], dhcpGateway=True, dhcpDNS=True,
                      dnsUDP=True, dnsTCP=True, httpsViaAP=True, clientObserved=True)
    except Exception as error:
        result['error'] = type(error).__name__ + ': ' + str(error)
        try:
            (out / 'private-failure.txt').write_text(shell('cat /run/np1-control-ap/private/failure.json; cat /run/np1-control-ap/private/dnsmasq.log; cat /run/np1-control-ap/private/startup.jsonl'), encoding='utf-8')
        except Exception:
            pass
    finally:
        if loss_process:
            try:
                loss_process.communicate(timeout=55)
            except Exception:
                result['staRollbackNeedsReview'] = True
        if pending:
            try:
                api('/network/wifi/rollback', {'transactionID': pending})
            except Exception:
                result['staRollbackNeedsReview'] = True
        if monitor:
            try:
                monitor.communicate(timeout=135)
            except Exception:
                result['clientCleanupNeedsReview'] = True
        if replaced:
            try:
                shell('systemctl stop ' + name)
                shell(stage + '/ncc --mode ap-cleanup')
                shell('systemctl start np1-control-ap.service')
                wait(lambda: status().get('enabled') is True, 25, 'original hotspot did not return')
                time.sleep(3)
                after = capture('after')
                # Connected AP's own dynamic interface route has a new ifindex,
                # but its text and every stock resource must remain identical.
                normalize = lambda value: [' '.join(line.split()) for line in value.splitlines()
                                           if line.strip() and ' dev nccap0 ' not in ' ' + line + ' ']
                unchanged = all(normalize(after[key]) == normalize(original[key]) for key in original)
                result['stockAndRoutingRestored'] = unchanged
                result['originalAPRestored'] = True
                result['passed'] = result['passed'] and unchanged
            except Exception:
                result['originalAPRestored'] = False
                result['passed'] = False
        if unit_installed:
            try:
                shell('systemctl reset-failed ' + name + ' 2>/dev/null || true')
                shell('rm ' + unit_path)
                shell('systemctl daemon-reload')
            except Exception:
                result['trialUnitCleanupNeedsReview'] = True
        save('summary.json', result)
    print(json.dumps(result))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
