"""Send a quiet synthetic tone through the Web PCM endpoint, never a microphone."""
import argparse
import base64
import http.cookiejar
import json
import math
import pathlib
import shutil
import ssl
import struct
import subprocess
import time
import urllib.error
import urllib.request
import uuid

parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--serial',required=True)
parser.add_argument('--adb',default=shutil.which('adb'))
args=parser.parse_args()
if not args.adb:raise SystemExit('adb not found; pass --adb')
root=pathlib.Path(__file__).resolve().parents[1]
base=json.loads((root/'tmp/device-access.json').read_text())['url']
opener=urllib.request.build_opener(urllib.request.HTTPSHandler(context=ssl._create_unverified_context()),urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
csrf=''
def call(path,method='GET',data=None):
    req=urllib.request.Request(base+'/api/v1'+path,method=method,data=None if data is None else json.dumps(data).encode(),headers={'Origin':base,'X-CSRF-Token':csrf,'X-Request-ID':str(uuid.uuid4()),'Content-Type':'application/json'})
    with opener.open(req,timeout=10) as response:return json.load(response)
def pulse():
    return subprocess.check_output([args.adb,'-s',args.serial,'shell','pactl list sink-inputs'],text=True)
csrf=call('/auth/session')['csrf']
session=None;evidence={}
try:
    session=call('/device/audio/sessions','POST')
    path='/device/audio/sessions/'+session['id']
    assert session['sampleRate']==24000 and session['channels']==1
    for sequence in range(1,13):
        pcm=b''.join(struct.pack('<h',round(1800*math.sin(2*math.pi*440*(n+(sequence-1)*4800)/24000))) for n in range(4800))
        result=call(path,'PUT',{'sequence':sequence,'pcm':base64.b64encode(pcm).decode()})
        assert result['accepted'] and result['sequence']==sequence
        if sequence==4:
            live=pulse()
            assert 'NP1-Web-Audio' in live and 'Browser-screen-audio' in live,'PulseAudio did not observe our stream'
            evidence['pulseStreamObserved']=True
        time.sleep(.2)
    time.sleep(2.3)
    assert 'Browser-screen-audio' not in pulse(),'Idle audio output survived its lease'
    evidence['idleOutputClosed']=True
    try:call(path,'PUT',{'sequence':13,'pcm':'AAA='})
    except urllib.error.HTTPError as error:assert error.code==410
    else:raise AssertionError('Expired audio session accepted PCM')
    session=None
finally:
    if session:
        try:call('/device/audio/sessions/'+session['id'],'DELETE')
        except urllib.error.HTTPError as error:
            if error.code!=410:raise
    (root/'tmp/browser-audio-hardware.json').write_text(json.dumps(evidence,indent=2)+'\n')
print('PASS: Web PCM accepted, dedicated PulseAudio stream observed, idle output closed; acoustic quality requires listening')
