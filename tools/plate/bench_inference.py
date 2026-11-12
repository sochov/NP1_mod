"""Measure TFLite inference latency on NP1 for plate-recognition planning.

Pushes model files to a private tmpfs directory on the device, runs the
stock `/usr/bin/benchmark_model` with each backend (CPU / XNNPACK / GPU /
NNAPI / Hexagon), then removes the directory. It never changes rootfs, stock
services or settings. Stock service health is compared before and after:
unit state, MainPID, NRestarts, export inode updates and DriveRec file growth.

Model files and raw logs stay in ignored tmp/plate/bench/.
"""
import argparse
import datetime
import json
import pathlib
import re
import shlex
import subprocess
import time

ROOT = pathlib.Path(__file__).resolve().parents[2]
DEVICE_DIR = '/tmp/np1-plate-bench'
BENCH = '/usr/bin/benchmark_model'
STOCK_UNITS = [
    'app-@Communication.service', 'app-@DriveRec.service', 'app-@ImageRecog.service',
    'app-@Voice.service', 'qmmf-server.service',
]
FRONT_EXPORT = '/tmp/application/imagerecog/imagedata/frontcamera/imagedata/value'
VIDEO_DIR = '/mnt/sdcard/DRIVERECDATA/Video'
DSP_LIB_PATH = '/usr/lib/rfsa/adsp;/dsp/cdsp;/dsp'

BACKENDS = {
    'cpu1': ['--num_threads=1'],
    'cpu2': ['--num_threads=2'],
    'cpu4': ['--num_threads=4'],
    'xnnpack2': ['--num_threads=2', '--use_xnnpack=true'],
    'xnnpack4': ['--num_threads=4', '--use_xnnpack=true'],
    'gpu': ['--use_gpu=true'],
    'nnapi': ['--use_nnapi=true'],
    'hexagon': ['--use_hexagon=true', '--hexagon_lib_path=/usr/lib'],
}


class Device:
    def __init__(self, serial):
        self.serial = serial

    def shell(self, command, timeout=60):
        result = subprocess.run(['adb', '-s', self.serial, 'shell', command],
                                capture_output=True, timeout=timeout)
        return result.returncode, result.stdout.decode(errors='replace')

    def push(self, local, remote):
        subprocess.run(['adb', '-s', self.serial, 'push', str(local), remote],
                       check=True, capture_output=True, timeout=120)


def stock_snapshot(device):
    snapshot = {}
    for unit in STOCK_UNITS:
        _, out = device.shell(f'systemctl show -p ActiveState -p MainPID -p NRestarts {shlex.quote(unit)}')
        snapshot[unit] = dict(line.split('=', 1) for line in out.split() if '=' in line)
    inodes = set()
    for _ in range(6):
        _, out = device.shell(f'stat -c %i {FRONT_EXPORT} 2>/dev/null')
        inodes.add(out.strip())
        time.sleep(0.3)
    snapshot['frontExportUpdating'] = len(inodes) > 1
    _, out = device.shell(f'ls -t {VIDEO_DIR}/*.MP4 2>/dev/null | head -1 | xargs stat -c "%n %s"')
    snapshot['latestRecording'] = out.strip()
    return snapshot


def compare_stock(before, after):
    problems = []
    for unit in STOCK_UNITS:
        b, a = before[unit], after[unit]
        if a.get('ActiveState') != 'active':
            problems.append(f'{unit} is {a.get("ActiveState")}')
        if b.get('MainPID') != a.get('MainPID') or b.get('NRestarts') != a.get('NRestarts'):
            problems.append(f'{unit} restarted ({b.get("MainPID")}->{a.get("MainPID")})')
    if not after['frontExportUpdating']:
        problems.append('front export not updating')
    if before['latestRecording'] == after['latestRecording']:
        problems.append('DriveRec latest recording did not grow')
    return problems


def external_actions(device, since):
    """Stock-service actions requested through the NCC API during the run (not caused by this tool)."""
    _, out = device.shell(f'journalctl --no-pager --since {shlex.quote(since)} 2>/dev/null '
                          f'| grep -E "stock-service id=.* action=" | sed -E "s/^.*(stock-service)/\\1/"')
    return [line.strip() for line in out.splitlines() if line.strip()]


