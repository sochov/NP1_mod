import importlib.util
import pathlib
import unittest
from unittest.mock import patch

ROOT = pathlib.Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('sta_trial', ROOT / 'tools/device/native-wifi-sta-trial.py')
trial = importlib.util.module_from_spec(spec)
spec.loader.exec_module(trial)

class NativeWiFiSTATrialTests(unittest.TestCase):
    def test_route_restore_is_one_fixed_wlan_default_and_rejects_injection(self):
        good = 'default via 10.1.0.1 dev wlan0 proto dhcp src 10.1.0.2 metric 10'
        self.assertEqual(trial.original_default(good, '10.1.0.2'), good.split())
        for bad in ('default via 10.1.0.1 dev wlan0;reboot', 'default via 999.1.0.1 dev wlan0',
                    good + '\n' + good, 'default via 10.1.0.1 dev wlan0 table 999',
                    'default via 10.1.0.1 dev wlan0 src 10.1.0.9', good + ' metric 2'):
            with self.subTest(bad=bad), self.assertRaises(RuntimeError):
                trial.original_default(bad, '10.1.0.2')

    def test_identity_includes_association_network_id_and_ip(self):
        before = {'ssid': 'fixture-only', 'id': '2', 'ip_address': '10.1.0.2', 'wpa_state': 'COMPLETED'}
        self.assertTrue(trial.same_wifi(before, before))
        for key in before:
            after = dict(before, **{key: 'changed'})
            self.assertFalse(trial.same_wifi(before, after))
        self.assertEqual(trial.address_prefix('4: wlan0 inet 10.1.0.2/24 scope global valid_lft 4sec'), '10.1.0.2/24')

    def cleanup_fixture(self, competing=False, late=False, foreign=False):
        before = b'wpa_state=COMPLETED\nssid=fixture-only\nid=2\nip_address=10.1.0.2\n'
        route = 'default via 10.1.0.1 dev wlan0 proto dhcp src 10.1.0.2'
        data = {'owner-before.txt': b'1234\n5678\n', 'stock-before.txt': b'stock', 'workers-before.txt': b'own',
                'wifi-before.txt': before, 'address-before.txt': b'4: wlan0 inet 10.1.0.2/24 scope global',
                'default-before.txt': route.encode(), 'ap-before.txt': b'ap', 'refs-before.txt': b'refs', 'preflight-before.txt': b'preflight'}
        calls = []; tick = [0]; route_added = [False]; native_calls = [0]
        def clock():
            tick[0] += 1
            return tick[0]
        def command(args, *unused, **kw):
            calls.append(args)
            if args[-1] == '--preflight':
                return 'preflight'
            if args[1:4] == ['-o', '-4', 'address']:
                return '4: wlan0 inet 10.1.0.2/24 scope global'
            if args[1:4] == ['-4', 'route', 'add']:
                route_added[0] = True
                return ''
            return route if route_added[0] else 'default dev usb0.1' if competing else ''
        def invoke(*args):
            calls.append(args)
            return '1'
        def state():
            native_calls[0] += 1
            return 1 if late and native_calls[0] == 2 else 0
        patches = [patch.object(trial, 'private', side_effect=lambda name, *unused: data[name]),
                   patch.object(trial, 'save', side_effect=lambda name, value: data.update({name: value if isinstance(value, bytes) else value.encode()})),
                   patch.object(trial, 'owner', return_value='9999\n' if foreign else '1234\n5678\n'),
                   patch.object(trial, 'stock', return_value='stock'), patch.object(trial, 'workers', return_value='own'),
                   patch.object(trial, 'ap_addresses', return_value='ap'), patch.object(trial, 'ap_http'),
                   patch.object(trial, 'snapshot_modem'),
                   patch.object(trial, 'memory_refs', return_value='refs'),
                   patch.object(trial, 'wpa', return_value=(before, trial.wpa_values(before))),
                   patch.object(trial, 'native40', side_effect=state), patch.object(trial, 'invoke', side_effect=invoke),
                   patch.object(trial, 'command', side_effect=command), patch.object(trial.time, 'monotonic', side_effect=clock),
                   patch.object(trial.time, 'sleep'), patch.object(trial.os.path, 'exists', side_effect=lambda path: path.endswith(('baseline-ready.txt', 'off-attempted.txt')))]
        for p in patches: p.start(); self.addCleanup(p.stop)
        return calls, route_added

    def test_cleanup_restores_only_missing_original_and_handles_late_off(self):
        calls, added = self.cleanup_fixture(late=True)
        trial.main('cleanup')
        self.assertTrue(added[0])
        native = [c for c in calls if isinstance(c, tuple)]
        self.assertEqual([c[1] for c in native], ['--restore-on', '--restore-on'])
        self.assertEqual([c for c in calls if isinstance(c, list) and c[1:4] == ['-4', 'route', 'add']],
                         [['/sbin/ip', '-4', 'route', 'add', 'default', 'via', '10.1.0.1', 'dev', 'wlan0', 'proto', 'dhcp', 'src', '10.1.0.2']])

    def test_cleanup_does_not_replace_competing_default(self):
        calls, added = self.cleanup_fixture(competing=True)
        with self.assertRaisesRegex(RuntimeError, 'STA_DEFAULT_RESTORE_UNVERIFIED_NO_REPLACE'):
            trial.main('cleanup')
        self.assertFalse(added[0])

    def test_foreign_owner_prevents_even_restore_request(self):
        calls, added = self.cleanup_fixture(foreign=True)
        with self.assertRaisesRegex(RuntimeError, 'STA_OWNER_CHANGED_NO_BLIND_RESTORE'):
            trial.main('cleanup')
        self.assertEqual(calls, [])
        self.assertFalse(added[0])

if __name__ == '__main__':
    unittest.main()
