"""Run on an idle NP1 as root. Only this sensor gets an isolated input view.

The test toggles two symlinks under /run. Stock camera files, the physical
encoder, recordings, and other processes' mount namespaces stay untouched.
"""
import contextlib
import glob
import sys
import http.client
import ast
import os
import socket
import signal
import subprocess
import tempfile
import time

UNIT = 'np1-control-plate.service'
DROPINS = '/run/systemd/system/' + UNIT + '.d'
OVERRIDE = DROPINS + '/96-ncc-verify-front-input.conf'
PRIMARY = '/tmp/application/imagerecog/imagedata/frontcamera/imagedata'
BROKER = '/run/np1-control'
RECORDER = '/mnt/sdcard/DRIVERECDATA'


def status():
    wire = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    wire.settimeout(2)
    try:
        wire.connect('/run/np1-control-plate/sensor.sock')
        wire.sendall(b'GET /status HTTP/1.0\r\nHost: localhost\r\n\r\n')
        response = http.client.HTTPResponse(wire)
        response.begin()
        # NP1's minimal Python omits json. Sensor status contains only strings and numbers.
        return ast.literal_eval(response.read().decode('utf-8'))
    finally:
        wire.close()


def expect(source, seconds, instance=None, timeout=60):
    deadline = time.monotonic() + timeout
    first = None
    last = None
    speeds = []
    while time.monotonic() < deadline:
        try:
            sample = status()
        except (OSError, ValueError):
            time.sleep(0.5)
            continue
        if instance and sample['instance'] != instance:
            raise RuntimeError('sensor restarted during failover')
        stamp = sample.get('lastFrameAt')
        if sample.get('state') == 'running' and sample.get('inputSource') == source and stamp:
            if stamp != last:
                if first is None:
                    first = time.monotonic()
                speeds.append(round(sample.get('fps', 0), 2))
                last = stamp
                if len(speeds) >= 3 and time.monotonic() - first >= seconds:
                    print(source, sample['instance'], stamp, speeds, flush=True)
                    return sample['instance']
        else:
            first, last, speeds = None, None, []
        time.sleep(1)
    raise RuntimeError('no sustained new frames on ' + source + ': ' + str(status()))


def terminate_decode_child():
    # Limit the fault to the NP1CC plate unit and to its software decoder.
    with open('/sys/fs/cgroup/memory/system.slice/' + UNIT + '/cgroup.procs') as f:
        pids = [int(line) for line in f]
    matches = []
    for pid in pids:
        try:
            if os.path.samefile('/proc/{}/exe'.format(pid), '/usr/bin/gst-launch-1.0'):
                matches.append(pid)
        except FileNotFoundError:
            pass
    if len(matches) != 1:
        raise RuntimeError('expected one active plate decoder: ' + str(matches))
    os.kill(matches[0], signal.SIGTERM)


def wait_until_all_inputs_stop(instance):
    deadline = time.monotonic() + 25
    while time.monotonic() < deadline:
        sample = status()
        if sample['instance'] != instance:
            raise RuntimeError('unexpected restart')
        if sample['state'] == 'camera-waiting' and not sample.get('fps', 0):
            stamp = sample.get('lastFrameAt')
            time.sleep(5)
            other = status()
            if other['state'] == 'camera-waiting' and other.get('lastFrameAt') == stamp:
                print('all-inputs-waiting', stamp, flush=True)
                return
        time.sleep(1)
    raise RuntimeError('last frame kept advancing without input')


def expect_recording_rotation(instance):
    pattern = RECORDER + '/Video/FILEM_*.MP4'
    first = max(glob.glob(pattern))
    deadline = time.monotonic() + 210
    while time.monotonic() < deadline:
        if max(glob.glob(pattern)) != first:
            time.sleep(4)
            expect('front-recording', 8, instance)
            print('front recording rotation verified', flush=True)
            return
        time.sleep(1)
    raise RuntimeError('no new front recording opened')


def switch(link, target):
    os.symlink(target, link + '.next')
    os.replace(link + '.next', link)


def service(*args):
    subprocess.check_call(['systemctl'] + list(args))


@contextlib.contextmanager
def original_view(source, target):
    subprocess.check_call(['mount', '--bind', source, target])
    try:
        subprocess.check_call(['mount', '-o', 'remount,bind,ro', target])
        yield
    finally:
        subprocess.check_call(['umount', target])


@contextlib.contextmanager
def verification_workspace():
    work = tempfile.mkdtemp(prefix='np1-front-test-', dir='/run')
    try:
        yield work
    finally:
        # Never recurse through a mountpoint, even when umount fails.
        for name in ['camera-view/value', 'broker-view/plate-front.sock', 'sd-view/Video', 'frozen']:
            path = os.path.join(work, name)
            if os.path.lexists(path):
                os.unlink(path)
        for name in ['camera-view', 'broker-view', 'sd-view', 'primary', 'broker', 'video']:
            path = os.path.join(work, name)
            if os.path.exists(path):
                os.rmdir(path)
        os.rmdir(work)


def main():
    if not os.path.exists(PRIMARY + '/value') or not os.path.exists(BROKER + '/plate-front.sock'):
        raise RuntimeError('live deployment required')
    # A single drop-in is removed in finally. systemd-run keeps cleanup alive
    # if the ADB client disconnects. Runtime-only paths disappear on reboot.
    with verification_workspace() as work:
        for name in ['primary', 'broker', 'camera-view', 'broker-view', 'video', 'sd-view']:
            os.mkdir(work + '/' + name)
        video_link = work + '/sd-view/Video'
        switch(video_link, work + '/video')
        primary_link = work + '/camera-view/value'
        broker_link = work + '/broker-view/plate-front.sock'
        switch(primary_link, work + '/primary/value')
        switch(broker_link, work + '/broker/plate-front.sock')
        frozen = work + '/frozen'
        with open(frozen, 'wb') as f:
            f.truncate(1920 * 1080 * 3 // 2 + 16)
        os.utime(frozen, (1, 1))
        os.makedirs(DROPINS, exist_ok=True)
        with original_view(PRIMARY, work + '/primary'), original_view(BROKER, work + '/broker'), original_view(RECORDER + '/Video', work + '/video'):
            with open(OVERRIDE, 'x') as f:
                f.write('[Service]\nBindReadOnlyPaths={w}/camera-view:{p} {w}/broker-view:{b} {w}/sd-view:{d}\n'.format(p=PRIMARY,b=BROKER,w=work,d=RECORDER))
            try:
                service('daemon-reload')
                service('restart', UNIT)
                instance = expect('front-export', 4)
                switch(primary_link, frozen)
                expect('front-encoder', 12, instance)
                terminate_decode_child()
                expect('front-recording', 10, instance)
                switch(primary_link, work + '/primary/value')
                expect('front-export', 4, instance)
                switch(broker_link, work + '/unavailable')
                if '--extended' in sys.argv:
                    switch(video_link, work + '/missing-card')
                switch(primary_link, work + '/missing-export')
                if '--extended' in sys.argv:
                    wait_until_all_inputs_stop(instance)
                    switch(video_link, work + '/video')
                expect('front-recording', 12, instance)
                if '--extended' in sys.argv:
                    expect_recording_rotation(instance)
                switch(primary_link, work + '/primary/value')
                expect('front-export', 4, instance)
            finally:
                os.unlink(OVERRIDE)
                service('daemon-reload')
                service('restart', UNIT)
            expect('front-export', 4)


if __name__ == '__main__':
    main()
