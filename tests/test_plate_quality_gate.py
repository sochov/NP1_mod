from pathlib import Path
import runpy
import unittest


MODULE = runpy.run_path(str(Path(__file__).resolve().parents[1] / 'tools' / 'plate' / 'quality_gate.py'))


class PlateQualityGateTest(unittest.TestCase):
    def test_additional_output_review_changes_precision_only(self):
        references = {('clip1', 'vehicle1'): '1234'}
        lighting = {'clip1': 'day'}
        rows = [
            {'clip': 'clip1', 'id': 'record1', 'number': '1234', 'verdict': 'linked', 'vehicle': 'vehicle1'},
            {'clip': 'clip1', 'id': 'record2', 'number': '5678', 'verdict': 'readback', 'actual': '5678'},
            {'clip': 'clip1', 'id': 'record3', 'number': '1578', 'verdict': 'readback', 'actual': '1573'},
        ]
        result = MODULE['score'](references, lighting, rows)['all']
        self.assertEqual(result['captured'], 1)
        self.assertEqual(result['duplicates'], 0)
        self.assertEqual(result['correct'], 2)
        self.assertEqual(result['wrong'], 1)
