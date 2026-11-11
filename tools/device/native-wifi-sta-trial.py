"""Bounded stock-owner STA OFF -> observation -> original ON restoration.

Device Python 3.5 compatible (no json module required). Only the fixed helper
may change STA state. No wpa DISCONNECT, setting40 write, CM launch or APN write.
Network identities, saved settings and logs remain in the root-only trial dir.
"""
import os
import re
import signal
import socket
import stat
import struct
import subprocess
import sys
import time

STOCK = ('app-@Communication', 'app-@DriveRec', 'app-@ImageRecog', 'app-@Voice', 'qmmf-server', 'NEventWatcherCom', 'app-@Setting')
OWN = ('np1-control', 'np1-control-broker', 'np1-control-ap', 'np1-control-modem', 'np1-control-native')
BASE = ''
child = None
seen_cm = set()
cm_proof_done = False
CM_GET_MODE = 'none'
OFF_DEADLINE = None

class OffObservationExpired(Exception):
    pass


def off_observation_expired(number, frame):
    # Only read-only observation children run inside this interval. Reap that
    # child before the shell's independent ON-recovery path starts.
    if child is not None and child.poll() is None:
        child.kill()
        child.wait(timeout=5)
    raise OffObservationExpired()

def fail(code):
    raise RuntimeError(code)

def save(name, data):
    if not re.fullmatch(r'[a-z0-9.-]+', name):
        fail('STA_PRIVATE_RECORD_NAME_INVALID')
    if not isinstance(data, bytes):
        data = data.encode('utf-8')
    with open(BASE + '/' + name, 'wb') as out:
        out.write(data)

def private(name, maximum=65536):
    path = BASE + '/' + name
    info = os.lstat(path)
    if info.st_uid != 0 or not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077 or info.st_size > maximum:
        fail('STA_PRIVATE_RECORD_INVALID')
    with open(path, 'rb') as source:
        return source.read(maximum + 1)

def command(args, timeout=4, allow_failure=False):
    global child
    child = subprocess.Popen(args, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    try:
        output = child.communicate(timeout=timeout)[0]
    except subprocess.TimeoutExpired:
        child.kill()
        child.communicate(timeout=5)
        fail('STA_FIXED_COMMAND_TIMEOUT')
    if child.returncode and not allow_failure:
        fail('STA_FIXED_COMMAND_FAILED')
    if len(output) > 65536:
        fail('STA_FIXED_COMMAND_OVERSIZE')
    return output.decode('utf-8').strip()

def interrupted(number, frame):
    if child is not None and child.poll() is None:
        child.kill()
        child.wait(timeout=5)
    raise SystemExit(128 + number)

def stock():
    data = []
    for unit in STOCK:
        row = command(['/bin/systemctl', 'show', unit + '.service', '-p', 'Id', '-p', 'MainPID', '-p', 'InvocationID', '-p', 'ActiveState', '-p', 'SubState'])
        if not stock_state_safe(unit, row):
            fail('STA_STOCK_NOT_READY')
        data.append(unit + '\n' + row)
    return '\n'.join(data) + '\n'

def stock_state_safe(unit, raw):
    values = {}
    for line in raw.strip().splitlines():
        pair = line.split('=', 1)
        if len(pair) != 2 or pair[0] in values:
            return False
        values[pair[0]] = pair[1]
    if len(values) != 5 or values.get('Id') != unit + '.service':
        return False
    pid = values.get('MainPID', '')
    invocation = values.get('InvocationID', '')
    if not re.fullmatch(r'0|[1-9][0-9]{0,9}', pid):
        return False
    if values.get('ActiveState') == 'active':
        return int(pid) > 1 and values.get('SubState') == 'running' and bool(re.fullmatch(r'[a-f0-9]{32}', invocation))
    if unit in ('app-@Communication', 'app-@Setting', 'NEventWatcherCom'):
        return False
    return values.get('ActiveState') == 'inactive' and values.get('SubState') == 'dead' and pid == '0' and (not invocation or bool(re.fullmatch(r'[a-f0-9]{32}', invocation)))

def workers():
    return '\n'.join(unit + '\n' + command(['/bin/systemctl', 'show', unit + '.service', '-p', 'MainPID', '-p', 'ActiveState']) for unit in OWN) + '\n'

def owner():
    pid = command(['/bin/systemctl', 'show', 'app-@Communication.service', '-p', 'MainPID']).split('=', 1)[-1]
    if not re.fullmatch('[1-9][0-9]*', pid):
        fail('STA_COMMUNICATION_PID_INVALID')
    with open('/proc/' + pid + '/stat') as source:
        raw = source.read(8192)
    fields = raw[raw.rfind(')') + 2:].split()
    if len(fields) < 20 or not fields[19].isdigit():
        fail('STA_COMMUNICATION_IDENTITY_INVALID')
    return pid + '\n' + fields[19] + '\n'

def wpa():
    with open('/etc/group') as source:
        gids = [int(line.split(':')[2]) for line in source if line.startswith('NEventWatcherCom:')]
    if len(gids) != 1:
        fail('STA_REPLY_GROUP_UNAVAILABLE')
    path = '/run/np1-control/wpa-sta-trial-' + str(os.getpid())
    if os.path.lexists(path):
        fail('STA_REPLY_SOCKET_EXISTS')
    channel = socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM)
    try:
        channel.bind(path)
        os.chown(path, 0, gids[0]); os.chmod(path, 0o660)
        channel.settimeout(2)
        channel.connect('/run/wifi/wpa_supplicant/wlan0')
        channel.send(b'STATUS')
        raw = channel.recv(32769)
        if len(raw) > 32768:
            fail('STA_STATUS_OVERSIZE')
    finally:
        channel.close()
        if os.path.lexists(path):
            os.unlink(path)
    values = {}
    for line in raw.decode('utf-8').splitlines():
        if '=' in line:
            key, value = line.split('=', 1)
            if key in values:
                fail('STA_STATUS_DUPLICATE')
            values[key] = value
    return raw, values

