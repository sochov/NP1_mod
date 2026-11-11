"""SSH spare-USB AP probe. Default is read-only preflight, never Windows Wi-Fi.

The separate authorization flag changes only the pinned, idle USB phy. Existing
uplink and lab AP interfaces are protected. Secrets travel on SSH stdin only.
"""
import argparse
import datetime
import importlib.util
import io
import json
import pathlib
import tempfile
import time

ROOT = pathlib.Path(__file__).resolve().parents[1]


def private_path(value):
    path = pathlib.Path(value).resolve()
    if ROOT / 'tmp' not in path.parents:
        raise ValueError('private paths must stay beneath ignored tmp')
    return path


def atomic_json(path, value):
    path = private_path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix('.next')
    temporary.write_text(json.dumps(value, indent=2), encoding='utf-8')
    temporary.replace(path)


def prepare_phase_paths(paths):
    paths = [private_path(path) for path in paths]
    if len(set(paths)) != 4 or any(path.exists() or path.with_suffix('.next').exists() for path in paths):
        raise ValueError('four distinct fresh coordinator phase paths required')
    for parent in set(path.parent for path in paths):
        parent.mkdir(parents=True, exist_ok=True)
        with tempfile.NamedTemporaryFile(dir=parent, prefix='.ncc-phase-preflight-', delete=True):
            pass
    return paths


def consume_remote(stdin, stdout, events, paths, out, allow_changes, gate_wait=None):
    gate_wait = gate_wait or wait_gate
    result = dict(passed=False, completeReceived=False, hostErrors=[])
    prepared = cleanup_sent = False
    for line in stdout:
        try:
            value = json.loads(line)
        except Exception as error:
            result['hostErrors'].append(dict(stage='parse-event', type=type(error).__name__))
            continue
        try:
            events.write(json.dumps(value) + '\n')
            events.flush()
        except Exception as error:
            result['hostErrors'].append(dict(stage='record-event', type=type(error).__name__))
        phase = value.get('phase')
        if phase == 'preflight':
            result['passed'] = value.get('passed') is True
        elif phase == 'complete':
            result.update(passed=value.get('passed') is True, restored=value.get('restored') is True, completeReceived=True)
        elif allow_changes and phase in ('prepared', 'traffic', 'failure'):
            try:
                if phase == 'prepared':
                    prepared = True
                    atomic_json(paths[0], value)
                    gate = gate_wait(paths[1], 85)
                    stdin.write(json.dumps(gate) + '\n')
                    stdin.flush()
                else:
                    atomic_json(paths[2], value)
                    if phase == 'failure':
                        atomic_json(out / 'failure-diagnostic.json', value)
                    if prepared and not cleanup_sent:
                        try:
                            gate = gate_wait(paths[3], 105)
                        except Exception as error:
                            result['hostErrors'].append(dict(stage='cleanup-gate', type=type(error).__name__))
                            gate = dict(cleanup=True, restored=False)
                        stdin.write(json.dumps(gate) + '\n')
                        stdin.flush()
                        cleanup_sent = True
            except Exception as error:
                result['hostErrors'].append(dict(stage='coordinate-' + phase, type=type(error).__name__))
                # Keep SSH open and drain the remote finally/complete receipt.
                # No fabricated NP1-restored proof is sent after a host failure.
                try:
                    stdin.write(json.dumps(dict(proceed=False) if phase == 'prepared' else dict(cleanup=True, restored=False)) + '\n')
                    stdin.flush()
                    if phase != 'prepared':
                        cleanup_sent = True
                except Exception as write_error:
                    result['hostErrors'].append(dict(stage='cancel-gate', type=type(write_error).__name__))
    if allow_changes and not result['completeReceived']:
        result['passed'], result['restored'] = False, False
    result['passed'] = result['passed'] and not result['hostErrors']
    return result


def wait_gate(path, timeout):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if path.exists():
            return json.loads(path.read_text(encoding='utf-8-sig'))
        time.sleep(.1)
    raise TimeoutError('Coordinator gate timed out')


