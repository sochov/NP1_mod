from pathlib import Path
import json
import runpy
import tempfile
import unittest


MODULE = runpy.run_path(str(Path(__file__).resolve().parents[1] / 'tools' / 'plate' / 'quality_gate.py'))


class PlateQualityGateTest(unittest.TestCase):
    def test_frozen_replay_includes_empty_clips_and_uses_original_times(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            paths = {name: root / name for name in ('refs', 'plan', 'used', 'cohort', 'replay')}
            (root / 'front.mp4').write_bytes(b'synthetic new source')
            def write(name, data):
                paths[name].write_text(json.dumps(data), encoding='utf-8')

            day = '2026-11-12T15:'
            span = {'source': 'front.mp4', 'from': day+'30:00Z', 'to': day+'30:05Z'}
            refs = [{'clip': 'first', 'vehicle': 'encounter-1', 'number': '1234', 'lighting': 'day'}]
            write('refs', refs)
            write('plan', [{'clip': 'first', 'lighting': 'day', 'parts': [span]},
                           {'clip': 'empty', 'lighting': 'day',
                            'parts': [{**span, 'from': day+'30:05Z', 'to': day+'30:10Z'}]}])
            write('used', [{'sha256': '0' * 64, 'from': day+'00:00Z', 'to': day+'01:00Z'}])
            freeze = MODULE['freeze_cohort']
            freeze(paths['refs'], paths['plan'], paths['used'], paths['cohort'])
            with self.assertRaises(FileExistsError):
                freeze(paths['refs'], paths['plan'], paths['used'], paths['cohort'])
            cohort = MODULE['read_cohort'](paths['cohort'], paths['refs'])
            saved = {'id': 'record1', 'reading': {'number': '1234'},
                     'firstSeen': day+'30:00Z', 'lastSeen': day+'30:04Z'}
            first = {'clip': 'first', 'records': [saved]}
            audited = [{'clip': 'first', 'id': 'record1', 'number': '1234',
                        'verdict': 'linked', 'vehicle': 'encounter-1'}]
            check = MODULE['check_audit_coverage']
            write('replay', [first])
            self.assertEqual(check(paths['replay'], audited), 1)
            with self.assertRaisesRegex(ValueError, 'frozen clips \(1 missing'):
                check(paths['replay'], audited, cohort)
            write('replay', [first, {'clip': 'empty', 'records': None}])
            self.assertEqual(check(paths['replay'], audited, cohort), 1)
            saved['lastSeen'] = day+'30:05Z'
            write('replay', [first, {'clip': 'empty', 'records': None}])
            with self.assertRaisesRegex(ValueError, 'outside frozen clip'):
                check(paths['replay'], audited, cohort)
            write('refs', [{**refs[0], 'number': '1284'}])
            with self.assertRaisesRegex(ValueError, 'reference file changed'):
                MODULE['read_cohort'](paths['cohort'], paths['refs'])

    def test_frozen_sources_and_encounters_cannot_be_reused(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            source = root / 'renamed-recording.mp4'
            source.write_bytes(b'fixture content')
            refs, plan, used, frozen = (root / q for q in ('refs', 'plan', 'used', 'frozen'))
            def write(path, value):
                path.write_text(json.dumps(value), encoding='utf-8')

            moment = '2026-11-12T12:00:'
            first_span = {'from': moment+'05Z', 'to': moment+'10Z'}
            second_span = {'from': moment+'10Z', 'to': moment+'15Z'}
            cohort = [{'clip': clip, 'lighting': 'day', 'parts': [{**span, 'source': source.name}]}
                      for clip, span in [('first', first_span), ('second', second_span)]]
            write(plan, cohort)
            labels = [{'clip': row['clip'], 'vehicle': 'encounter-a', 'number': '1234', 'lighting': 'day'}
                      for row in cohort]
            write(refs, labels)
            freeze = MODULE['freeze_cohort']
            write(used, [{'sha256': MODULE['sha256'](source), 'from': moment+'00Z', 'to': moment+'02Z'}])
            with self.assertRaisesRegex(ValueError, 'uses a development recording'):
                freeze(refs, plan, used, frozen)
            copy = root / 'original-name.mp4'
            copy.write_bytes(source.read_bytes())
            write(used, [{'source': copy.name, 'from': moment+'00Z', 'to': moment+'02Z'}])
            with self.assertRaisesRegex(ValueError, 'uses a development recording'):
                freeze(refs, plan, used, frozen)
            write(used, [{'sha256': '0' * 64, **first_span}])
            with self.assertRaisesRegex(ValueError, 'overlaps development recording time'):
                freeze(refs, plan, used, frozen)
            write(used, [{'sha256': '0' * 64, 'from': moment+'00Z', 'to': moment+'02Z'}])
            with self.assertRaisesRegex(ValueError, 'more than one reference'):
                freeze(refs, plan, used, frozen)
            self.assertFalse(frozen.exists())

    def test_every_saved_record_including_unknown_needs_an_audit(self):
        saved = [
            {'clip': 'road', 'records': [
                {'id': 'record1', 'reading': {'number': '1234'}},
                {'id': 'record2', 'reading': {'number': '5678'}},
            ]},
            {'clip': 'empty', 'records': None},
        ]
        reviewed = [
            {'clip': 'reference', 'originalClip': 'road', 'id': 'record1', 'number': '1234',
             'verdict': 'linked', 'vehicle': 'vehicle1'},
            {'clip': 'road', 'id': 'record2', 'number': '5678', 'verdict': 'unknown'},
        ]
        with tempfile.TemporaryDirectory() as folder:
            source = Path(folder) / 'replay.json'
            source.write_text(json.dumps(saved), encoding='utf-8')
            check = MODULE['check_audit_coverage']
            self.assertEqual(check(source, reviewed), 2)
            with self.assertRaisesRegex(ValueError, '1 missing'):
                check(source, reviewed[:1])
            with self.assertRaisesRegex(ValueError, '1 extra'):
                check(source, [*reviewed, {'clip': 'empty', 'id': 'record1', 'number': '1234'}])
            with self.assertRaisesRegex(ValueError, '1 serial mismatches'):
                check(source, [reviewed[0], {**reviewed[1], 'number': '5679'}])

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

    def test_unknown_rate_and_precision_bounds_cover_all_saved_records(self):
        references = {('road', 'vehicle1'): '1234'}
        lighting = {'road': 'day', 'rain': 'night'}
        rows = [
            {'clip': 'road', 'id': 'correct', 'number': '1234', 'verdict': 'linked', 'vehicle': 'vehicle1'},
            {'clip': 'road', 'id': 'wrong', 'number': '9999', 'verdict': 'false_detection'},
            {'clip': 'rain', 'id': 'unknown', 'number': '5678', 'verdict': 'unknown'},
        ]
        results = MODULE['score'](references, lighting, rows)
        self.assertEqual(results['all']['precision'], .5)
        self.assertAlmostEqual(results['all']['unknownRate'], 1 / 3)
        self.assertAlmostEqual(results['all']['precisionIfUnknownIncorrect'], 1 / 3)
        self.assertAlmostEqual(results['all']['precisionIfUnknownCorrect'], 2 / 3)
        self.assertEqual(results['day']['unknownRate'], 0)
        self.assertEqual(results['night']['unknownRate'], 1)
        self.assertEqual(results['night']['precisionIfUnknownIncorrect'], 0)
        self.assertEqual(results['night']['precisionIfUnknownCorrect'], 1)

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
