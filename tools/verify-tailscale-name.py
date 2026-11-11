"""Change the local Tailscale hostname through Web API and restore it by CAS."""
import http.cookiejar
import json
import pathlib
import re
import ssl
import urllib.request
import uuid

root=pathlib.Path(__file__).resolve().parents[1]
base=json.loads((root/'tmp/device-access.json').read_text())['url']
opener=urllib.request.build_opener(urllib.request.HTTPSHandler(context=ssl._create_unverified_context()),urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
csrf=''
def call(path,method='GET',data=None):
    request=urllib.request.Request(base+'/api/v1'+path,method=method,data=None if data is None else json.dumps(data).encode(),headers={'Origin':base,'X-CSRF-Token':csrf,'X-Request-ID':str(uuid.uuid4()),'Content-Type':'application/json'})
    with opener.open(request,timeout=25) as response:return json.load(response)
def status():return call('/vpn/tailscale/status')['data']
csrf=call('/auth/session')['csrf'];before=status();original=before['hostName']
assert re.fullmatch(r'[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?',original)
assert before['nameWritable'] and before['accountLogin'] and before['deviceName']
target='np1-check-'+uuid.uuid4().hex[:10]
evidence={}
try:
    changed=call('/vpn/tailscale/name','PUT',{'expectedHostName':original,'hostName':target})['data']
    assert changed['hostName']==target
    if before['deviceName']==original:
        assert changed['deviceName']!=before['deviceName'],'Registered name did not change'
        evidence['registeredNameChanged']=True
    assert changed['accountLogin']==before['accountLogin']
    assert changed['ips']==before['ips']
    evidence['hostNameChanged']=True
    evidence['accountPreserved']=True
    evidence['addressesPreserved']=True
finally:
    current=status()
    if current['hostName']==target:
        restored=call('/vpn/tailscale/name','PUT',{'expectedHostName':target,'hostName':original})['data']
    elif current['hostName']==original:restored=current
    else:raise AssertionError('Concurrent name change; refused blind restoration')
    assert restored['hostName']==original
    if before['deviceName']==original:assert restored['deviceName']==original
    evidence['originalRestored']=True
    (root/'tmp/tailscale-name-roundtrip.json').write_text(json.dumps(evidence,indent=2)+'\n')
print('PASS: Tailscale hostname Web change and restoration; owner identity and addresses preserved')
