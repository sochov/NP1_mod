"""Initialize the NCC AP through its typed API. Credentials stay in ignored tmp.

Run after the isolated AP/DHCP trial passes. A configured AP is never overwritten.
The enabled AP remains available for the owner and can be changed from Wi-Fi/AP.
"""
import http.cookiejar
import json
import pathlib
import secrets
import ssl
import urllib.request
import urllib.error
import uuid

root=pathlib.Path(__file__).resolve().parents[1]
base=json.loads((root/'tmp/device-access.json').read_text())['url']
opener=urllib.request.build_opener(urllib.request.HTTPSHandler(context=ssl._create_unverified_context()),urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
csrf=''
def call(path,method='GET',data=None):
    request=urllib.request.Request(base+'/api/v1'+path,method=method,data=None if data is None else json.dumps(data).encode(),headers={'Origin':base,'X-CSRF-Token':csrf,'X-Request-ID':str(uuid.uuid4()),'Content-Type':'application/json'})
    with opener.open(request,timeout=45) as response:return json.load(response)
csrf=call('/auth/session')['csrf']
before=call('/network/hotspot')['data']['settings']
if before['enabled'] or before['passphraseConfigured']:
    raise SystemExit('AP is already configured; use its existing Web settings instead')
private=root/'tmp/ap-client-access.json'
if private.exists():
    access=json.loads(private.read_text())
    if access.get('ssid')!='NP1-LAN' or not 8<=len(access.get('password',''))<=63:
        raise SystemExit('Existing private AP access file is not a valid initialization record')
else:
    access={'ssid':'NP1-LAN','password':secrets.token_urlsafe(18)}
    private.write_text(json.dumps(access,indent=2)+'\n')
try:
    result=call('/network/hotspot','PUT',{'expectedRevision':before['revision'],'settings':{'enabled':True,'ssid':access['ssid'],'passphrase':access['password']}})
except urllib.error.HTTPError as error:
    detail=json.load(error)
    raise SystemExit('AP apply not confirmed: '+str(detail.get('code','unknown'))+' '+str(detail.get('reason','')))
if not result['data']['enabled'] or not result['data']['dhcpReady']:
    raise SystemExit('AP enable was not confirmed; inspect Web status')
print('PASS: AP enabled with DHCP. Access credentials: tmp/ap-client-access.json (Git ignored)')
