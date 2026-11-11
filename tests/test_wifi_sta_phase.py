import datetime
import importlib.util
import json
import pathlib
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('sta_phase', ROOT / 'tools/wifi_sta_phase.py')
phase = importlib.util.module_from_spec(spec)
spec.loader.exec_module(phase)


class STAPhaseTests(unittest.TestCase):
    def test_only_correlated_fixed_off_marker_publishes_private_gate(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            off, complete = root / 'tmp/off.json', root / 'tmp/complete.json'
            gates = phase.STAPhaseFiles(root, 45, off, complete)
            gates.accept(b'STA owner OFF verified without a protocol marker\n')
            self.assertFalse(off.exists())
            now = datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0)
            marker = ('STA_OWNER_OFF_CONFIRMED hold=45 utc=' + now.strftime('%Y-%m-%dT%H:%M:%SZ') + '\n').encode()
            gates.accept(marker)
            value = json.loads(off.read_text())
            self.assertTrue(value['offConfirmed'])
            self.assertEqual(datetime.datetime.fromisoformat(value['restoreDeadline']) - datetime.datetime.fromisoformat(value['offAt']), datetime.timedelta(seconds=45))
            gates.finish(True)
            self.assertTrue(json.loads(complete.read_text())['restored'])
            with self.assertRaisesRegex(ValueError, 'STA_PHASE_OFF_CONFIRMATION_INVALID'):
                gates.accept(marker)
            self.assertEqual(list((root / 'tmp').glob('*.pending')), [])

    def test_wrong_duration_stale_timestamp_and_existing_gate_never_pass(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            off = root / 'tmp/off.json'
            gates = phase.STAPhaseFiles(root, 45, off)
            with self.assertRaisesRegex(ValueError, 'STA_PHASE_OFF_CONFIRMATION_INVALID'):
                gates.accept(b'STA_OWNER_OFF_CONFIRMED hold=30 utc=2020-01-01T00:00:00Z\n')
            with self.assertRaisesRegex(ValueError, 'STA_PHASE_OFF_TIMESTAMP_UNVERIFIED'):
                gates.accept(b'STA_OWNER_OFF_CONFIRMED hold=45 utc=2020-01-01T00:00:00Z\n')
            off.write_text('foreign')
            with self.assertRaisesRegex(ValueError, 'STA_PHASE_FRESH_PRIVATE_TARGET_REQUIRED'):
                phase.STAPhaseFiles(root, 45, off)
            with self.assertRaises(ValueError):
                phase.STAPhaseFiles(root, 45, root / 'public.json')

    def test_failed_cleanup_is_not_reported_as_restored(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            done = root / 'tmp/complete.json'
            gates = phase.STAPhaseFiles(root, 45, complete=done)
            gates.finish(False)
            self.assertFalse(json.loads(done.read_text())['restored'])


if __name__ == '__main__':
    unittest.main()
