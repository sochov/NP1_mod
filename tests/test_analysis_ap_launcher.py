"""Coordinator I/O fixtures; no SSH session or network mutation."""
import importlib.util
import io
import json
import pathlib
import tempfile
import unittest
from unittest.mock import patch

ROOT = pathlib.Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('ap_launcher', ROOT / 'tools/verify-analysis-ap.py')
M = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(M)


class AnalysisAPLauncher(unittest.TestCase):
    def test_phase_parents_created_and_duplicates_rejected_before_ssh(self):
        (ROOT / 'tmp').mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=ROOT / 'tmp', prefix='phase-fixture-') as directory:
            base = pathlib.Path(directory).resolve()
            self.assertIn(ROOT / 'tmp', base.parents)
            paths = [base / str(n) / 'phase.json' for n in range(4)]
            self.assertEqual(M.prepare_phase_paths(paths), paths)
            self.assertTrue(all(path.parent.is_dir() and not path.exists() for path in paths))
            M.atomic_json(paths[0], {'fixture': True})
            with self.assertRaises(ValueError):
                M.prepare_phase_paths(paths)
            with self.assertRaises(ValueError):
                M.prepare_phase_paths([paths[1]] * 4)

    def test_host_phase_failure_still_drains_remote_complete(self):
        stdin = io.StringIO()
        lines = [json.dumps(v) + '\n' for v in [dict(phase='failure', code='DHCP_TIMEOUT'), dict(phase='complete', passed=False, restored=True)]]
        with patch.object(M, 'atomic_json', side_effect=OSError('fixture-write-failed')):
            result = M.consume_remote(stdin, lines, io.StringIO(), [pathlib.Path('fixture')] * 4, ROOT / 'tmp', True)
        self.assertTrue(result['completeReceived'])
        self.assertTrue(result['restored'])
        self.assertFalse(result['passed'])
        self.assertTrue(result['hostErrors'])

    def test_prepared_write_failure_cancels_then_awaits_cleanup(self):
        stdin = io.StringIO()
        events = [dict(phase='prepared'), dict(phase='failure'), dict(phase='complete', passed=False, restored=True)]
        with patch.object(M, 'atomic_json', side_effect=[OSError('fixture'), None, None]):
            result = M.consume_remote(stdin, [json.dumps(v) for v in events], io.StringIO(), [pathlib.Path('fixture')] * 4, ROOT / 'tmp', True, gate_wait=lambda *args: dict(cleanup=True, restored=True))
        gates = [json.loads(v) for v in stdin.getvalue().splitlines()]
        self.assertEqual(gates, [dict(proceed=False), dict(cleanup=True, restored=True)])
        self.assertTrue(result['completeReceived'])
        self.assertFalse(result['passed'])

    def test_cleanup_timeout_does_not_invent_upstream_restore(self):
        stdin = io.StringIO()
        events = [dict(phase='prepared'), dict(phase='traffic'), dict(phase='complete', passed=False, restored=True)]
        with patch.object(M, 'atomic_json'):
            gates = iter([dict(proceed=True), TimeoutError('fixture')])
            def wait(*args):
                value = next(gates)
                if isinstance(value, Exception): raise value
                return value
            result = M.consume_remote(stdin, [json.dumps(v) for v in events], io.StringIO(), [pathlib.Path('fixture')] * 4, ROOT / 'tmp', True, gate_wait=wait)
        self.assertIn(dict(cleanup=True, restored=False), [json.loads(v) for v in stdin.getvalue().splitlines()])
        self.assertTrue(result['completeReceived'])
        self.assertFalse(result['passed'])

    def test_event_record_failure_does_not_close_before_complete(self):
        class FailedLog:
            def write(self, value): raise OSError('fixture')
        result = M.consume_remote(io.StringIO(), [json.dumps(dict(phase='complete', passed=True, restored=True))], FailedLog(), [], ROOT / 'tmp', True)
        self.assertTrue(result['completeReceived'])
        self.assertFalse(result['passed'])


if __name__ == '__main__':
    unittest.main()