def bootstrap_command():
    # This argv contains neither a credential nor a host-specific identifier.
    return "sudo -S -p '' /usr/bin/python3 -u -c 'import sys,json; p=json.loads(sys.stdin.readline()); exec(compile(p.pop(\"source\"),\"<ncc-usb-probe>\",\"exec\")); main(p)'"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--access', type=pathlib.Path, default=ROOT / 'tmp/analysis-pc-access.json')
    parser.add_argument('--profile', type=pathlib.Path, required=True)
    parser.add_argument('--ap-access', type=pathlib.Path)
    parser.add_argument('--out', type=pathlib.Path, required=True)
    parser.add_argument('--allow-spare-usb-wifi-changes', action='store_true')
    parser.add_argument('--prepared-file', type=pathlib.Path)
    parser.add_argument('--proceed-file', type=pathlib.Path)
    parser.add_argument('--traffic-file', type=pathlib.Path)
    parser.add_argument('--cleanup-file', type=pathlib.Path)
    args = parser.parse_args()
    paths = [args.prepared_file, args.proceed_file, args.traffic_file, args.cleanup_file]
    if args.allow_spare_usb_wifi_changes and (args.ap_access is None or not all(paths)):
        parser.error('USB changes require AP access and all four coordinator gates')
    if not args.allow_spare_usb_wifi_changes and (args.ap_access or any(paths)):
        parser.error('Mutation inputs require explicit spare-USB authorization')
    out = private_path(args.out)
    if out.exists():
        parser.error('fresh private output directory required')
    profile = json.loads(private_path(args.profile).read_text(encoding='utf-8-sig'))
    script = (ROOT / 'tools/remote/usb-wifi-ap-probe.py').read_text(encoding='utf-8')
    # Validate the profile without importing the remote Linux-only controller.
    definition = script.split('class RemoteProbe:', 1)[0]
    namespace = {}
    exec(compile(definition, '<usb-profile-validation>', 'exec'), namespace)
    namespace['validate_profile'](profile)
    access = json.loads(private_path(args.access).read_text(encoding='utf-8-sig'))
    payload = dict(source=script, profile=profile, preflightOnly=not args.allow_spare_usb_wifi_changes,
                   allowSpareUSBWiFiChanges=args.allow_spare_usb_wifi_changes)
    if args.allow_spare_usb_wifi_changes:
        payload['ap'] = json.loads(private_path(args.ap_access).read_text(encoding='utf-8-sig'))
    out.mkdir(parents=True)
    if args.allow_spare_usb_wifi_changes:
        paths = prepare_phase_paths(paths)
    spec = importlib.util.spec_from_file_location('analysis_pc', ROOT / 'tools/analysis-pc.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    client = module.connect(args.access)
    result = dict(passed=False, readOnly=not args.allow_spare_usb_wifi_changes)
    try:
        stdin, stdout, stderr = client.exec_command(bootstrap_command(), timeout=360)
        stdin.write(access['password'] + '\n' + json.dumps(payload) + '\n')
        stdin.flush()
        if not args.allow_spare_usb_wifi_changes:
            stdin.channel.shutdown_write()
        record_error = False
        try:
            events = (out / 'events.jsonl').open('w', encoding='utf-8')
        except OSError:
            events, record_error = io.StringIO(), True
        with events:
            result.update(consume_remote(stdin, stdout, events, paths, out, args.allow_spare_usb_wifi_changes))
        if record_error:
            result['hostErrors'].append(dict(stage='open-events', type='OSError'))
            result['passed'] = False
        (out / 'stderr.txt').write_bytes(stderr.read())
        result['exitCode'] = stdout.channel.recv_exit_status()
        result['passed'] = result['passed'] and result['exitCode'] == 0
    finally:
        client.close()
        result['observedAt'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        atomic_json(out / 'summary.json', result)
    print(json.dumps(result))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
