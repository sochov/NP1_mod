"""Synthetic owner/read-back evidence only; never invokes ADB or vendor code."""
import contextlib
import importlib.util
import io
import pathlib
import struct
import subprocess
import sys
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('audio_trial', ROOT / 'tools/stage-native-audio.py')
TRIAL = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(TRIAL)


def fixture(directory, index=1, key=466, next_value=1):
    data = bytearray(704)
    struct.pack_into('<4I', data, 0, 0, 16, 0, 86)
    for i in range(86):
        struct.pack_into('<Ii', data, 16+8*i, 460+i, 0)
    changed = bytearray(data)
    struct.pack_into('<i', changed, 20+8*(key-460), next_value)
    for phase, payload, value in (('before', data, 0), ('applied', changed, next_value), ('after', data, 0)):
        (directory / ('saved-'+phase+'.dat')).write_bytes(payload)
        text = f'{index:02d}={value}\nsem0=1\n'
        (directory / (phase+'-values.txt')).write_text(text, encoding='ascii')
    stock = 'synthetic-boot\n' + ''.join(f'Unit=synthetic-{i}\nMainPID={100+i}\nActiveState=active\n' for i in range(10))
    for phase in ('before', 'after'):
        (directory / ('stock-'+phase+'.txt')).write_text(stock, encoding='ascii')
    for phase in ('apply', 'restore'):
        (directory / (phase+'.txt')).write_text('cleanup=matched; own-receiver-removed', encoding='ascii')


class NativeAudioTrialTests(unittest.TestCase):
    def test_cli_help_get_and_preflight_do_not_require_write_values(self):
        r = subprocess.run([sys.executable, str(ROOT/'tools/stage-native-audio.py'), '--help'],
                           stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.assertEqual(r.returncode, 0)
        self.assertIn(b'{preflight,get,roundtrip}', r.stdout)
        for action in ('preflight', 'get'):
            args = TRIAL.parse_arguments(['--serial', 'SYNTHETIC', '--action', action])
            self.assertIsNone(args.id)
            self.assertIsNone(args.next)
        for id, maximum in (('mute', 1), ('volume-correction', 2)):
            for value in range(maximum+1):
                TRIAL.parse_arguments(['--serial', 'SYNTHETIC', '--action', 'roundtrip', '--id', id, '--next', str(value)])

    def test_cli_master_volume_unknown_enum_and_missing_fields_are_rejected(self):
        for tail in (['--action', 'roundtrip', '--id', 'master-volume', '--next', '1'],
                     ['--action', 'roundtrip', '--id', 'mute', '--next', '2'],
                     ['--action', 'roundtrip', '--id', 'volume-correction', '--next', '3'],
                     ['--action', 'roundtrip'], ['--action', 'get', '--next', '1']):
            with self.subTest(tail=tail), contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
                TRIAL.parse_arguments(['--serial', 'SYNTHETIC']+tail)

    def test_both_persistent_keys_and_complete_restoration(self):
        for id, index, key, value in (('mute', 1, 466, 1), ('volume-correction', 22, 468, 2)):
            with self.subTest(id=id), tempfile.TemporaryDirectory() as tmp:
                p = pathlib.Path(tmp); fixture(p, index, key, value)
                TRIAL.verify_readback(p, id, str(value))

    def test_owner_get_requires_coherent_values_and_own_queue_cleanup(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = pathlib.Path(tmp); fixture(p)
            baseline = 'native-volume=17\n01=0\n22=2\nsem0=1\n'
            for phase in ('before', 'after'):
                (p/(phase+'-values.txt')).write_text(baseline, encoding='ascii')
            output = 'audio-volume=17\naudio-mute=0\naudio-correction=2\ncleanup=matched; own-receiver-removed\n'
            (p/'get.txt').write_text(output, encoding='ascii')
            TRIAL.verify_readback(p)
            (p/'get.txt').write_text(output.replace('audio-mute=0', 'audio-mute=1'), encoding='ascii')
            with self.assertRaisesRegex(ValueError, 'disagree'):
                TRIAL.verify_readback(p)
            (p/'get.txt').write_text(output.replace('cleanup=matched; own-receiver-removed', ''), encoding='ascii')
            with self.assertRaisesRegex(ValueError, 'cleanup'):
                TRIAL.verify_readback(p)

    def test_unrelated_saved_change_and_unknown_mute_are_rejected(self):
        for invalid in ('unrelated', 'enum'):
            with self.subTest(invalid=invalid), tempfile.TemporaryDirectory() as tmp:
                p = pathlib.Path(tmp); fixture(p, next_value=2 if invalid=='enum' else 1)
                if invalid == 'unrelated':
                    path = p/'saved-applied.dat'; payload=bytearray(path.read_bytes()); struct.pack_into('<i', payload, 20, 1); path.write_bytes(payload)
                with self.assertRaises(ValueError):
                    TRIAL.verify_readback(p, 'mute', '2' if invalid=='enum' else '1')


if __name__ == '__main__':
    unittest.main()
