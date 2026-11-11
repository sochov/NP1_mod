import importlib.util
import pathlib
import struct
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]


def load_tool(name, relative):
    spec = importlib.util.spec_from_file_location(name, ROOT / relative)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


inventory = load_tool('stock_inventory', 'tools/inspect-stock-services.py')
recovery = load_tool('opening_recovery', 'tools/device/recover-opening-sync.py')


class StockDiagnosticTests(unittest.TestCase):
    def test_inventory_never_exports_raw_device_fields(self):
        text = ('=== UNIT app-@Opening.service ===\nId=app-@Opening.service\n'
                'MainPID=12345\nActiveState=active\nExecStart=private-command\n'
                'Description=private-description\nEnvironment=SECRET=value\n'
                '--- unit definition ---\nSSID=private-network\n'
                '=== UNIT socketserver.service ===\nMainPID=0\nActiveState=inactive\n')
        result = inventory.parse_public(text)
        self.assertEqual(len(result), 2)
        self.assertTrue(result[0]['hasMainPID'])
        self.assertFalse(result[1]['hasMainPID'])
        self.assertEqual(set(result[0]), {'unit', 'Id', 'ActiveState', 'hasMainPID'})
        self.assertNotIn('private', str(result))
        self.assertNotIn('12345', str(result))

    def test_stale_lock_header_accepts_only_proven_format(self):
        data = bytearray(128)
        for offset, value in ((16, 2), (20, 1), (24, 123456), (28, 1), (32, 129), (112, 1)):
            struct.pack_into('<I', data, offset, value)
        self.assertEqual(recovery.stale_owner(bytes(data)), 123456)
        for offset, value in ((0, 1), (16, 0), (20, 2), (24, 0), (28, 0), (32, 145), (112, 2)):
            bad = bytearray(data)
            struct.pack_into('<I', bad, offset, value)
            with self.subTest(offset=offset), self.assertRaises(RuntimeError):
                recovery.stale_owner(bytes(bad))
        for size in (0, 127, 129):
            with self.subTest(size=size), self.assertRaises(RuntimeError):
                recovery.stale_owner(bytes(size))


if __name__ == '__main__':
    unittest.main()
