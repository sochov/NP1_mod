import importlib.util
import pathlib
import unittest

ROOT=pathlib.Path(__file__).resolve().parents[1]
SPEC=importlib.util.spec_from_file_location('native_web_baseline',ROOT/'tools/native_web_baseline.py')
BASE=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(BASE)


def fixture(stopped=()):
    blocks=['NCC_BOOT=00000000-0000-0000-0000-000000000001']
    for unit in sorted(BASE.UNITS):
        inactive=unit in stopped
        blocks.append('MainPID='+('0'if inactive else'1234')+'\nId='+unit+'\nLoadState=loaded\nInvocationID='+(''if inactive else'a'*32)+'\nActiveState='+('inactive'if inactive else'active')+'\nSubState='+('dead'if inactive else'running')+'\nJob=')
    return '\n\n'.join(blocks)+'\n'


class NativeWebBaselineTests(unittest.TestCase):
    def test_stable_inactive_consumers_are_preserved_without_starting(self):
        before=BASE.parse(fixture(('app-@DriveRec.service','app-@ImageRecog.service')))
        BASE.require_setting_owner(before);BASE.unchanged(before,BASE.parse(fixture(('app-@DriveRec.service','app-@ImageRecog.service'))))

    def test_stopped_setting_blocks_all_test_writes(self):
        before=BASE.parse(fixture(('app-@Setting.service',)))
        with self.assertRaisesRegex(ValueError,'intentionally stopped'):BASE.require_setting_owner(before)

    def test_failed_transitional_unknown_and_incomplete_states_fail_closed(self):
        source=fixture()
        for invalid in (source.replace('ActiveState=active','ActiveState=failed',1),source.replace('Job=','Job=123 /job/123',1),source.replace('MainPID=1234','MainPID=0',1),'\n\n'.join(source.split('\n\n')[:-1])):
            with self.subTest(invalid=invalid[:50]),self.assertRaises(ValueError):BASE.parse(invalid)

    def test_boot_or_inactive_to_active_change_is_never_repaired_or_ignored(self):
        before=BASE.parse(fixture(('app-@DriveRec.service',)))
        for after in (BASE.parse(fixture()),BASE.parse(fixture(('app-@DriveRec.service',)).replace('000000000001','000000000002',1))):
            with self.assertRaises(ValueError):BASE.unchanged(before,after)


if __name__=='__main__':unittest.main()
