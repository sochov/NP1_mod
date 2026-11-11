"""Read-only verification of persistent management services after a reboot."""
import json
import pathlib
import ssl
import urllib.request

root=pathlib.Path(__file__).resolve().parents[1]
base=json.loads((root/'tmp/device-access.json').read_text())['url']
context=ssl._create_unverified_context()
def get(path):
    with urllib.request.urlopen(base+'/api/v1'+path,context=context,timeout=15) as response:return json.load(response)
summary=get('/system/summary')
ota=get('/device/ota-policy')['data']
archive=get('/trips/archive')['data']
ap=get('/network/hotspot')['data']
assert all(item['state']=='active' for item in summary['services']['data'])
assert ota['disabled'] and ota['managed'] and ota['writable']
assert archive['active'] and archive['storage']['available'] and archive['lastPoll']
assert ap['enabled'] and ap['dhcpReady'] and ap['sharing']['ready']
assert summary['tailscale']['data']['online'] is True
report={'stockServices':True,'otaStillDisabled':True,'retainedJourneyCollector':True,
        'hotspotAutomaticallySharing':True,'upstream':ap['sharing']['upstream']['kind'],
        'tailscaleOnline':True}
(root/'tmp/postboot-service-verification.json').write_text(json.dumps(report,indent=2)+'\n')
print('PASS: stock services, OTA policy, journey collector, automatic hotspot sharing and Tailscale after reboot')