def wpa_values(raw):
    return dict(line.split('=', 1) for line in raw.decode('utf-8').splitlines() if '=' in line)

def same_wifi(before, after):
    return after.get('wpa_state') == 'COMPLETED' and all(before.get(key) is not None and before.get(key) == after.get(key) for key in ('ssid', 'id', 'ip_address'))

def ipv4(value):
    return bool(re.fullmatch(r'[0-9]{1,3}(\.[0-9]{1,3}){3}', value)) and all(int(v) <= 255 for v in value.split('.'))

def original_default(raw, address):
    rows = [line.split() for line in raw.splitlines() if line.strip()]
    if len(rows) != 1:
        fail('STA_ORIGINAL_DEFAULT_NOT_UNIQUE')
    row = rows[0]
    if len(row) < 5 or row[:2] != ['default', 'via'] or row[3:5] != ['dev', 'wlan0'] or not ipv4(row[2]):
        fail('STA_ORIGINAL_DEFAULT_UNSUPPORTED')
    seen = set()
    for at in range(5, len(row), 2):
        if at + 1 >= len(row) or row[at] in seen:
            fail('STA_ORIGINAL_ROUTE_OPTION_INVALID')
        key, value = row[at:at + 2]; seen.add(key)
        if key == 'proto' and value in ('boot', 'dhcp', 'static'):
            continue
        if key == 'src' and value == address and ipv4(value):
            continue
        if key == 'metric' and re.fullmatch('[0-9]{1,9}', value):
            continue
        fail('STA_ORIGINAL_ROUTE_OPTION_UNSUPPORTED')
    return row

def address_prefix(raw):
    rows = [line.split() for line in raw.splitlines() if line.strip()]
    if len(rows) != 1 or len(rows[0]) < 4 or rows[0][1].split('@')[0] != 'wlan0' or rows[0][2] != 'inet':
        fail('STA_IPV4_PROOF_INVALID')
    value = rows[0][3].split('/')
    if len(value) != 2 or not ipv4(value[0]) or not value[1].isdigit() or not 0 <= int(value[1]) <= 32:
        fail('STA_IPV4_PROOF_INVALID')
    return rows[0][3]

def ap_addresses():
    return command(['/sbin/ip', '-o', '-4', 'address', 'show', 'dev', 'nccap0'])

def ap_http():
    rows = ap_addresses().splitlines()
    if len(rows) != 1 or len(rows[0].split()) < 4:
        fail('STA_AP_IPV4_UNAVAILABLE')
    address = rows[0].split()[3].split('/')[0]
    if not ipv4(address):
        fail('STA_AP_IPV4_INVALID')
    command(['/usr/bin/wget', '-q', '-O', '/dev/null', '-T', '2', 'http://' + address + '/'], 3)

