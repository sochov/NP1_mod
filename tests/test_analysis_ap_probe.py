"""Pure fixtures for the remote USB controller; no SSH or Wi-Fi operations."""
import importlib.util
import datetime
import json
import pathlib
import socket
import stat
import struct
import types
import unittest
from unittest.mock import patch

ROOT = pathlib.Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('remote_ap', ROOT / 'tools/remote/usb-wifi-ap-probe.py')
M = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(M)


def profile():
    return dict(interface='spare0', phy='phy5', usbPath='/sys/devices/fixture/usb1/1-3/1-3.2', driver='rtw88_fixture', protectedInterfaces=['uplink0', 'labap0'])


def dhcp_fixture(kind=5):
    data = bytearray(240)
    data[:3], data[4:8], data[16:20], data[28:34], data[236:240] = b'\x02\x01\x06', b'ABCD', socket.inet_aton('192.168.226.20'), b'123456', b'\x63\x82\x53\x63'
    options = {53: bytes([kind]), 54: socket.inet_aton(M.AP), 1: socket.inet_aton('255.255.255.0'), 3: socket.inet_aton(M.AP), 6: socket.inet_aton(M.AP), 51: struct.pack('!I', 3600)}
    for key, value in options.items():
        data += bytes([key, len(value)]) + value
    return bytes(data + b'\xff')


def offer_frame(payload, checksum=False):
    mac = b'123456'
    udp = struct.pack('!HHHH', 67, 68, len(payload) + 8, 0) + payload
    source, target = socket.inet_aton(M.AP), b'\xff' * 4
    if checksum:
        value = M.internet_checksum(source + target + b'\0\x11' + struct.pack('!H', len(udp)) + udp)
        udp = udp[:6] + struct.pack('!H', value or 65535) + udp[8:]
    ip = struct.pack('!BBHHHBBH4s4s', 0x45, 0, 20 + len(udp), 0, 0, 64, 17, 0, source, target)
    ip = ip[:10] + struct.pack('!H', M.internet_checksum(ip)) + ip[12:]
    return mac + b'\x02\0\0\0\0\1\x08\x00' + ip + udp


