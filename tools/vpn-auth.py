"""Start the fixed Tailscale login flow. Store the short-lived URL only in ignored tmp."""
import http.cookiejar
import json
import pathlib
import urllib.request
import uuid
root = pathlib.Path(__file__).resolve().parents[1]
access=json.loads((root/'tmp/device-access.json').read_text())
base=access['httpURL']
opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
with opener.open(base+'/api/v1/auth/session') as response:
    csrf=json.load(response)['csrf']
req=urllib.request.Request(base+'/api/v1/vpn/tailscale/login',method='POST',headers={'Origin':base,'X-CSRF-Token':csrf,'X-Request-ID':str(uuid.uuid4())})
with opener.open(req,timeout=25) as response:
    data=json.load(response)
(root/'tmp/tailscale-auth.json').write_text(json.dumps(data,indent=2),encoding='utf8')
print('Tailscale login prepared; URL stored in tmp/tailscale-auth.json (never committed).')
