"""Read-only long-run Web/API monitor over an explicitly selected ADB target.

No service, network, modem or reboot action is sent. Store only health/status
metadata beneath tmp. A run is not a successful soak merely because it ended:
continuous healthy time resets on an error or a detected device boot change.
"""
import argparse
import datetime
import json
import pathlib
import re
import subprocess
import time
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parents[1]


class HealthWindow:
    def __init__(self, started, maximum_gap=90):
        self.started = started
        self.healthy_since = None
        self.last_boot = None
        self.samples = 0
        self.errors = 0
        self.boot_changes = 0
        self.longest = 0
        self.last_sample = None
        self.maximum_gap = maximum_gap

    def observe(self, now, health):
        self.samples += 1
        boot = health.get('boot')
        changed = self.last_boot is not None and boot is not None and boot != self.last_boot
        gap = self.last_sample is not None and now - self.last_sample > self.maximum_gap
        if changed:
            self.boot_changes += 1
        if boot is not None:
            self.last_boot = boot
        if not health.get('healthy') or changed or gap:
            if self.healthy_since is not None:
                previous = now if self.last_sample is None else self.last_sample
                self.longest = max(self.longest, previous - self.healthy_since)
            self.healthy_since = None
        if not health.get('healthy'):
            self.errors += 1
        elif self.healthy_since is None:
            self.healthy_since = now
        self.last_sample = now
        return self.report(now)

    def report(self, now):
        current = 0 if self.healthy_since is None else max(0, now - self.healthy_since)
        return {'samples': self.samples, 'errors': self.errors, 'bootChanges': self.boot_changes,
                'elapsedSeconds': round(max(0, now - self.started), 1),
                'continuousHealthySeconds': round(current, 1),
                'longestHealthySeconds': round(max(self.longest, current), 1),
                'qualified24Hours': current >= 24 * 3600, 'qualified72Hours': current >= 72 * 3600}


def summarize(value):
    def data(name):
        observation = value.get(name)
        return observation.get('data') if isinstance(observation, dict) and observation.get('status') == 'available' else None
    boot = data('bootID')
    if not isinstance(boot, str) or not re.fullmatch(r'[a-f0-9-]{36}', boot):
        boot = None
    rows = data('services')
    services = {row['id']: row['state'] for row in rows if isinstance(row, dict) and isinstance(row.get('id'), str) and isinstance(row.get('state'), str)} if isinstance(rows, list) else {}
    required = ('communication', 'drive-rec', 'image-recog', 'voice', 'qmmf', 'event-av', 'event-com', 'setting')
    sd, hotspot, tailscale = data('sd'), data('hotspot'), data('tailscale')
    # Expected offline mobile/GNSS values are preserved as states, not healthy zeros.
    hotspot_ready = bool(isinstance(hotspot, dict) and hotspot.get('enabled') is True and hotspot.get('dhcpReady') is True)
    healthy = boot is not None and hotspot_ready and sd is not None and all(services.get(name) == 'active' for name in required)
    return {'healthy': healthy, 'boot': boot, 'services': services,
            'statuses': {name: value.get(name, {}).get('status', 'unknown') for name in ('cpu', 'ram', 'sd', 'hotspot', 'tailscale', 'modem', 'gps', 'recording')},
            'hotspotReady': hotspot_ready,
            'tailscaleOnline': tailscale.get('online') if isinstance(tailscale, dict) else None,
            'storageObserved': sd is not None}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--serial', required=True)
    parser.add_argument('--out', type=pathlib.Path, required=True)
    parser.add_argument('--hours', type=float, default=72)
    parser.add_argument('--interval', type=float, default=30)
    args = parser.parse_args()
    out = args.out.resolve()
    if not re.fullmatch(r'[A-Za-z0-9._:-]+', args.serial) or ROOT / 'tmp' not in out.parents or out.exists() or not 0 < args.hours <= 168 or not 5 <= args.interval <= 300:
        parser.error('explicit serial, fresh tmp directory, 0<hours<=168 and 5<=interval<=300 required')
    out.mkdir(parents=True)
    adb = ['adb', '-s', args.serial]
    forwarded = None
    started = time.monotonic()
    health = HealthWindow(started, maximum_gap=3 * args.interval)
    summary = {}
    completed = False
    def remove_forward():
        if forwarded:
            try:
                subprocess.run(adb + ['forward', '--remove', forwarded], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
            except (subprocess.SubprocessError, OSError):
                pass
    try:
        while time.monotonic() - started < args.hours * 3600:
            tick = time.monotonic()
            record = {'at': datetime.datetime.now(datetime.timezone.utc).isoformat()}
            try:
                if forwarded is None:
                    port = subprocess.check_output(adb + ['forward', 'tcp:0', 'tcp:80'], stderr=subprocess.DEVNULL, timeout=10).decode().strip()
                    if not port.isdigit():
                        raise RuntimeError('forward unavailable')
                    forwarded = 'tcp:' + port
                address = 'http://127.0.0.1:' + forwarded.split(':')[1]
                with urllib.request.urlopen(address + '/api/v1/system/summary', timeout=15) as response:
                    raw = response.read(1024 * 1024 + 1)
                    if response.status != 200 or len(raw) > 1024 * 1024:
                        raise RuntimeError('summary unavailable')
                    value = json.loads(raw)
                record.update(summarize(value))
            except Exception as error:
                record.update(healthy=False, code=type(error).__name__)
                if forwarded:
                    remove_forward()
                    forwarded = None
            summary = health.observe(time.monotonic(), record)
            summary.update(status='running', startedAt=datetime.datetime.fromtimestamp(time.time() - (time.monotonic()-started), datetime.timezone.utc).isoformat(), targetHours=args.hours, lastSample=record)
            with (out / 'samples.jsonl').open('a', encoding='utf8') as stream:
                stream.write(json.dumps(record) + '\n')
            temporary = out / 'summary.tmp'
            temporary.write_text(json.dumps(summary, indent=2) + '\n', encoding='utf8')
            temporary.replace(out / 'summary.json')
            time.sleep(min(max(0, args.interval - (time.monotonic() - tick)), max(0, args.hours * 3600 - (time.monotonic() - started))))
        completed = True
    except KeyboardInterrupt:
        pass
    finally:
        if forwarded:
            remove_forward()
        summary.update(health.report(time.monotonic()), status='completed' if completed else 'interrupted')
        (out / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n', encoding='utf8')
    print('Read-only soak monitor ended; inspect private summary for duration and failures')


if __name__ == '__main__':
    main()