def invoke(name, *args):
    global child
    env = dict(os.environ, LD_LIBRARY_PATH='/usr/local/lib', NCC_NATIVE_WIFI_STA_TRIAL='1')
    with open(BASE + '/' + name + '.txt', 'wb') as out, open(BASE + '/' + name + '-error.txt', 'wb') as err:
        child = subprocess.Popen([BASE + '/helper'] + list(args), stdout=out, stderr=err, env=env)
        try:
            rc = child.wait(timeout=35)
        except subprocess.TimeoutExpired:
            child.kill(); child.wait(timeout=5)
            fail('STA_NATIVE_HELPER_TIMEOUT')
    if rc:
        fail('STA_NATIVE_HELPER_FAILED')
    values = private(name + '.txt', 4096).decode('utf-8')
    state = re.findall(r'^wifi-sta-wire=([12])$', values, re.M)
    native = re.findall(r'^native-40=([01])$', values, re.M)
    if len(state) != 1 or len(native) != 1:
        fail('STA_OWNER_STATE_UNVERIFIED')
    if (state[0], native[0]) not in (('1', '0'), ('2', '1')) or 'cleanup=matched; own-receiver-removed' not in values:
        fail('STA_OWNER_STATE_UNVERIFIED')
    return state[0]

def observe(index):
    prefix = 'off-%02d' % index
    save(prefix + '-routes.txt', command(['/sbin/ip', '-4', 'route', 'show', 'table', 'main', 'default']))
    save(prefix + '-addresses.txt', command(['/sbin/ip', '-o', '-4', 'address', 'show']))
    cm = command(['pidof', 'quectel-CM'], allow_failure=True)
    save(prefix + '-cm.txt', cm)
    snapshot_cm(cm)
    snapshot_cm_proof(cm)
    save(prefix + '-workers.txt', workers())
    if stock().encode('utf-8') != private('stock-before.txt') or ap_addresses().encode('utf-8') != private('ap-before.txt'):
        fail('STA_STOCK_OR_AP_CHANGED')
    ap_http()
    snapshot_modem(prefix + '-modem.json')

def snapshot_cm(raw_pids):
    # Capture provenance for a later opt-in GET review. Only proc metadata and
    # executable files are read; device FDs are neither opened nor duplicated.
    pids = raw_pids.split()
    if len(pids) > 4 or any(not re.fullmatch('[1-9][0-9]{0,9}', pid) for pid in pids):
        fail('STA_CM_INVENTORY_UNSUPPORTED')
    for pid in pids:
        if pid in seen_cm:
            continue
        seen_cm.add(pid)
        root = '/proc/' + pid + '/'
        stem = 'cm-' + pid + '-'
        try:
            for name, maximum in (('stat', 8192), ('status', 32768), ('cgroup', 16384), ('cmdline', 4096)):
                with open(root + name, 'rb') as source:
                    value = source.read(maximum + 1)
                if len(value) > maximum:
                    fail('STA_CM_METADATA_OVERSIZE')
                save(stem + name + '.private', value)
            save(stem + 'exe.private', os.readlink(root + 'exe'))
            save(stem + 'netns.private', os.readlink(root + 'ns/net'))
            save(stem + 'digest.private', command(['sha256sum', root + 'exe']))
            count = 0
            for name in os.listdir(root + 'fd'):
                try:
                    count += os.readlink(root + 'fd/' + name) == '/dev/qcqmi0'
                except OSError:
                    pass
            save(stem + 'qmi-count.private', str(count))
            with open(root + 'stat', 'rb') as source:
                save(stem + 'stat-after.private', source.read(8192))
        except (OSError, RuntimeError):
            save(stem + 'incomplete.private', 'process-metadata-changed-or-unavailable\n')

