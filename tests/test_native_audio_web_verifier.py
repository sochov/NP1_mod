import copy
import importlib.util
import pathlib
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('audio_web', ROOT/'tools/verify-native-audio-control.py')
WEB = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(WEB)


def observation(mute=1, correction=1):
    return {'status': 'available', 'data': {'masterVolume': 17, 'mute': {'value': mute, 'enabled': mute==0},
            'correction': {'value': correction, 'label': ('OFF','弱','強')[correction]}, 'writable': True}}


class AudioWebVerifierTests(unittest.TestCase):
    def test_both_targets_restore_after_confirmed_puts(self):
        for id in WEB.IDS:
            with self.subTest(id=id):
                state=observation();original=copy.deepcopy(state);puts=[]
                def call(path,method='GET',body=None):
                    if method=='PUT':
                        self.assertTrue(path.endswith('/'+id));field='mute'if id=='mute'else'correction'
                        self.assertEqual(state['data'][field]['value'],body['expectedValue']);puts.append(body)
                        value=body['value'];state['data'][field]['value']=value
                        if id=='mute':state['data']['mute']['enabled']=value==0
                        else:state['data']['correction']['label']=('OFF','弱','強')[value]
                    return copy.deepcopy(state)
                evidence={};WEB.roundtrip(call,id,None,evidence)
                self.assertEqual(state,original);self.assertEqual(len(puts),2)
                self.assertEqual(evidence['restored'],original['data'])

    def test_lost_put_response_still_restores_only_own_matching_value(self):
        state=observation();puts=[]
        def call(path,method='GET',body=None):
            if method=='PUT':
                puts.append(body);state['data']['mute']={'value':body['value'],'enabled':body['value']==0}
                if len(puts)==1:raise TimeoutError('synthetic lost response')
            return copy.deepcopy(state)
        evidence={}
        with self.assertRaises(TimeoutError):WEB.roundtrip(call,'mute',0,evidence)
        self.assertEqual(state['data']['mute']['value'],1);self.assertEqual(len(puts),2)
        self.assertEqual(evidence['restored']['mute']['value'],1)

    def test_competing_third_value_is_never_overwritten(self):
        state=observation(correction=0);puts=0
        def call(path,method='GET',body=None):
            nonlocal puts
            if method=='PUT':
                puts+=1;state['data']['correction']={'value':2,'label':'強'}
                raise TimeoutError('synthetic concurrent change')
            return copy.deepcopy(state)
        evidence={}
        with self.assertRaisesRegex(ValueError,'competing'):WEB.roundtrip(call,'volume-correction',1,evidence)
        self.assertEqual(puts,1);self.assertEqual(state['data']['correction']['value'],2)

    def test_unknown_state_and_master_volume_write_are_rejected(self):
        for invalid in ({},observation(mute=1)):
            bad=copy.deepcopy(invalid)
            if 'data'in bad:bad['data']['mute']['enabled']=True
            with self.assertRaises(ValueError):WEB.inspect(bad)
        with self.assertRaises(ValueError):WEB.roundtrip(lambda *_:observation(),'master-volume',1,{})


if __name__=='__main__':unittest.main()
