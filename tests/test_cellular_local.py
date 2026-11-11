import importlib.util
import pathlib
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('cellular_local', ROOT / 'tools/verify-cellular-local.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class CellularLocalTests(unittest.TestCase):
    def test_ap_only_management_requires_positive_http_witness(self):
        value = dict(ready=True, managementWiFiCompleted=False, managementHotspotVerified=True,
                     managementPath='hotspot', restorableDefaultCaptured=False)
        self.assertTrue(module.validate_network(value))
        for key, wrong in [('managementHotspotVerified', False), ('managementWiFiCompleted', True),
                           ('managementPath', 'wifi-sta'), ('restorableDefaultCaptured', True)]:
            with self.subTest(key=key):
                self.assertFalse(module.validate_network(dict(value, **{key: wrong})))

    def test_bound_tls_requires_every_result_and_origin(self):
        value = dict(status='passed', source='np1-origin-bound-cellular', dnsVerified=True,
                     tlsVerified=True, httpVerified=True, identityUnchanged=True)
        self.assertTrue(module.validate_internet(value))
        for key in ('dnsVerified', 'tlsVerified', 'httpVerified', 'identityUnchanged'):
            self.assertFalse(module.validate_internet(dict(value, **{key: False})))
        self.assertFalse(module.validate_internet(dict(value, source='wifi')))

    def test_device_tty_line_endings_and_missing_result(self):
        self.assertEqual(module.read_json_line(b'noise\r\n{"ready":true}\r\n'), {'ready': True})
        with self.assertRaises(ValueError):
            module.read_json_line(b'no result')