def snapshot_cm_proof(raw_pids):
    global child, cm_proof_done
    if cm_proof_done or not raw_pids.strip():
        return
    # CM may have appeared before it opens QMI. Wait for a descriptor rather
    # than spending the only metadata proof on that transient initialization.
    # Readlinks are metadata only; no FD is opened or duplicated here.
    pids = raw_pids.split()
    if len(pids) == 1 and re.fullmatch('[1-9][0-9]{0,9}', pids[0]):
        try:
            directory = '/proc/' + pids[0] + '/fd'
            has_qmi = False
            for name in os.listdir(directory):
                try:
                    has_qmi = has_qmi or os.readlink(directory + '/' + name) == '/dev/qcqmi0'
                except OSError:
                    pass
            if not has_qmi:
                return
        except OSError:
            return
    cm_proof_done = True
    # This separate mode reads proc/sysfs and executable hashes only, and does
    # not relax the production QMI guard or open any modem FD.
    with open(BASE + '/cm-proof.json', 'wb') as out, open(BASE + '/cm-proof-error.private', 'wb') as err:
        child = subprocess.Popen([BASE + '/cm-proof', '--mode', 'stock-cm-ownership-readonly'], stdout=out, stderr=err)
        try:
            rc = child.wait(timeout=12)
        except subprocess.TimeoutExpired:
            child.kill(); child.wait(timeout=5)
            save('cm-proof-result.private', 'metadata-timeout\n')
            return
    save('cm-proof-result.private', 'metadata-exit-%d\n' % rc)
    if rc == 0 and CM_GET_MODE != 'none':
        # New fixed getters require their own live metadata proof again. No
        # receipt or existing JSON file authorizes a QMI open. Reserve 15s so
        # this query cannot deliberately extend the fixed OFF interval.
        if OFF_DEADLINE is None or OFF_DEADLINE - time.monotonic() < 15:
            save('cm-get-skipped.private', 'insufficient-off-budget\n')
            return
        modes = {'nas': 'stock-cm-nas-readonly', 'profiles': 'stock-cm-profiles-readonly'}
        if CM_GET_MODE not in modes:
            fail('STA_CM_GET_MODE_INVALID')
        with open(BASE + '/cm-get.json', 'wb') as out, open(BASE + '/cm-get-error.private', 'wb') as err:
            child = subprocess.Popen([BASE + '/cm-proof', '--mode', modes[CM_GET_MODE]], stdout=out, stderr=err)
            try:
                rc = child.wait(timeout=12)
            except subprocess.TimeoutExpired:
                child.kill(); child.wait(timeout=5)
                save('cm-get-result.private', 'fixed-get-timeout\n')
                return
        save('cm-get-result.private', 'fixed-get-exit-%d\n' % rc)

def snapshot_modem(name):
    path = '/run/np1-control-modem/state.json'
    info = os.lstat(path)
    if not stat.S_ISREG(info.st_mode) or info.st_size > 16384:
        fail('STA_MODEM_STATE_UNAVAILABLE')
    with open(path, 'rb') as source:
        raw = source.read(16385)
    if len(raw) > 16384:
        fail('STA_MODEM_STATE_OVERSIZE')
    save(name, raw)

def native40():
    path = '/tmp/victoria/victoria_PMEM_AL_SRV_SET_SETTING_ITEM.map'
    if os.path.islink(path):
        fail('STA_SHARED_STATE_INVALID')
    with open(path, 'rb') as source:
        raw = source.read(7297)
    at = lambda offset: struct.unpack_from('<I', raw, offset)[0]
    if len(raw) != 7296 or at(0) != 7168 or at(4) != 0 or at(128) != 1 or at(132) != 1 or at(140) != 0 or at(148) != 44 or at(152) != 86:
        fail('STA_SHARED_STATE_INVALID')
    value = at(128 + 44 + 4 * 40)
    if value not in (0, 1):
        fail('STA_SHARED_STATE_UNKNOWN')
    return value

def memory_refs():
    # NStandard mapping header offset112 is the verified process reference
    # count. Never read/store the private payload after its 128-byte header.
    rows = []
    root = '/tmp/victoria/'
    for name in sorted(os.listdir(root)):
        if not name.endswith('.map') or not re.fullmatch(r'[A-Za-z0-9._-]+', name):
            continue
        path = root + name
        info = os.lstat(path)
        if not stat.S_ISREG(info.st_mode) or info.st_size < 128:
            fail('STA_EXISTING_MAPPING_INVALID')
        with open(path, 'rb') as source:
            raw = source.read(128)
        if len(raw) != 128:
            fail('STA_EXISTING_MAPPING_INVALID')
        rows.append(name + '\t' + str(struct.unpack_from('<Q', raw, 112)[0]))
    return '\n'.join(rows) + '\n'