class AnalysisAPParsers(unittest.TestCase):
    def test_private_profile_rejects_protected_and_unpinned_targets(self):
        M.validate_profile(profile())
        for key, value in [('interface', 'uplink0'), ('phy', 'phy5;true'), ('usbPath', '/tmp/fake'), ('driver', 'other'), ('protectedInterfaces', ['uplink0'])]:
            with self.subTest(key=key), self.assertRaises(M.ProbeError):
                M.validate_profile(dict(profile(), **{key: value}))

    def test_dhcp_requires_correlated_server_address_and_options(self):
        self.assertEqual(M.parse_dhcp(dhcp_fixture(), b'ABCD', b'123456', 5), '192.168.226.20')
        for mode in ('xid', 'mac', 'server', 'gateway', 'dns', 'address', 'truncated', 'duplicate'):
            with self.subTest(mode=mode):
                data = bytearray(dhcp_fixture())
                if mode == 'xid':
                    data[4] ^= 1
                elif mode == 'mac':
                    data[28] ^= 1
                elif mode in ('server', 'gateway', 'dns'):
                    option = {'server': 54, 'gateway': 3, 'dns': 6}[mode]
                    offset = data.index(bytes([option, 4]), 240)
                    data[offset + 5] ^= 1
                elif mode == 'address':
                    data[16:20] = socket.inet_aton('10.0.0.2')
                elif mode == 'truncated':
                    data = data[:-2]
                else:
                    data = data[:-1] + b'\x35\x01\x05\xff'
                with self.assertRaises(M.ProbeError):
                    M.parse_dhcp(bytes(data), b'ABCD', b'123456', 5)

    def test_dhcp_raw_frame_checksums_and_direction(self):
        payload = dhcp_fixture(2)
        sent = M.dhcp_frame(payload, b'123456')
        self.assertEqual(sent[:6], b'\xff' * 6)
        self.assertEqual(sent[6:12], b'123456')
        self.assertEqual(M.internet_checksum(sent[14:34]), 0)
        self.assertEqual(struct.unpack('!HH', sent[34:38]), (68, 67))
        self.assertIsNone(M.dhcp_from_frame(sent, b'123456'))
        for checksum in (False, True):
            frame = offer_frame(payload, checksum)
            self.assertEqual(M.dhcp_from_frame(frame, b'123456'), payload)
            self.assertIsNone(M.dhcp_from_frame(frame, b'654321'))
            invalid = bytearray(frame)
            invalid[24] ^= 1
            self.assertIsNone(M.dhcp_from_frame(invalid, b'123456'))
            if checksum:
                invalid = bytearray(frame)
                invalid[-1] ^= 1
                self.assertIsNone(M.dhcp_from_frame(invalid, b'123456'))

    def test_dhcp_exchange_uses_packet_socket_before_ipv4_is_assigned(self):
        sent = []
        class FakeSocket:
            def __enter__(self): return self
            def __exit__(self, *args): pass
            def bind(self, address): self.bound = address
            def settimeout(self, seconds): pass
            def send(self, value): sent.append(value)
            def recv(self, size):
                kind = M.dhcp_options(sent[-1][42:])[53][0]
                return offer_frame(dhcp_fixture(2 if kind == 1 else 5), checksum=True)
        with patch.object(M.socket, 'AF_PACKET', 17, create=True), patch.object(M.socket, 'socket', return_value=FakeSocket()) as create, patch.object(M.os, 'urandom', return_value=b'ABCD'):
            self.assertEqual(M.dhcp_request('spare0', b'123456'), '192.168.226.20')
            self.assertEqual(create.call_args.args[:2], (17, socket.SOCK_RAW))
        self.assertEqual(len(sent), 2)

    def test_dhcp_request_ignores_duplicate_offer_and_handles_nak_before_address(self):
        sent, replies = [], []
        class FakeSocket:
            def __enter__(self): return self
            def __exit__(self, *args): pass
            def bind(self, address): pass
            def settimeout(self, seconds): pass
            def send(self, value): sent.append(value)
            def recv(self, size):
                kind = M.dhcp_options(sent[-1][42:])[53][0]
                if kind == 1:
                    return offer_frame(dhcp_fixture(2))
                if not replies:
                    replies.append('duplicate')
                    return offer_frame(dhcp_fixture(2))
                return offer_frame(dhcp_fixture(5))
        with patch.object(M.socket, 'AF_PACKET', 17, create=True), patch.object(M.socket, 'socket', return_value=FakeSocket()), patch.object(M.os, 'urandom', return_value=b'ABCD'):
            stats = {}
            self.assertEqual(M.dhcp_request('spare0', b'123456', stats), '192.168.226.20')
        self.assertEqual(len(sent), 2)
        self.assertEqual(stats['ignoredFrames'], 1)
        self.assertEqual(stats['ackReceived'], 1)
        nak = bytearray(dhcp_fixture(6))
        nak[16:20] = bytes(4)
        with self.assertRaisesRegex(M.ProbeError, 'DHCP_NAK'):
            M.parse_dhcp(nak, b'ABCD', b'123456', 5)

    def test_dns_rejects_bad_id_empty_and_nonpublic_answers(self):
        query = b'AB\x81\x80\x00\x01\x00\x01\x00\x00\x00\x00\x07example\x03com\x00\x00\x01\x00\x01'
        answer = query + b'\xc0\x0c\x00\x01\x00\x01\x00\x00\x00\x1e\x00\x04' + socket.inet_aton('93.184.216.34')
        self.assertEqual(M.dns_answer(answer, b'AB'), '93.184.216.34')
        for value in ('10.1.2.3', '100.64.1.1', '127.0.0.1', '169.254.1.1', '192.168.226.1', '192.0.2.1', '224.0.0.1'):
            with self.subTest(value=value), self.assertRaises(M.ProbeError):
                M.dns_answer(answer[:-4] + socket.inet_aton(value), b'AB')
        for bad in (b'ZZ' + answer[2:], query[:6] + b'\0\0' + query[8:], answer[:-2]):
            with self.assertRaises(M.ProbeError):
                M.dns_answer(bad, b'AB')

    def test_snapshot_ignores_lifetimes_but_detects_route_and_dns_change(self):
        first = {'links': [{'ifname': 'uplink0', 'valid_life_time': 100, 'preferred_life_time': 80}], 'routes': [{'gateway': '192.0.2.1', 'expires': 5}], 'resolver': 'hash-one'}
        later = {'links': [{'ifname': 'uplink0', 'valid_life_time': 99, 'preferred_life_time': 79}], 'routes': [{'gateway': '192.0.2.1', 'expires': 4}], 'resolver': 'hash-one'}
        self.assertEqual(M.normalized(first), M.normalized(later))
        later['resolver'] = 'hash-two'
        self.assertNotEqual(M.normalized(first), M.normalized(later))
        later['resolver'] = 'hash-one'
        later['routes'][0]['gateway'] = '192.0.2.2'
        self.assertNotEqual(M.normalized(first), M.normalized(later))

    def test_start_without_explicit_authorization_does_nothing(self):
        probe = M.RemoteProbe.__new__(M.RemoteProbe)
        probe.payload = {}
        with self.assertRaisesRegex(M.ProbeError, 'NOT_AUTHORIZED'):
            probe.start()

    def test_real_start_lock_validation_reaches_preflight(self):
        probe = M.RemoteProbe.__new__(M.RemoteProbe)
        probe.payload = dict(allowSpareUSBWiFiChanges=True, ap=dict(ssid='fixture-ssid', password='fixture-password'))
        probe.preflight = lambda: (_ for _ in ()).throw(M.ProbeError('PREFLIGHT_REACHED'))
        lock = types.SimpleNamespace(fileno=lambda: 17)
        metadata = types.SimpleNamespace(st_uid=0, st_mode=stat.S_IFREG | 0o600, st_nlink=1, st_size=0)
        fake_fcntl = types.SimpleNamespace(LOCK_EX=2, LOCK_NB=4, flock=lambda *args: None)
        with patch.object(M, 'fcntl', fake_fcntl), patch.object(M.os, 'O_NOFOLLOW', 0, create=True), patch.object(M.os, 'O_CLOEXEC', 0, create=True), patch.object(M.os, 'open', return_value=17), patch.object(M.os, 'fdopen', return_value=lock), patch.object(M.os, 'fstat', return_value=metadata):
            with self.assertRaisesRegex(M.ProbeError, 'PREFLIGHT_REACHED'):
                probe.start()
        self.assertEqual(probe.stage, 'preflight')

    def test_diagnostic_omits_exception_message_and_sensitive_locals(self):
        try:
            raise ValueError('fixture-ssid fixture-password')
        except ValueError as error:
            diagnostic = M.safe_diagnostic(types.SimpleNamespace(stage='own-lock'), error)
        self.assertEqual(diagnostic['stage'], 'own-lock')
        self.assertEqual(diagnostic['exceptionType'], 'ValueError')
        self.assertTrue(diagnostic['frames'])
        self.assertNotIn('fixture-ssid', json.dumps(diagnostic))
        self.assertNotIn('fixture-password', json.dumps(diagnostic))


