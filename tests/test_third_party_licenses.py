"""Offline checks for complete dependency notices and unchanged upstream text."""
import contextlib
import importlib.util
import io
import json
import pathlib
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('third_party', ROOT / 'tools/third-party-licenses.py')
M = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(M)


class ThirdPartyLicenses(unittest.TestCase):
    def setUp(self):
        (ROOT / 'tmp').mkdir(exist_ok=True)
        self.directory = tempfile.TemporaryDirectory(dir=ROOT / 'tmp', prefix='license-fixture-')
        self.addCleanup(self.directory.cleanup)
        self.root = pathlib.Path(self.directory.name)
        (self.root / 'backend').mkdir()
        (self.root / 'frontend').mkdir()
        (self.root / 'backend/go.mod').write_text('module example.invalid/app\nrequire example.invalid/module v1.0.0\n')
        (self.root / 'backend/go.sum').write_text('')
        (self.root / 'frontend/package.json').write_text('{}')
        self.lock = {'packages': {'': {}, 'node_modules/example': {'version': '1.0.0', 'license': 'MIT'}}}
        self.write_lock()
        notices = M.store_notices(self.root, 'npm', 'example', '1.0.0', [('LICENSE', b'MIT fixture license\n')])
        self.inventory = {'schemaVersion': 1, 'inputHashes': M.input_hashes(self.root), 'packages': [
            {'ecosystem': 'npm', 'name': 'example', 'version': '1.0.0', 'license': 'MIT', 'notices': notices},
            {'ecosystem': 'go', 'name': 'example.invalid/module', 'version': 'v1.0.0', 'license': 'MIT', 'notices': notices},
        ]}
        self.write_inventory()

    def write_lock(self):
        (self.root / 'frontend/package-lock.json').write_text(json.dumps(self.lock))

    def write_inventory(self):
        (self.root / 'licenses/inventory.json').write_text(json.dumps(self.inventory))

    def test_complete_inventory_and_original_text(self):
        with contextlib.redirect_stdout(io.StringIO()):
            M.verify(self.root)
        notice = self.inventory['packages'][0]['notices'][0]['path']
        (self.root / notice).write_text('changed')
        with self.assertRaisesRegex(ValueError, 'Notice text changed'):
            M.verify(self.root)

    def test_lock_update_requires_new_notices(self):
        self.lock['packages']['node_modules/new'] = {'version': '2.0.0', 'license': 'MIT'}
        self.write_lock()
        with self.assertRaisesRegex(ValueError, 'Dependency inputs changed'):
            M.verify(self.root)
        self.inventory['inputHashes'] = M.input_hashes(self.root)
        self.write_inventory()
        with self.assertRaisesRegex(ValueError, 'npm inventory differs'):
            M.verify(self.root)

    def test_single_line_go_require_is_checked(self):
        self.inventory['packages'][1]['version'] = 'v0.9.0'
        self.write_inventory()
        with self.assertRaisesRegex(ValueError, 'Go inventory differs'):
            M.verify(self.root)

    def test_missing_unknown_or_escaping_notices_are_rejected(self):
        package = self.inventory['packages'][0]
        package['license'] = 'UNLICENSED'
        self.write_inventory()
        with self.assertRaisesRegex(ValueError, 'Unknown license'):
            M.verify(self.root)
        package['license'] = 'MIT'
        package['notices'][0]['path'] = '../outside'
        self.write_inventory()
        with self.assertRaisesRegex(ValueError, 'escapes'):
            M.verify(self.root)
        with self.assertRaisesRegex(ValueError, 'Missing license text'):
            M.store_notices(self.root, 'npm', 'unknown', '1', [])


if __name__ == '__main__':
    unittest.main()