def observe_off(hold_seconds):
    global OFF_DEADLINE
    if hold_seconds not in (3, 30, 45):
        fail('STA_OFF_HOLD_INVALID')
    # A monotonic deadline bounds OFF independently of the number/duration of
    # observations. It never waits for a host's traffic completion signal.
    until = time.monotonic() + hold_seconds
    OFF_DEADLINE = until
    print('STA_OWNER_OFF_CONFIRMED hold=%d utc=%s' % (hold_seconds, time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())), flush=True)
    index = 0
    previous = signal.signal(signal.SIGALRM, off_observation_expired)
    signal.alarm(hold_seconds)
    try:
        while time.monotonic() < until:
            observe(index)
            index += 1
            remaining = until - time.monotonic()
            if remaining > 0:
                time.sleep(min(0.5, remaining))
    except OffObservationExpired:
        pass
    finally:
        signal.alarm(0)
        signal.signal(signal.SIGALRM, previous)
        OFF_DEADLINE = None


def main(mode, hold_seconds=3):
    if mode == 'cleanup':
        if not os.path.exists(BASE + '/baseline-ready.txt'):
            return
        if owner().encode('utf-8') != private('owner-before.txt') or stock().encode('utf-8') != private('stock-before.txt'):
            fail('STA_OWNER_CHANGED_NO_BLIND_RESTORE')
        if os.path.exists(BASE + '/off-attempted.txt'):
            pid = private('owner-before.txt').decode().splitlines()[0]
            if invoke('restore-on', '--restore-on', pid) != '1':
                fail('STA_OWNER_RESTORE_UNVERIFIED')
            before = wpa_values(private('wifi-before.txt'))
            until = time.monotonic() + 50
            stable_since = None
            late_restores = 0
            while True:
                try:
                    raw, after = wpa()
                    save('wifi-after.txt', raw)
                except (OSError, RuntimeError):
                    # ON completion and supplicant STATUS readiness need not
                    # be simultaneous. Keep the independent restoration bound.
                    after = {}
                if native40() == 1:
                    # An earlier timed-out OFF may finish after an initial ON
                    # read. Restore only through a fresh owner GET/CAS, twice at
                    # most; never resend OFF and never modify derived setting40.
                    if late_restores >= 2:
                        fail('STA_LATE_OFF_NOT_SETTLED')
                    invoke('late-restore-%d' % late_restores, '--restore-on', pid)
                    late_restores += 1
                    stable_since = None
                elif same_wifi(before, after):
                    if stable_since is None:
                        stable_since = time.monotonic()
                    if time.monotonic() - stable_since >= 15:
                        break
                else:
                    stable_since = None
                if stock().encode() != private('stock-before.txt') or ap_addresses().encode() != private('ap-before.txt'):
                    fail('STA_STOCK_OR_AP_CHANGED_DURING_RESTORE')
                if time.monotonic() >= until:
                    fail('STA_ASSOCIATION_RESTORE_UNVERIFIED')
                time.sleep(0.5)
            if address_prefix(command(['/sbin/ip', '-o', '-4', 'address', 'show', 'dev', 'wlan0'])) != address_prefix(private('address-before.txt').decode()):
                fail('STA_IPV4_CHANGED_NO_ROUTE_RESTORE')
            current = command(['/sbin/ip', '-4', 'route', 'show', 'table', 'main', 'default'])
            route = original_default(private('default-before.txt').decode(), before['ip_address'])
            if not current:
                # Re-check the exact stock owner, association and address before
                # adding the single missing original route. Never flush/replace.
                if owner().encode() != private('owner-before.txt') or stock().encode() != private('stock-before.txt') or not same_wifi(before, wpa()[1]):
                    fail('STA_ROUTE_RESTORE_PROOF_CHANGED')
                command(['/sbin/ip', '-4', 'route', 'add'] + route)
                save('route-restored.txt', 'missing-original-default-only\n')
            current = command(['/sbin/ip', '-4', 'route', 'show', 'table', 'main', 'default'])
            if [line.split() for line in current.splitlines()] != [route]:
                fail('STA_DEFAULT_RESTORE_UNVERIFIED_NO_REPLACE')
        save('stock-after.txt', stock()); save('workers-after.txt', workers()); save('ap-after.txt', ap_addresses())
        if private('stock-before.txt') != private('stock-after.txt') or private('workers-before.txt') != private('workers-after.txt') or private('ap-before.txt') != private('ap-after.txt'):
            fail('STA_STOCK_OR_WORKER_CHANGED')
        ap_http()
        snapshot_modem('modem-after.json')
        if os.path.exists(BASE + '/saved-before.dat'):
            with open('/pio_data/SYSTEM/SETTING/Setting.DAT', 'rb') as source:
                after = source.read(705)
            save('saved-after.dat', after)
            if private('saved-before.dat') != after:
                fail('STA_SAVED_SETTINGS_CHANGED')
        save('refs-after.txt', memory_refs())
        save('preflight-after.txt', command([BASE + '/helper', '--preflight'], 10))
        if private('refs-before.txt') != private('refs-after.txt') or private('preflight-before.txt') != private('preflight-after.txt'):
            fail('STA_VENDOR_REFERENCES_OR_SEMAPHORES_CHANGED')
        print('STA owner cleanup verified; stock, workers, original profile and AP preserved')
        return
    save('stock-before.txt', stock()); save('workers-before.txt', workers()); save('owner-before.txt', owner())
    save('ap-before.txt', ap_addresses()); ap_http()
    snapshot_modem('modem-before.json')
    save('preflight-before.txt', command([BASE + '/helper', '--preflight'], 10))
    save('refs-before.txt', memory_refs())
    save('baseline-ready.txt', 'ready\n')
    # Preflight does not call a vendor constructor; its output schema is distinct.
    if mode == 'preflight':
        print('STA preflight passed; no owner request sent')
        return
    pid = private('owner-before.txt').decode().splitlines()[0]
    state = invoke('get', '--get', pid)
    if mode == 'get':
        print('STA owner GET verified; wire=' + state)
        return
    if state != '1':
        fail('STA_TRIAL_REQUIRES_ORIGINAL_ON')
    raw, wifi = wpa()
    if wifi.get('wpa_state') != 'COMPLETED' or not ipv4(wifi.get('ip_address', '')):
        fail('STA_ORIGINAL_ASSOCIATION_REQUIRED')
    save('wifi-before.txt', raw)
    address = command(['/sbin/ip', '-o', '-4', 'address', 'show', 'dev', 'wlan0'])
    if address_prefix(address).split('/')[0] != wifi['ip_address']:
        fail('STA_ORIGINAL_IPV4_PROOF_MISMATCH')
    save('address-before.txt', address)
    defaults = command(['/sbin/ip', '-4', 'route', 'show', 'table', 'main', 'default'])
    original_default(defaults, wifi['ip_address']); save('default-before.txt', defaults)
    with open('/pio_data/SYSTEM/SETTING/Setting.DAT', 'rb') as source:
        saved = source.read(705)
    if len(saved) != 704:
        fail('STA_SAVED_SETTINGS_LAYOUT_UNSUPPORTED')
    save('saved-before.dat', saved)
    save('off-attempted.txt', 'one-owner-request-only\n')
    if invoke('off', '--apply', pid, '1', '2') != '2':
        fail('STA_OWNER_OFF_UNVERIFIED')
    save('off-confirmed.txt', 'correlated-owner-response-and-state40\n')
    print('STA owner OFF verified; observing briefly before ON restoration', flush=True)
    observe_off(hold_seconds)

