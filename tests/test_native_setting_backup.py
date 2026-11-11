import importlib.util
import pathlib
import struct
import unittest

SOURCE = pathlib.Path(__file__).resolve().parents[1] / 'tools/native/setting_backup.py'
SPEC = importlib.util.spec_from_file_location('setting_backup', SOURCE)
BACKUP = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BACKUP)


def fixture(detection=0, sensitivity=1):
    values = {506: detection, 507: sensitivity}
    return struct.pack('<4I', 0, 16, 1, 86) + b''.join(
        struct.pack('<Ii', key, values.get(key, 0)) for key in sorted(BACKUP.PERSISTENT_IDS)
    )


class NativeSettingBackupTests(unittest.TestCase):
    def test_proven_keys_not_ordinal_offsets(self):
        self.assertEqual(BACKUP.parse_backup(fixture(1, 2)), {50: 1, 51: 2})

    def test_rejects_partial_unknown_header_order_and_duplicates(self):
        valid = fixture()
        for invalid in (valid[:-1], valid+b'x', b'wrong!!!'+valid[8:], valid[:12]+struct.pack('<I',85)+valid[16:], valid[:16]+valid[24:32]+valid[16:24]+valid[32:], valid[:16]+valid[24:32]+valid[24:]):
            with self.subTest(size=len(invalid)):
                with self.assertRaises(ValueError):
                    BACKUP.parse_backup(invalid)

    def test_unknown_enum_is_not_displayed(self):
        for detection, sensitivity in ((2, 1), (0, -1), (0, 3)):
            with self.assertRaises(ValueError):
                BACKUP.parse_backup(fixture(detection, sensitivity))

    def test_alexa_is_persistent_key_544_not_native_ordinal_85(self):
        data = bytearray(fixture())
        offset = 16 + sorted(BACKUP.PERSISTENT_IDS).index(544) * 8 + 4
        struct.pack_into('<i', data, offset, 1)
        self.assertEqual(BACKUP.parse_alexa_backup(data), {85: 1})
        struct.pack_into('<i', data, offset, 2)
        with self.assertRaises(ValueError):
            BACKUP.parse_alexa_backup(data)


if __name__ == '__main__':
    unittest.main()
