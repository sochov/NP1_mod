"""Exercise five-zone screen colors and automatic restoration on the real NP1."""
import http.cookiejar
import json
import pathlib
import ssl
import time
import urllib.error
import urllib.request
import uuid

root=pathlib.Path(__file__).resolve().parents[1]
base=json.loads((root/'tmp/device-access.json').read_text())['url']
opener=urllib.request.build_opener(urllib.request.HTTPSHandler(context=ssl._create_unverified_context()),urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
csrf=''
def call(path,method='GET',data=None):
    request=urllib.request.Request(base+'/api/v1'+path,method=method,data=None if data is None else json.dumps(data).encode(),headers={'Origin':base,'X-CSRF-Token':csrf,'X-Request-ID':str(uuid.uuid4()),'Content-Type':'application/json'})
    with opener.open(request,timeout=10) as response:return json.load(response)
def wait(predicate):
    deadline=time.monotonic()+4
    while time.monotonic()<deadline:
        value=call('/device/led')['data']
        if predicate(value):return value
        time.sleep(.1)
    raise AssertionError('LED command state did not reach the expected mode')
csrf=call('/auth/session')['csrf']
original=call('/device/led')['data']['settings']
session=None
evidence={}
try:
    session=call('/device/led/screen/sessions','POST',{})
    path='/device/led/screen/sessions/'+session['id']
    zones=[[180,0,0],[0,180,0],[0,0,180],[90,90,90],[90,45,10]]
    for sequence in range(1,13):
        result=call(path,'PUT',{'sequence':sequence,'zones':zones})
        assert result['accepted'] and result['sequence']==sequence
        time.sleep(.105)
    value=wait(lambda v:v['lastCommand']['mode']=='screen' and v['lastCommand'].get('screenSequence')==12)
    assert value['lastCommand']['zones']==zones
    assert value['settings']==original
    evidence['fiveZoneCommands']=True
    time.sleep(1.7)
    value=wait(lambda v:v['lastCommand']['mode']==original['mode'])
    assert value['settings']==original
    evidence['timeoutRestored']=True
    try:call(path,'PUT',{'sequence':13,'zones':zones})
    except urllib.error.HTTPError as error:assert error.code==410
    else:raise AssertionError('Expired session accepted a frame')
    session=call('/device/led/screen/sessions','POST',{})
    path='/device/led/screen/sessions/'+session['id']
    call(path,'PUT',{'sequence':1,'zones':zones})
    wait(lambda v:v['lastCommand']['mode']=='screen')
    call(path,'DELETE')
    session=None
    wait(lambda v:v['lastCommand']['mode']==original['mode'])
    evidence['stopRestored']=True
finally:
    if session:
        try:call('/device/led/screen/sessions/'+session['id'],'DELETE')
        except urllib.error.HTTPError as error:
            if error.code!=410:raise
    (root/'tmp/screen-led-hardware.json').write_text(json.dumps(evidence,indent=2)+'\n')
assert call('/device/led')['data']['settings']==original
print('PASS: five distinct RGB commands, unchanged saved settings, timeout and explicit-stop restoration')
