from pathlib import Path
import json
import runpy
import tempfile
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

    def test_twilight_counts_toward_overall_but_not_day_or_night(self):
        labels = [{'clip': f'clip{i}', 'vehicle': 'vehicle1', 'number': '1234', 'lighting': group}
                  for i, group in enumerate(['day'] * 40 + ['night'] * 40 + ['twilight'] * 40)]
        rows = [{'clip': row['clip'], 'id': 'record1', 'number': row['number'],
                 'verdict': 'linked', 'vehicle': row['vehicle']} for row in labels]
        with tempfile.TemporaryDirectory() as folder:
            source = Path(folder) / 'refs.json'
            source.write_text(json.dumps(labels), encoding='utf-8')
            references, lighting = MODULE['read_references'](source)
        results = MODULE['score'](references, lighting, rows)
        self.assertEqual(results['all']['references'], 120)
        self.assertEqual(results['all']['captured'], 120)
        self.assertEqual(results['twilight']['captured'], 40)
        self.assertTrue(all(MODULE['gate'](results).values()))

        lighting['clip0'] = 'twilight'
        results = MODULE['score'](references, lighting, rows)
        self.assertEqual(results['all']['references'], 120)
        self.assertEqual(results['day']['references'], 39)
        self.assertFalse(MODULE['gate'](results)['sampleSize'])
