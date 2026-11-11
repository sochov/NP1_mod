"""Read-only installed guard verification; never sends a service action.

An independent ADB HTTP forward avoids a stale shared test forward. The only
host mutation is that temporary forward, removed in finally. No stop/start,
reboot or unsafe reproduction is performed.
"""
import argparse
import json
import pathlib
import re
import subprocess
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--serial', required=True)
    args = parser.parse_args()
    if not re.fullmatch(r'[A-Za-z0-9._:-]+', args.serial):
        parser.error('valid explicit serial required')
    adb = ['adb', '-s', args.serial]
    units = ['qmmf-server.service', 'NEventWatcherAV.service', 'app-@DriveRec.service',
             'app-@ImageRecog.service', 'app-@RemoteMonitoring.service',
             'app-@VirtualPassenger.service', 'app-@System.service']
    def baseline():
        boot = subprocess.check_output(adb + ['shell', 'cat', '/proc/sys/kernel/random/boot_id'], timeout=10)
        state = subprocess.check_output(adb + ['shell', 'systemctl', 'show'] + units + ['-p', 'Id', '-p', 'MainPID', '-p', 'InvocationID', '-p', 'ActiveState'], timeout=10)
        return boot, state
    original = baseline()
    port = subprocess.check_output(adb + ['forward', 'tcp:0', 'tcp:80'], timeout=10).decode().strip()
    if not port.isdigit():
        raise RuntimeError('temporary ADB forward unavailable')
    try:
        with urllib.request.urlopen('http://127.0.0.1:' + port + '/api/v1/system/services', timeout=15) as response:
            value = json.load(response)
        assert value['status'] == 'available'
        services = {item['id']: item for item in value['data']}
        assert services['qmmf']['state'] == 'active' and services['event-av']['state'] == 'active'
        assert services['qmmf']['canStop'] is False and services['qmmf']['canRestart'] is False
        assert services['qmmf']['controlReason']
        assert services['event-av']['canStop'] is False and services['event-av']['canRestart'] is False
        assert services['event-av']['controlReason']
        assert baseline() == original, 'service state or boot changed during read-only observation'
        report = {'readOnly': True, 'qmmfStopRestartBlocked': True, 'avStopRestartBlocked': True,
                  'reasonsVisible': True, 'servicesAndBootPreserved': True, 'unsafeReproductionPerformed': False}
        (ROOT / 'tmp/camera-service-guards.json').write_text(json.dumps(report, indent=2) + '\n')
        print('PASS: installed camera dependency guards; no service action or reboot performed')
    finally:
        subprocess.run(adb + ['forward', '--remove', 'tcp:' + port], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)


if __name__ == '__main__':
    main()
