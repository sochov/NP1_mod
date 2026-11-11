"""Owner-authorized fixed-setting roundtrip via the Web API; restores original value.

Device changes use the checked-in, firmware-pinned native worker. No proprietary
file is written by this host tool. Private evidence stays under ignored tmp/.
"""
import http.cookiejar
import json
import pathlib
import ssl
import urllib.request
import uuid
import argparse

parser=argparse.ArgumentParser()
parser.add_argument('--serial',required=True)
parser.add_argument('--setting',choices=['tailgating-sensitivity','tailgating-detection','privacy-mode','driving-support','service-cabin-camera','traffic-check','traffic-congestion','traffic-regulation','ferry','toll-road','smart-ic','route-search','daily-voice','support-voice','front-car-start','signal-change'],default='tailgating-sensitivity')
args=parser.parse_args()
setting=args.setting

root = pathlib.Path(__file__).resolve().parents[1]
access = json.loads((root / 'tmp/device-access.json').read_text())
base = access['url']
opener = urllib.request.build_opener(
    urllib.request.HTTPSHandler(context=ssl._create_unverified_context()),
    urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
csrf = ''

def call(path, method='GET', data=None):
    request = urllib.request.Request(base+'/api/v1'+path, method=method,
        data=None if data is None else json.dumps(data).encode(),
        headers={'Origin':base,'X-CSRF-Token':csrf,'X-Request-ID':str(uuid.uuid4()),'Content-Type':'application/json'})
    with opener.open(request, timeout=25) as response:
        return json.load(response)

def item():
    return next(i for i in call('/device/native-settings')['data']['items']
                if i['id']==setting)

from native_web_baseline import baseline
with baseline(args.serial, root/'tmp'/('native-web-baseline-'+uuid.uuid4().hex)):
    csrf = call('/auth/session')['csrf']
    before = item()
    assert before['writable'] and before['status']=='available'
    target = 0 if before['value'] != 0 else 1
    evidence = {'before':before['value'],'requested':target}
    try:
        result = call('/device/native-settings/'+setting,'PUT',
                      {'expectedValue':before['value'],'value':target})
        current = next(i for i in result['data']['items'] if i['id']==before['id'])
        assert current['value']==target
        evidence['applied']=current['value']
    finally:
        current = item()
        if current['value']==target:
            call('/device/native-settings/'+setting,'PUT',
                 {'expectedValue':target,'value':before['value']})
        evidence['restored']=item()['value']
        evidence['setting']=setting
        (root/('tmp/native-api-'+setting+'-roundtrip.json')).write_text(json.dumps(evidence,indent=2))
        assert evidence['restored']==before['value'], 'Original setting not restored'
    print('PASS: native setting Web apply, persistent read-back and restoration')