class AnalysisAPCleanup(unittest.TestCase):
    def make_probe(self, fail_flush=False):
        probe = M.RemoteProbe.__new__(M.RemoteProbe)
        actions = []
        probe.iface, probe.phy, probe.ns, probe.hostpid, probe.hostns = 'spare0', 'phy5', 'ncc-usb-fixture', 42, 1
        probe.runtime = probe.lock = None
        probe.testns = probe.wpa_pidfd = None
        probe.moved = probe.created = probe.unmanaged = probe.autoconnect_changed = True
        probe.original_up, probe.original_autoconnect = True, 'yes'
        probe.original_mac, probe.original_mtu, probe.original_target_routes = '02:00:00:00:00:01', 1500, []
        probe.target_routes = lambda: []
        probe.before = {'unchanged': True}
        probe.leave = lambda: actions.append(['leave'])
        probe.identity = lambda: actions.append(['identity'])
        probe.nm = lambda field: {'NM-MANAGED': 'yes', 'STATE': '30 (disconnected)', 'AUTOCONNECT': 'yes'}[field]
        probe.host_snapshot = lambda: probe.before
        probe.wpa = None
        probe.stop_own_wpa = lambda: actions.append(['stop-own-wpa-pidfd'])
        probe.named_namespace_owned = lambda: True
        probe.namespace_target_owned = lambda: None
        def command(args, namespace=False):
            actions.append(args + (['pinned-testns'] if namespace else []))
            if fail_flush and 'flush' in args:
                raise M.ProbeError('FIXTURE_FAILURE')
            if args[:3] == ['ip', '-j', 'addr']:
                return json.dumps([{'flags': ['UP'], 'addr_info': [], 'address': probe.original_mac, 'mtu': 1500}])
            if args[:3] == ['iw', 'dev', 'spare0']:
                return 'Not connected.\n\ttype managed'
            return ''
        probe.command = command
        return probe, actions

    def test_restore_moves_phy_before_namespace_delete_and_restores_nm(self):
        probe, actions = self.make_probe()
        with patch.object(M.os, 'close'):
            self.assertTrue(probe.cleanup())
        move = ['iw', 'phy#5', 'set', 'netns', '42', 'pinned-testns']
        delete = ['ip', 'netns', 'delete', probe.ns]
        self.assertLess(actions.index(move), actions.index(delete))
        self.assertLess(actions.index(['stop-own-wpa-pidfd']), actions.index(move))
        self.assertIn(['nmcli', 'device', 'set', 'spare0', 'managed', 'yes'], actions)
        self.assertIn(['nmcli', 'device', 'set', 'spare0', 'autoconnect', 'yes'], actions)
        self.assertFalse(any('uplink0' in command or 'labap0' in command for command in actions))

    def test_restore_failure_continues_other_steps_and_never_passes(self):
        probe, actions = self.make_probe(fail_flush=True)
        with patch.object(M.os, 'close'):
            self.assertFalse(probe.cleanup())
        self.assertIn(['nmcli', 'device', 'set', 'spare0', 'managed', 'yes'], actions)
        self.assertIn(['identity'], actions)

    def test_foreign_namespace_name_is_preserved_while_pinned_phy_restored(self):
        probe, actions = self.make_probe()
        probe.named_namespace_owned = lambda: False
        with patch.object(M.os, 'close'):
            self.assertFalse(probe.cleanup())
        self.assertIn(['iw', 'phy#5', 'set', 'netns', '42', 'pinned-testns'], actions)
        self.assertNotIn(['ip', 'netns', 'delete', probe.ns], actions)
        self.assertFalse(any('exec' in action for action in actions))

    def test_foreign_managed_connection_is_not_overwritten(self):
        probe, actions = self.make_probe()
        probe.nm = lambda field: '100 (connected)'
        with patch.object(M.os, 'close'):
            self.assertFalse(probe.cleanup())
        self.assertFalse(any(action[0] == 'nmcli' for action in actions))
        self.assertNotIn(['ip', 'link', 'set', 'spare0', 'up'], actions)

    def test_reused_pid_never_receives_a_signal(self):
        probe = M.RemoteProbe.__new__(M.RemoteProbe)
        probe.wpa = types.SimpleNamespace(poll=lambda: None)
        probe.wpa_pidfd = 17
        with patch.object(M.os, 'P_PIDFD', 3, create=True), patch.object(M.os, 'WNOWAIT', 0x1000000, create=True), patch.object(M.os, 'WEXITED', 4, create=True), patch.object(M.os, 'WNOHANG', 1, create=True), patch.object(M.os, 'waitid', side_effect=ChildProcessError, create=True), patch.object(M.signal, 'pidfd_send_signal', create=True) as send:
            with self.assertRaisesRegex(M.ProbeError, 'CHILD_IDENTITY_CHANGED'):
                probe.stop_own_wpa()
            send.assert_not_called()


