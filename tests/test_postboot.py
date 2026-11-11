import importlib.util
import pathlib
import unittest
spec = importlib.util.spec_from_file_location('postboot', pathlib.Path(__file__).resolve().parents[1] / 'tools/device/postboot.py')
postboot = importlib.util.module_from_spec(spec)
spec.loader.exec_module(postboot)
class PostbootTests(unittest.TestCase):
    def test_idempotent_preserves_other_owner_hooks(self):
        original = '#!/bin/sh\necho vendor\n# OTHER_OWNER_BLOCK\necho keep\n'
        added = postboot.transform(original, 'add')
        self.assertEqual(added, postboot.transform(added, 'add'))
        removed = postboot.transform(added, 'remove')
        self.assertIn('# OTHER_OWNER_BLOCK\necho keep', removed)
        self.assertNotIn(postboot.BEGIN, removed)
    def test_refuses_unrecognized_existing_block(self):
        with self.assertRaises(ValueError):
            postboot.transform(postboot.BEGIN + '\necho unexpected\n' + postboot.END, 'add')
    def test_refuses_unpaired_marker(self):
        with self.assertRaises(ValueError):
            postboot.transform(postboot.BEGIN, 'remove')
if __name__ == '__main__':
    unittest.main()
