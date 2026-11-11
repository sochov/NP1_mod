"""Temporal diagnostic must distinguish mmap updates, static scenes and truncation."""
import importlib.util
import pathlib
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('camera_diagnostic', ROOT / 'tools/diagnose-camera-export.py')
DIAGNOSTIC = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(DIAGNOSTIC)


def sample(**changes):
    result = {'capturedAt': '2026-11-11T00:00:00Z', 'size': 3112960, 'readBytes': 3112960,
              'inode': 1, 'mtime': 1, 'frameSHA256': 'a', 'prefixSHA256': 'a',
              'lumaSHA256': 'a', 'chromaSHA256': 'a', 'producerTimestamp': [1, 0]}
    return dict(result, **changes)


class CameraExportDiagnosticTests(unittest.TestCase):
    def test_mmap_frame_update_is_visible_with_unchanged_metadata(self):
        report = DIAGNOSTIC.compare_samples([sample(), sample(frameSHA256='b', lumaSHA256='b')])
        self.assertTrue(report['sameMtime'])
        self.assertTrue(report['sameInode'])
        self.assertTrue(report['frameChanged'])

    def test_static_scene_can_have_a_live_producer(self):
        report = DIAGNOSTIC.compare_samples([sample(), sample(inode=2, mtime=2, producerTimestamp=[2, 0])])
        self.assertFalse(report['frameChanged'])
        self.assertTrue(report['producerTimestampChanged'])
        self.assertFalse(report['sameInode'])

    def test_short_pull_is_not_a_complete_frame(self):
        report = DIAGNOSTIC.compare_samples([sample(), sample(readBytes=100)])
        self.assertFalse(report['completeFrames'])

    def test_one_sample_is_inconclusive(self):
        with self.assertRaises(ValueError):
            DIAGNOSTIC.compare_samples([sample()])


if __name__ == '__main__':
    unittest.main()