if __name__ == '__main__':
    if len(sys.argv) not in (3, 4, 5) or sys.argv[1] not in ('preflight', 'get', 'roundtrip', 'cleanup'):
        raise SystemExit('Fixed STA trial mode and private runtime required')
    hold = 3
    if len(sys.argv) >= 4:
        if sys.argv[1] != 'roundtrip' or sys.argv[3] not in ('3', '30', '45'):
            raise SystemExit('Fixed bounded OFF duration required')
        hold = int(sys.argv[3])
    if len(sys.argv) == 5:
        if sys.argv[4] not in ('none', 'nas', 'profiles'):
            raise SystemExit('Fixed CM GET mode required')
        CM_GET_MODE = sys.argv[4]
    BASE = sys.argv[2]
    if os.geteuid() != 0 or not re.fullmatch(r'/run/np1-native-wifi-sta\.[A-Za-z0-9]+', BASE):
        raise SystemExit('Root private STA runtime required')
    info = os.lstat(BASE)
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o077:
        raise SystemExit('Private STA runtime ownership invalid')
    os.umask(0o077)
    if sys.argv[1] != 'cleanup':
        for number in (signal.SIGHUP, signal.SIGINT, signal.SIGTERM):
            signal.signal(number, interrupted)
    try:
        main(sys.argv[1], hold)
    except Exception as e:
        # No raw command output, SSID, address or private exception text escapes.
        code = str(e)
        print(code if re.fullmatch(r'STA_[A-Z0-9_]+', code) else 'STA_TRIAL_UNVERIFIED', file=sys.stderr)
        raise SystemExit(1)
