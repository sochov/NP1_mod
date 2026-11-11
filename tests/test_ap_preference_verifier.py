import copy
import importlib.util
import json
import io
import pathlib
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('ap_preference_verifier', ROOT / 'tools/verify-ap-preference.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

class FakeAP:
    def __init__(self):
        self.config = {'revision': 4, 'enabled': True, 'ssid': 'synthetic-lan', 'passphrase': 'synthetic-secret', 'standaloneFallback': True, 'preference': 'cellular'}
        self.ownership = {'ifIndex': 12, 'alias': 'synthetic', 'hostapd': {'pid': 100, 'start': '1000'}, 'dhcp': {'pid': 101, 'start': '1001'}}
        self.requests = []
        self.records = {}
        self.lost_reply = False
        self.foreign_revision = False
        self.client = True
        self.reject_and_rollback = False
    def call(self, path, method='GET', data=None):
        if method == 'PUT':
            self.requests.append(copy.deepcopy(data))
            if data['expectedRevision'] != self.config['revision']:
                raise RuntimeError('AP_PREFERENCE_TEST_CAS_CONFLICT')
            if self.reject_and_rollback:
                raise module.urllib.error.HTTPError('http://synthetic.invalid', 409, 'Conflict', {}, io.BytesIO(b'{"error":"AP_PREFERENCE_NOT_CONFIRMED_PREVIOUS_CONFIG_RESTORED"}'))
            self.config['revision'] += 1
            self.config['preference'] = data['settings']['preference']
            if self.foreign_revision and len(self.requests) == 1:
                self.config['revision'] += 1
            if self.lost_reply and len(self.requests) == 1:
                raise OSError('synthetic private transport detail')
        return {'data': {'settings': {'revision': self.config['revision'], 'preference': self.config['preference'], 'ssid': self.config['ssid']},
                         'enabled': True, 'dhcpReady': True, 'clientsAvailable': True,
                         'clients': [{'mac': '02:00:00:00:00:01', 'ipv4': '192.0.2.20', 'authorized': True}] if self.client else [],
                         'sharing': {'preference': self.config['preference'], 'ready': True, 'upstream': {'kind': 'wifi'}}}}
    def read(self, command):
        if command[0] == 'systemctl':
            return '\n'.join('Id=synthetic\nMainPID=%d\nActiveState=active\nSubState=running' % n for n in (10, 11, 12))
        if command[1].endswith('/ap/config.json'):
            return json.dumps(self.config)
        if command[1].endswith('ownership.json'):
            return json.dumps(self.ownership)
        return '0 02:00:00:00:00:01 192.0.2.20 synthetic-host *\n'
    def verifier(self):
        return module.PreferenceVerification(self.call, self.read, lambda name, value: self.records.update({name: copy.deepcopy(value)}))

class APPreferenceVerifierTests(unittest.TestCase):
    def test_pending_peer_dhcp_does_not_invent_address_or_hide_proven_client(self):
        ap = {'clientsAvailable': True, 'clients': [
            {'mac': '02:00:00:00:00:01', 'ipv4': '192.0.2.20', 'authorized': True},
            {'mac': '02:00:00:00:00:02', 'authorized': True}]}
        self.assertEqual(module.client_addresses(ap), {'02:00:00:00:00:01': '192.0.2.20'})
        self.assertEqual(module.client_associations(ap), {'02:00:00:00:00:01', '02:00:00:00:00:02'})
        ap['clients'] = ap['clients'][1:]
        with self.assertRaisesRegex(RuntimeError, 'AP_PREFERENCE_ASSOCIATED_DHCP_CLIENT_REQUIRED'):
            module.client_addresses(ap)

    def test_three_web_partial_cas_puts_restore_original_and_preserve_secret(self):
        fake = FakeAP(); before = copy.deepcopy(fake.config)
        result = fake.verifier().run()
        self.assertTrue(result['passed'])
        self.assertTrue(result['originalRestored'])
        self.assertEqual(result['webPUTs'], 3)
        self.assertEqual([v['expectedRevision'] for v in fake.requests], [4, 5, 6])
        self.assertEqual([v['settings'] for v in fake.requests], [{'preference': 'wifi'}, {'preference': 'cellular'}, {'preference': 'cellular'}])
        self.assertEqual(module.radio_tuple(fake.config), module.radio_tuple(before))
        self.assertEqual(fake.config['revision'], 7)
        self.assertNotIn('synthetic-secret', json.dumps(result))

    def test_lost_success_reply_still_restores_only_own_revision(self):
        fake = FakeAP(); fake.lost_reply = True; verifier = fake.verifier()
        with self.assertRaises(OSError):
            verifier.run()
        self.assertEqual(fake.config['preference'], 'cellular')
        self.assertTrue(verifier.result['originalRestored'])
        self.assertEqual([v['expectedRevision'] for v in fake.requests], [4, 5])

    def test_foreign_revision_never_gets_blind_original_write(self):
        fake = FakeAP(); fake.foreign_revision = True
        with self.assertRaisesRegex(RuntimeError, 'AP_PREFERENCE_FOREIGN_CHANGE_NO_BLIND_RESTORE'):
            fake.verifier().run()
        self.assertEqual(len(fake.requests), 1)

    def test_rejected_put_already_restored_exact_baseline_needs_no_second_put(self):
        fake = FakeAP(); fake.reject_and_rollback = True; before = copy.deepcopy(fake.config)
        verifier = fake.verifier()
        with self.assertRaises(module.urllib.error.HTTPError):
            verifier.run()
        self.assertTrue(verifier.result['originalRestored'])
        self.assertEqual(verifier.result['webPUTs'], 1)
        self.assertFalse(verifier.result['passed'])
        self.assertEqual(fake.config, before)

    def test_http_failure_diagnostics_are_private_bounded_and_preserve_error(self):
        records = {}
        error = module.urllib.error.HTTPError('http://synthetic.invalid', 409, 'Conflict', {}, io.BytesIO(b'{"status":"error","code":"AP_PREFERENCE_NOT_CONFIRMED_PREVIOUS_CONFIG_RESTORED","reason":"synthetic private reason"}'))
        class Opener:
            def open(self, request, timeout):
                raise error
        with self.assertRaises(module.urllib.error.HTTPError) as raised:
            module.call_http_json(Opener(), object(), lambda name, value: records.update({name: value}))
        self.assertIs(raised.exception, error)
        diagnostic = next(iter(records.values()))
        self.assertEqual(diagnostic['status'], 409)
        self.assertEqual(diagnostic['errorCode'], 'AP_PREFERENCE_NOT_CONFIRMED_PREVIOUS_CONFIG_RESTORED')
        self.assertEqual(diagnostic['responseReason'], 'synthetic private reason')
        self.assertFalse(diagnostic['truncated'])
        error = module.urllib.error.HTTPError('http://synthetic.invalid', 500, 'x' * 1000, {}, io.BytesIO(b'x' * 100000))
        with self.assertRaises(module.urllib.error.HTTPError):
            module.call_http_json(Opener(), object(), lambda name, value: records.update({name: value}))
        diagnostic = list(records.values())[-1]
        self.assertTrue(diagnostic['truncated'])
        self.assertEqual(len(diagnostic['body']), 8192)
        self.assertEqual(len(diagnostic['reason']), 256)

    def test_missing_dhcp_client_is_not_a_vacuous_continuity_pass(self):
        fake = FakeAP(); fake.client = False
        with self.assertRaisesRegex(RuntimeError, 'AP_PREFERENCE_ASSOCIATED_DHCP_CLIENT_REQUIRED'):
            fake.verifier().run()
        self.assertEqual(fake.requests, [])

    def test_lease_duplicate_and_missing_proof_are_rejected(self):
        with self.assertRaisesRegex(RuntimeError, 'AP_PREFERENCE_LEASE_DUPLICATE'):
            module.lease_addresses('0 02:00:00:00:00:01 192.0.2.1\n0 02:00:00:00:00:01 192.0.2.2')
        with self.assertRaisesRegex(RuntimeError, 'AP_PREFERENCE_CLIENTS_UNKNOWN'):
            module.client_addresses({'clients': []})

if __name__ == '__main__':
    unittest.main()
