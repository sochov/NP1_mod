import datetime as dt
from pathlib import Path
import runpy
import tempfile
import unittest


MODULE = runpy.run_path(str(Path(__file__).resolve().parents[1] / 'tools' / 'plate' / 'select_drive_windows.py'))


def rmc(fix, minutes, speed):
    body = f'GPRMC,{fix:%H%M%S}.00,A,35{minutes:07.4f},N,13900.0000,E,{speed},0.0,{fix:%d%m%y},,,A'
    checksum = 0
    for char in body:
        checksum ^= ord(char)
    return f'${body}*{checksum:02X}\n'


class SelectPlateDriveWindows(unittest.TestCase):
    def test_identifies_moving_windows_and_rejects_stationary_recordings(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for name, speed, movement in (('120000', 14, .004), ('121000', 0, 0), ('122000', 14, 0)):
                video = root / f'FILEM_261120_{name}_0000.MP4'
                video.touch()
                start = dt.datetime.strptime('20261120' + name, '%Y%m%d%H%M%S') - dt.timedelta(hours=9)
                video.with_suffix('.NMEA').write_text(''.join(
                    rmc(start + dt.timedelta(seconds=i), i * movement, speed) for i in range(160)
                ), encoding='ascii')

            clips, totals = MODULE['select'](root, set(), 45)
            self.assertEqual(totals['noMovingWindow'], 2)
            self.assertEqual(totals['dayFiles'], 1)
            self.assertEqual({clip['offset'] for clip in clips}, {8, 53, 98})
            self.assertTrue(all(clip['motion']['movingSeconds'] == 45 for clip in clips))

    def test_excludes_duplicate_video_names_and_rejects_bad_nmea_checksums(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            duplicate = 'FILEM_261120_181000_0000.MP4'
            (root / duplicate).touch()
            video = root / 'FILEM_261120_180000_0000.MP4'
            video.touch()
            good = rmc(dt.datetime(2026, 11, 20, 9), 0, 14)
            bad = good[:-3] + ('FF' if good[-3:-1] != 'FF' else '00') + '\n'
            video.with_suffix('.NMEA').write_text(bad, encoding='ascii')

            clips, totals = MODULE['select'](root, {duplicate.casefold()}, 45)
            self.assertEqual(clips, [])
            self.assertEqual(totals['excludedDuplicateName'], 1)
            self.assertEqual(totals['noValidFix'], 1)
