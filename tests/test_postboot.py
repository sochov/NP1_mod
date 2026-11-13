import importlib.util
import pathlib
import unittest
import tempfile
spec = importlib.util.spec_from_file_location('postboot', pathlib.Path(__file__).resolve().parents[1] / 'tools/device/postboot.py')
postboot = importlib.util.module_from_spec(spec)
spec.loader.exec_module(postboot)
class PostbootTests(unittest.TestCase):
    def test_known_ap_block_migrates_without_touching_foreign_text(self):
        text='#!/bin/sh\necho vendor\n'+postboot.AP_BLOCK_LEGACY+'\n# OTHER_OWNER\n'
        after=postboot.transform(text,'add',postboot.AP_BLOCK)
        self.assertIn('touch /run/xtables.lock',after)
        self.assertIn('# OTHER_OWNER',after)
        self.assertEqual(after,postboot.transform(after,'add',postboot.AP_BLOCK))
        unknown=text.replace('systemctl daemon-reload','echo altered')
        with self.assertRaises(ValueError):postboot.transform(unknown,'add',postboot.AP_BLOCK)
    def test_all_feature_blocks_preserve_each_other(self):
        blocks=(postboot.BLOCK,postboot.VPN_BLOCK,postboot.LED_BLOCK,postboot.MODEM_BLOCK,postboot.NATIVE_BLOCK,postboot.AP_BLOCK,postboot.APN_BLOCK,postboot.PLATE_BLOCK,postboot.OTA_GUARD_BLOCK,postboot.UPLINK_BLOCK)
        value='#!/bin/sh\necho vendor\n'
        for block in blocks:
            value=postboot.transform(value,'add',block)
            self.assertEqual(value,postboot.transform(value,'add',block))
        for block in blocks:
            self.assertIn(block,value)
        for block in reversed(blocks):
            value=postboot.transform(value,'remove',block)
        self.assertIn('echo vendor',value)
        self.assertNotIn('NP1_CONTROL_CENTER',value)
    def test_atomic_persistent_target_and_live_target(self):
        with tempfile.TemporaryDirectory() as directory:
            root=pathlib.Path(directory)
            persistent=root/'upper.sh'
            live=root/'live.sh'
            for path in (persistent,live):
                path.write_text('#!/bin/sh\necho vendor\n',encoding='utf8')
                postboot.update_target(str(path),'add',postboot.BLOCK)
                self.assertIn(postboot.BLOCK,path.read_text(encoding='utf8'))
                postboot.update_target(str(path),'remove',postboot.BLOCK)
                self.assertNotIn(postboot.BEGIN,path.read_text(encoding='utf8'))
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
