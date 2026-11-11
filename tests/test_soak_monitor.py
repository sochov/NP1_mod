import importlib.util
import pathlib
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('soak_monitor', ROOT / 'tools/soak-monitor.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class SoakMonitorTests(unittest.TestCase):
    def test_qualification_requires_continuous_actual_duration(self):
        window = module.HealthWindow(0)
        good = {'healthy': True, 'boot': 'first'}
        for now in range(0, 86341, 60):
            window.observe(now, good)
        self.assertFalse(window.observe(86399, good)['qualified24Hours'])
        self.assertTrue(window.observe(86400, good)['qualified24Hours'])
        self.assertFalse(window.observe(86400, good)['qualified72Hours'])
        for now in range(86460, 259200, 60):
            window.observe(now, good)
        self.assertTrue(window.observe(259200, good)['qualified72Hours'])

    def test_error_or_reboot_resets_healthy_duration_without_hiding_history(self):
        window = module.HealthWindow(0)
        for now in range(0,4000,50):
            window.observe(now, {'healthy': True, 'boot': 'a'})
        value = window.observe(4000, {'healthy': False})
        self.assertEqual(value['continuousHealthySeconds'], 0)
        self.assertEqual(value['errors'], 1)
        for now in range(4010,6000,50):
            window.observe(now, {'healthy': True, 'boot': 'a'})
        value = window.observe(6000, {'healthy': True, 'boot': 'b'})
        self.assertEqual(value['continuousHealthySeconds'], 0)
        self.assertEqual(value['bootChanges'], 1)
        self.assertEqual(value['longestHealthySeconds'], 3950)

    def test_host_sleep_cannot_be_reported_as_monitored_time(self):
        window = module.HealthWindow(0)
        window.observe(0, {'healthy': True, 'boot': 'a'})
        value = window.observe(259200, {'healthy': True, 'boot': 'a'})
        self.assertFalse(value['qualified72Hours'])
        self.assertEqual(value['continuousHealthySeconds'], 0)

    def test_missing_core_state_is_not_healthy_and_private_fields_are_excluded(self):
        value = {'services': {'status': 'available', 'data': [{'id': 'qmmf', 'state': 'active'}]},
                 'modem': {'status': 'unknown', 'data': {'apn': 'private-value'}},
                 'tailscale': {'status': 'available', 'data': {'online': True, 'accountLogin': 'private-value'}}}
        result = module.summarize(value)
        self.assertFalse(result['healthy'])
        self.assertEqual(result['statuses']['modem'], 'unknown')
        self.assertNotIn('private-value', str(result))


if __name__ == '__main__':
    unittest.main()
