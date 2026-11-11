"""Real Alexa wake-word API roundtrip with conditional original-state restoration."""
import http.cookiejar
import json
import pathlib
import ssl
import urllib.request
import uuid
import time
import urllib.error

root=pathlib.Path(__file__).resolve().parents[1]
base=json.loads((root/'tmp/device-access.json').read_text())['url']
opener=urllib.request.build_opener(urllib.request.HTTPSHandler(context=ssl._create_unverified_context()),urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
csrf=''
def call(path,method='GET',data=None):
    req=urllib.request.Request(base+'/api/v1'+path,method=method,data=None if data is None else json.dumps(data).encode(),headers={'Origin':base,'X-CSRF-Token':csrf,'X-Request-ID':str(uuid.uuid4()),'Content-Type':'application/json'})
    with opener.open(req,timeout=30) as response:return json.load(response)
csrf=call('/auth/session')['csrf']
before=call('/device/alexa')['data']
assert before['writable'] and not before['wakeWord']['fixed']
word=before['wakeWord']
target=not word['enabled']
evidence={'before':word['value']}
try:
    started=time.monotonic()
    result=call('/device/alexa','PUT',{'expectedValue':word['value'],'enabled':target})['data']
    assert result['wakeWord']['enabled']==target
    evidence['applied']=result['wakeWord']['value']
except urllib.error.HTTPError as error:
    evidence['errorCode']=json.load(error).get('code')
    evidence['requestSeconds']=round(time.monotonic()-started,2)
    raise
finally:
    current=call('/device/alexa')['data']['wakeWord']
    if current['enabled']==target and not current['fixed']:
        call('/device/alexa','PUT',{'expectedValue':current['value'],'enabled':word['enabled']})
    evidence['restored']=call('/device/alexa')['data']['wakeWord']['value']
    (root/'tmp/alexa-api-roundtrip.json').write_text(json.dumps(evidence,indent=2)+'\n')
    assert evidence['restored']==word['value'],'Original Alexa setting not restored'
print('PASS: Alexa Web API change, live and persistent read-back, original setting restored')