def device_now(device):
    _, out = device.shell('date -u "+%Y-%m-%d %H:%M:%S"')
    return out.strip()


def parse_result(text):
    avg = re.findall(r'Inference \(avg\): ([0-9.e+]+)', text) or re.findall(r'avg=([0-9.e+]+)', text)
    init = re.search(r'Init: ([0-9.e+]+)', text)
    delegated = re.search(r'Replacing (\d+) node\(s\) with delegate \(([^)]+)\) node, yielding (\d+) partitions', text)
    applied = re.search(r'(Explicitly applied|Applied) (\S+) delegate', text)
    failure = None
    for pattern in (r'Failed to apply \S+ delegate', r'ERROR: .*', r'Could not \S+.*', r'Segmentation fault'):
        match = re.search(pattern, text)
        if match:
            failure = match.group(0)[:160]
            break
    return {
        'avgMs': round(float(avg[-1]) / 1000, 2) if avg else None,
        'initMs': round(float(init.group(1)) / 1000, 1) if init else None,
        'delegatedNodes': int(delegated.group(1)) if delegated else None,
        'partitions': int(delegated.group(3)) if delegated else None,
        'delegateApplied': bool(applied or delegated),
        'failure': failure,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--serial', required=True)
    parser.add_argument('--model', action='append', type=pathlib.Path, required=True)
    parser.add_argument('--backend', action='append', choices=sorted(BACKENDS))
    parser.add_argument('--runs', type=int, default=30)
    parser.add_argument('--timeout', type=int, default=180)
    args = parser.parse_args()
    backends = args.backend or list(BACKENDS)
    device = Device(args.serial)
    out_dir = ROOT / 'tmp' / 'plate' / 'bench' / datetime.datetime.now().strftime('%Y%m%d-%H%M%S')
    out_dir.mkdir(parents=True, exist_ok=True)

    since = device_now(device)
    before = stock_snapshot(device)
    device.shell(f'rm -rf {DEVICE_DIR} && mkdir -p {DEVICE_DIR}')
    results = []
    try:
        for model in args.model:
            remote = f'{DEVICE_DIR}/{model.name}'
            device.push(model, remote)
            for backend in backends:
                flags = ' '.join(BACKENDS[backend])
                # busybox timeout takes `-t SECS`; coreutils takes `SECS`.
                command = (f'cd {DEVICE_DIR} && if timeout -t 1 true 2>/dev/null; '
                           f'then T="timeout -t {args.timeout}"; else T="timeout {args.timeout}"; fi; '
                           f'ADSP_LIBRARY_PATH={shlex.quote(DSP_LIB_PATH)} '
                           f'$T {BENCH} --graph={remote} {flags} '
                           f'--num_runs={args.runs} --warmup_runs=3 --enable_op_profiling=false 2>&1; '
                           f'echo "exit=$?"')
                started = time.time()
                try:
                    _, text = device.shell(command, timeout=args.timeout + 30)
                except subprocess.TimeoutExpired:
                    text = 'host timeout'
                (out_dir / f'{model.stem}-{backend}.log').write_text(text, encoding='utf-8')
                row = {'model': model.name, 'backend': backend, 'wallS': round(time.time() - started, 1)}
                row.update(parse_result(text))
                exit_code = re.search(r'exit=(\d+)', text)
                row['exit'] = int(exit_code.group(1)) if exit_code else None
                results.append(row)
                print(json.dumps(row, ensure_ascii=False), flush=True)
    finally:
        device.shell(f'rm -rf {DEVICE_DIR}')
        time.sleep(2)
        after = stock_snapshot(device)
        problems = compare_stock(before, after)
        external = external_actions(device, since)
        (out_dir / 'results.json').write_text(json.dumps(
            {'results': results, 'stockProblems': problems, 'externalStockActions': external},
            ensure_ascii=False, indent=2), encoding='utf-8')
        print('stock check:', 'OK' if not problems else '; '.join(problems))
        if external:
            print('external stock-service actions during run (NCC API):', '; '.join(external))
        print('logs:', out_dir.relative_to(ROOT))


if __name__ == '__main__':
    main()
