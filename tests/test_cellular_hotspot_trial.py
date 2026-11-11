import datetime as dt
import importlib.util
import pathlib
import unittest

path = pathlib.Path(__file__).resolve().parents[1] / 'tools/verify-cellular-hotspot.py'
spec = importlib.util.spec_from_file_location('cellular_hotspot_trial', path)
trial = importlib.util.module_from_spec(spec)
spec.loader.exec_module(trial)


class FakeIO:
    hold_seconds = 45

    def __init__(self, mode='valid'):
        self.mode, self.t = mode, 0
        self.epoch = dt.datetime(2026, 1, 1, tzinfo=dt.timezone.utc)
        self.phases = {}
        self.sta_started = False
        self.events = []

    def now(self): return self.epoch + dt.timedelta(seconds=self.t)
    def sleep(self, value): self.t += value
    def save(self, *args): pass
    def start_pc(self): self.events.append('pc-start')
    def pc_finished(self): return self.mode == 'pc-failed'
    def start_sta(self):
        if not self.phase('prepared'): raise AssertionError('writer before PC ready')
        self.events.append('sta-start'); self.sta_started = True
    def sta_finished(self): return False
    def phase(self, name):
        if name in self.phases: return self.phases[name]
        if name == 'prepared' and self.mode != 'pc-failed':
            return {key: True for key in ('prepared', 'association', 'dhcpGateway', 'dhcpDNS', 'httpUI', 'clientObserved')}
        if name == 'off' and self.sta_started and self.t >= 1:
            return {'offConfirmed': True, 'offAt': (self.epoch + dt.timedelta(seconds=1)).isoformat(), 'restoreDeadline': (self.epoch + dt.timedelta(seconds=46)).isoformat(), 'holdSeconds': 45}
        if name == 'traffic' and self.phases.get('proceed', {}).get('proceed') and self.t >= (49 if self.mode == 'late' else 8):
            value = {key: True for key in ('passed', 'dnsUDP', 'dnsTCP', 'standardTLSHTTPS', 'interfaceBound')}
            value.update(upstream='cellular', trafficStartedAt=(self.epoch + dt.timedelta(seconds=4)).isoformat(), observedAt=(self.epoch + dt.timedelta(seconds=49 if self.mode == 'late' else 8)).isoformat())
            if self.mode == 'bad-proof': value['standardTLSHTTPS'] = False
            return value
        return None
    def write_phase(self, name, value):
        self.events.append(name); self.phases[name] = value
    def ap(self):
        cellular = self.t >= 3 and self.mode != 'no-cellular' and not (self.mode == 'fallback' and self.t >= 6)
        return {'status': 'available', 'data': {'enabled': True, 'dhcpReady': True, 'sharing': {'ready': True, 'upstream': {'kind': 'cellular' if cellular else 'wifi', 'interface': 'usb0.1' if cellular else 'wlan0'}}}}
    def nat(self, name):
        count = 3 if name == 'before' or self.mode == 'no-nat' else 9
        return f'{count} 500 MASQUERADE all -- * usb0.1 192.168.226.0/24 0.0.0.0/0\n'
    def finish_sta(self):
        self.events.append('sta-restored'); self.t = max(self.t, 62)
        return {'restored': self.mode != 'restore-failed', 'returnCode': 0}
    def finish_pc(self):
        if 'cleanup' not in self.phases: raise AssertionError('PC cleanup before restoration gate')
        self.events.append('pc-cleaned')
        return {'returnCode': 0, 'profileCleaned': True}
    def check_preserved(self):
        return {'ownServicesUnchanged': True, 'APCredentialsAndSettingsUnchanged': True, 'savedSTAPSKUnchanged': self.mode != 'psk-changed', 'originalSTAAndStockPIDsRestored': True}


class CellularHotspotTrialTests(unittest.TestCase):
    def test_prepared_then_off_then_traffic_then_restored_cleanup(self):
        io = FakeIO(); result = trial.coordinate(io)
        self.assertTrue(result['passed']); self.assertEqual(result['cellularNATPacketDelta'], 6)
        self.assertLess(io.events.index('pc-start'), io.events.index('sta-start'))
        self.assertLess(io.events.index('sta-restored'), io.events.index('cleanup'))
        self.assertLess(io.events.index('cleanup'), io.events.index('pc-cleaned'))

    def test_failed_pc_never_starts_writer(self):
        io = FakeIO('pc-failed'); result = trial.coordinate(io)
        self.assertFalse(result['passed']); self.assertFalse(io.sta_started)

    def test_no_cellular_never_opens_traffic_gate(self):
        io = FakeIO('no-cellular'); result = trial.coordinate(io)
        self.assertFalse(result['passed']); self.assertFalse(io.phases['proceed']['proceed']); self.assertTrue(result['restored'])

    def test_fallback_timeout_invalid_proof_or_nat_never_pass(self):
        for mode in ('fallback', 'late', 'bad-proof', 'no-nat', 'restore-failed', 'psk-changed'):
            with self.subTest(mode=mode):
                result = trial.coordinate(FakeIO(mode)); self.assertFalse(result['passed'])

    def test_nat_requires_exact_selected_cellular_egress_and_subnet(self):
        self.assertEqual(trial.nat_packets('8 99 MASQUERADE all -- * usb0.1 192.168.226.0/24 0.0.0.0/0', 'usb0.1'), 8)
        for raw in ('8 99 MASQUERADE all -- * wlan0 192.168.226.0/24 0.0.0.0/0', '8 99 MASQUERADE all -- * usb0.1 10.0.0.0/24 0.0.0.0/0'):
            with self.assertRaises(ValueError): trial.nat_packets(raw, 'usb0.1')


if __name__ == '__main__': unittest.main()
