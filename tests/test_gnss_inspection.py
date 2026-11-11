import importlib.util
import json
import pathlib
import unittest

ROOT=pathlib.Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('inspect_gnss',ROOT/'tools/inspect-gnss.py')
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
def sentence(text):
    checksum=0
    for byte in text.encode():checksum^=byte
    return ('$'+text+'*'+format(checksum,'02X')+'\n').encode()

class GNSSInspection(unittest.TestCase):
    def test_aggregate_reports_fix_flags_without_position_values(self):
        result=module.summarize(sentence('GPRMC,120000,A,3500.0000,N,13900.0000,E,0,0,111126,,,A')+sentence('GPGGA,120000,3500.0000,N,13900.0000,E,1,08,1.2,0,M,0,M,,'))
        self.assertEqual(result['rmcStatus'],{'A':1});self.assertEqual(result['ggaQuality'],{'1':1});self.assertEqual(result['maxUsedSatellites'],8)
        self.assertNotIn('3500.0000',json.dumps(result));self.assertNotIn('13900.0000',json.dumps(result))
    def test_invalid_checksum_does_not_claim_a_fix(self):
        result=module.summarize(b'$GPRMC,120000,A*00\n'+sentence('GPRMC,,V,,,,,,,,,,N'))
        self.assertEqual(result['rmcStatus'],{'V':1});self.assertEqual(result['badChecksums'],1)
