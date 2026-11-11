import importlib.util
import pathlib
import unittest

ROOT=pathlib.Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('h264_live',ROOT/'tools/verify-h264-live.py')
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
def box(kind,payload=b''):
    return (8+len(payload)).to_bytes(4,'big')+kind+payload

class H264LiveCaptureTests(unittest.TestCase):
    def test_stopping_mid_fragment_preserves_all_completed_compressed_bytes(self):
        init=box(b'ftyp')+box(b'moov')
        one=box(b'moof')+box(b'mdat',b'original-compressed-bytes')
        two=box(b'moof')+box(b'mdat',b'next-compressed-bytes')
        for cut in (1,7,8,12,len(two)-1):
            self.assertEqual(module.complete_fragment_prefix(init+one+two[:cut]),init+one)
        self.assertEqual(module.complete_fragment_prefix(init+one+two),init+one+two)
    def test_incomplete_initialization_and_unpaired_media_are_rejected(self):
        for data in (box(b'ftyp'),box(b'moov')+box(b'mdat'),box(b'moov')+box(b'moof')+box(b'moof')):
            with self.assertRaises(ValueError):module.complete_fragment_prefix(data)
