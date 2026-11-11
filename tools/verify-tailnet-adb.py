"""Verify root ADB over the device's observed Tailscale address, then restore USB forwards."""
import argparse
import ipaddress
import json
import pathlib
import shutil
import ssl
import subprocess
import time
import urllib.request

parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--serial',required=True)
parser.add_argument('--adb',default=shutil.which('adb'))
args=parser.parse_args()
if not args.adb:raise SystemExit('adb not found; pass --adb')
root=pathlib.Path(__file__).resolve().parents[1]
base=json.loads((root/'tmp/device-access.json').read_text())['url']
with urllib.request.urlopen(base+'/api/v1/vpn/tailscale/status',context=ssl._create_unverified_context(),timeout=15) as response:
    state=json.load(response)['data']
assert state['online'] is True
address=next((ipaddress.ip_address(value) for value in state['ips']
              if ipaddress.ip_address(value).version==4 and ipaddress.ip_address(value) in ipaddress.ip_network('100.64.0.0/10')),None)
assert address is not None,'No observed Tailscale IPv4'
target=f'{address}:5556'
evidence={}
try:
    # Keep addresses and connection diagnostics in the ignored local evidence.
    result=subprocess.run([args.adb,'connect',target],capture_output=True,text=True,timeout=15)
    (root/'tmp/tailnet-adb-connect.txt').write_text(result.stdout+result.stderr)
    root_id=subprocess.check_output([args.adb,'-s',target,'shell','id -u'],timeout=10,text=True).strip()
    assert root_id=='0','Tailscale ADB was not a root shell'
    evidence['rootADB']=True
finally:
    subprocess.run([args.adb,'disconnect',target],capture_output=True,timeout=10)
    import os
    environment=dict(os.environ)
    environment['PATH']=str(pathlib.Path(args.adb).resolve().parent)+os.pathsep+environment['PATH']
    # The target's ADB daemon briefly closes USB transports when a TCP peer
    # disconnects. Wait and retry our read-only forward setup, never services.
    restored=False
    for attempt in range(4):
        subprocess.run([args.adb,'-s',args.serial,'wait-for-device'],timeout=15,check=True)
        result=subprocess.run([shutil.which('python') or 'python',str(root/'tools/connect.py'),'--serial',args.serial],env=environment,capture_output=True,text=True,timeout=20)
        if result.returncode==0:
            restored=True
            break
        time.sleep(.5+attempt*.5)
    assert restored,'USB management forwards could not be restored'
    (root/'tmp/tailnet-adb-postboot.json').write_text(json.dumps(evidence,indent=2)+'\n')
print('PASS: observed Tailscale address exposes root ADB; USB HTTP/HTTPS forwards restored')