class AnalysisAPCoordination(unittest.TestCase):
    def run_fake(self, measure_fails=False, local_only=False):
        actions, events = [], []
        fake = types.SimpleNamespace(start=lambda: actions.append('start'), emit=lambda phase, **values: events.append(dict(phase=phase, **values)), cleanup=lambda: actions.append('cleanup') or True)
        def measure(kind):
            actions.append('measure')
            if measure_fails:
                raise M.ProbeError('FIXTURE_TLS_FAILURE')
            return dict(passed=True, upstream=kind)
        fake.measure = measure
        gate = dict(proceed=True, localOnly=True) if local_only else dict(proceed=True, expectedUpstream='cellular', expiresAt=(datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(seconds=40)).isoformat())
        gates = [gate, dict(cleanup=True, restored=True)]
        signals = types.SimpleNamespace(SIGTERM=15, SIGINT=2, SIGHUP=1, SIGALRM=14, SIG_IGN=1, signal=lambda *args: None, alarm=lambda seconds: None)
        with patch.object(M, 'RemoteProbe', return_value=fake), patch.object(M, 'signal', signals), patch.object(M.os, 'umask'), patch.object(M, 'read_gate', side_effect=gates) as read:
            M.main({})
            self.assertEqual(read.call_count, 2)
        self.assertEqual(actions[-1], 'cleanup')
        return actions, events

    def test_complete_requires_cleanup_gate(self):
        actions, events = self.run_fake()
        self.assertEqual(actions, ['start', 'measure', 'cleanup'])
        self.assertEqual([e['phase'] for e in events], ['prepared', 'traffic', 'complete'])
        self.assertTrue(events[-1]['passed'])

    def test_measure_failure_still_waits_for_upstream_restore(self):
        _, events = self.run_fake(measure_fails=True)
        self.assertEqual([e['phase'] for e in events], ['prepared', 'failure', 'complete'])
        self.assertFalse(events[-1]['passed'])
        self.assertTrue(events[-1]['restored'])

    def test_local_only_never_claims_or_measures_internet(self):
        actions, events = self.run_fake(local_only=True)
        self.assertNotIn('measure', actions)
        self.assertEqual(events[1]['scope'], 'ap-lan-only')
        self.assertNotIn('standardTLSHTTPS', events[1])


if __name__ == '__main__':
    unittest.main()
