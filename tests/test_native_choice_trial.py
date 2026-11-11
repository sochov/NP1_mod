"""Synthetic private-evidence fixtures; these are not device field proof."""
import importlib.util
import pathlib
import struct
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('choice_trial', ROOT / 'tools/stage-native-choice.py')
TRIAL = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(TRIAL)


def fixture(directory, index, persistent):
    before = bytearray(704)
    struct.pack_into('<4I', before, 0, 0, 16, 0, 86)
    for ordinal in range(86):
        struct.pack_into('<Ii', before, 16 + ordinal * 8, 460 + ordinal, 0)
    applied = bytearray(before)
    struct.pack_into('<i', applied, 20 + (persistent - 460) * 8, 5)
    for phase, data, value in (('before', before, 0), ('applied', applied, 5), ('after', before, 0)):
        (directory / ('saved-' + phase + '.dat')).write_bytes(data)
        (directory / (phase + '-values.txt')).write_text(f'{index:02d}={value}\nsem0=1\n', encoding='ascii')
    stock = ''.join(f'Unit=synthetic-{i}\nMainPID={100 + i}\nActiveState=active\n' for i in range(10))
    for phase in ('before', 'after'):
        (directory / ('stock-' + phase + '.txt')).write_text(stock, encoding='ascii')
    for phase in ('apply', 'restore'):
        (directory / (phase + '.txt')).write_text('cleanup-owned-mutexes=0; cleanup-owned-mappings=0; refs=matched', encoding='ascii')


class NativeChoiceTrialTests(unittest.TestCase):
    def test_all_three_keys_accept_synthetic_maximum_five_roundtrip(self):
        for name, index, key in (('route-search', 68, 526), ('daily-voice', 77, 536), ('support-voice', 78, 537)):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as temp:
                directory = pathlib.Path(temp)
                fixture(directory, index, key)
                TRIAL.verify_readback(directory, name, '5')

    def test_unrelated_applied_setting_change_is_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = pathlib.Path(temp)
            fixture(directory, 77, 536)
            file = directory / 'saved-applied.dat'
            data = bytearray(file.read_bytes())
            struct.pack_into('<i', data, 20, 1)
            file.write_bytes(data)
            with self.assertRaisesRegex(ValueError, 'unrelated persistent'):
                TRIAL.verify_readback(directory, 'daily-voice', '5')

    def test_missing_cleanup_proof_is_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = pathlib.Path(temp)
            fixture(directory, 68, 526)
            (directory / 'restore.txt').write_text('', encoding='ascii')
            with self.assertRaisesRegex(ValueError, 'cleanup'):
                TRIAL.verify_readback(directory, 'route-search', '5')

    def test_stock_restart_is_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = pathlib.Path(temp)
            fixture(directory, 78, 537)
            file = directory / 'stock-after.txt'
            file.write_text(file.read_text().replace('MainPID=100', 'MainPID=999'), encoding='ascii')
            with self.assertRaisesRegex(ValueError, 'PID/state changed'):
                TRIAL.verify_readback(directory, 'support-voice', '5')


if __name__ == '__main__':
    unittest.main()
