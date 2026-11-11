"""Synthetic read-back fixtures only, not evidence of device apply success."""
import importlib.util
import contextlib
import io
import pathlib
import struct
import subprocess
import sys
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('notification_trial', ROOT / 'tools/stage-native-notification.py')
TRIAL = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(TRIAL)


def fixture(directory, index, persistent, value=2):
    before = bytearray(704)
    struct.pack_into('<4I', before, 0, 0, 16, 0, 86)
    for ordinal in range(86):
        struct.pack_into('<Ii', before, 16 + ordinal * 8, 460 + ordinal, 0)
    applied = bytearray(before)
    struct.pack_into('<i', applied, 20 + (persistent - 460) * 8, value)
    for phase, data, actual in (('before', before, 0), ('applied', applied, value), ('after', before, 0)):
        (directory / ('saved-' + phase + '.dat')).write_bytes(data)
        (directory / (phase + '-values.txt')).write_text(f'{index:02d}={actual}\nsem0=1\n', encoding='ascii')
    stock = ''.join(f'Unit=synthetic-{i}\nMainPID={100+i}\nActiveState=active\n' for i in range(10))
    for phase in ('before', 'after'):
        (directory / ('stock-' + phase + '.txt')).write_text(stock, encoding='ascii')
    for phase in ('apply', 'restore'):
        (directory / (phase + '.txt')).write_text('cleanup-owned-mutexes=0; cleanup-owned-mappings=0; refs=matched', encoding='ascii')


class NativeNotificationTrialTests(unittest.TestCase):
    def test_cli_help_and_preflight_parse_before_any_device_transport(self):
        result = subprocess.run([sys.executable, str(ROOT / 'tools/stage-native-notification.py'), '--help'],
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.assertEqual(result.returncode, 0, result.stderr.decode(errors='replace'))
        self.assertIn(b'--action {preflight,roundtrip}', result.stdout)
        args = TRIAL.parse_arguments(['--serial', 'SYNTHETIC', '--action', 'preflight'])
        self.assertEqual(args.action, 'preflight')
        self.assertIsNone(args.id)
        self.assertIsNone(args.next)

    def test_cli_roundtrip_requires_fixed_id_and_bounded_value(self):
        for name in ('front-car-start', 'signal-change'):
            for value in ('0', '1', '2'):
                args = TRIAL.parse_arguments(['--serial', 'SYNTHETIC', '--action', 'roundtrip', '--id', name, '--next', value])
                self.assertEqual((args.id, args.next), (name, value))
        invalid = [[], ['--id', 'front-car-start'], ['--next', '1'],
                   ['--action', 'roundtrip'], ['--action', 'roundtrip', '--id', 'signal-change', '--next', '3'],
                   ['--action', 'roundtrip', '--id', 'unknown', '--next', '1']]
        for tail in invalid:
            # Empty default preflight is valid; explicit missing serial is not.
            argv = [] if not tail else ['--serial', 'SYNTHETIC'] + tail
            with self.subTest(argv=argv), contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
                TRIAL.parse_arguments(argv)

    def test_both_notifications_observe_memory_and_persistent_roundtrip(self):
        for name, index, key in (('front-car-start', 79, 538), ('signal-change', 80, 539)):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as tmp:
                directory = pathlib.Path(tmp)
                fixture(directory, index, key)
                TRIAL.verify_readback(directory, name, '2')

    def test_unrelated_persistent_mutation_is_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = pathlib.Path(tmp)
            fixture(directory, 79, 538)
            path = directory / 'saved-applied.dat'
            data = bytearray(path.read_bytes()); struct.pack_into('<i', data, 20, 1); path.write_bytes(data)
            with self.assertRaisesRegex(ValueError, 'unrelated persistent'):
                TRIAL.verify_readback(directory, 'front-car-start', '2')

    def test_unknown_enum_is_not_accepted_as_successful_roundtrip(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = pathlib.Path(tmp)
            fixture(directory, 79, 538, value=3)
            with self.assertRaisesRegex(ValueError, 'unsupported notification enum'):
                TRIAL.verify_readback(directory, 'front-car-start', '3')

    def test_missing_cleanup_or_consumer_restart_is_rejected(self):
        for condition in ('cleanup', 'restart'):
            with self.subTest(condition=condition), tempfile.TemporaryDirectory() as tmp:
                directory = pathlib.Path(tmp)
                fixture(directory, 80, 539)
                if condition == 'cleanup':
                    (directory / 'restore.txt').write_text('', encoding='ascii')
                else:
                    path = directory / 'stock-after.txt'
                    path.write_text(path.read_text().replace('MainPID=100', 'MainPID=999'), encoding='ascii')
                with self.assertRaises(ValueError):
                    TRIAL.verify_readback(directory, 'signal-change', '2')


if __name__ == '__main__':
    unittest.main()
