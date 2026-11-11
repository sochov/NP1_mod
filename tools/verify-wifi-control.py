"""Real Wi-Fi control trial: current-profile confirm, unavailable target, rollback.

Uses the typed Web API over the owner's USB forward. A synthetic unavailable
network is created briefly, then removed by NCC's normal rollback operation.
Never prints SSIDs, credentials, addresses, or profile IDs.
"""
import http.cookiejar
import json
import pathlib
import secrets
import ssl
import time
import urllib.request
import uuid

root=pathlib.Path(__file__).resolve().parents[1]
base=json.loads((root/'tmp/device-access.json').read_text())['url']
opener=urllib.request.build_opener(urllib.request.HTTPSHandler(context=ssl._create_unverified_context()),urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
csrf=''
def call(path,method='GET',data=None):
    request=urllib.request.Request(base+'/api/v1/network/wifi/'+path if path else base+'/api/v1/auth/session',method=method,data=None if data is None else json.dumps(data).encode(),headers={'Origin':base,'X-CSRF-Token':csrf,'X-Request-ID':str(uuid.uuid4()),'Content-Type':'application/json'})
    with opener.open(request,timeout=30) as response:return json.load(response)
csrf=call('')['csrf']
before=call('networks')
assert before['status']['state']=='COMPLETED' and before['status']['networkID'] is not None
assert not before['transaction'] or before['transaction']['state']!='pending'
original=before['status']['networkID']
trial=call('connect-saved','POST',{'networkID':original})
assert trial['state']=='pending'
assert call('confirm','POST',{'transactionID':trial['id']})['state']=='confirmed'
assert call('networks')['status']['networkID']==original
trial=None
try:
    trial=call('connect-new','POST',{'ssid':'NCC-unavailable-'+secrets.token_hex(8),'security':'wpa2','password':secrets.token_urlsafe(18)})
    assert trial['state']=='pending'
    time.sleep(2)
finally:
    state=call('networks')
    pending=state.get('transaction')
    if pending and pending['state']=='pending':
        assert call('rollback','POST',{'transactionID':pending['id']})['state']=='rolled-back'
after=call('networks')
assert after['status']['state']=='COMPLETED' and after['status']['networkID']==original
assert {(n['id'],n['ssid']) for n in after['networks']}=={(n['id'],n['ssid']) for n in before['networks']}
assert after['saved']==before['saved']
print('PASS: current Wi-Fi confirm, unavailable target rollback, owned profile cleanup and original connection read-back')
