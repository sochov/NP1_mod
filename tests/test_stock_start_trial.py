"""Host validation for the fixed start-barrier launcher; never accesses ADB."""
import importlib.util
import pathlib
import subprocess
import unittest

ROOT=pathlib.Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('stock_start_stage',ROOT/'tools/stage-stock-start.py')
stage=importlib.util.module_from_spec(spec)
spec.loader.exec_module(stage)

class StockStartTrialTests(unittest.TestCase):
    def test_preflight_parse(self):
        for setting in stage.IDS:
            args=stage.parse_arguments(['--serial','HOST_TEST_ONLY','--id',setting])
            self.assertEqual(args.action,'preflight')
            self.assertEqual(args.id,setting)

    def test_notify_parse(self):
        args=stage.parse_arguments(['--serial','HOST_TEST_ONLY','--id','drive-rec','--action','notify'])
        self.assertEqual(args.action,'notify')

    def test_unknown_or_shell_input_refused(self):
        for args in (['--serial','HOST_TEST_ONLY','--id','Voice'],['--serial','; touch /tmp/no','--id','drive-rec'],['--serial','HOST_TEST_ONLY','--id','drive-rec','--action','restart']):
            with self.assertRaises(SystemExit):
                stage.parse_arguments(args)

    def test_script_has_no_service_mutation(self):
        text=(ROOT/'tools/device/stock-start-trial.sh').read_text(encoding='utf8')
        for forbidden in ('systemctl start','systemctl stop','systemctl restart','reboot','kill -','killall','dd '):
            self.assertNotIn(forbidden,text)
        self.assertIn('cmp -s "$BASE/stock-before.txt" "$BASE/stock-after.txt"',text)
        self.assertIn('NCC_EXIT_STATUS=%s',text)

    def test_shell_syntax(self):
        bash=pathlib.Path(r'C:\Program Files\Git\bin\bash.exe')
        if not bash.is_file():
            self.skipTest('Git Bash not installed')
        subprocess.run([str(bash),'-n',str(ROOT/'tools/device/stock-start-trial.sh')],check=True)

if __name__=='__main__':
    unittest.main()
