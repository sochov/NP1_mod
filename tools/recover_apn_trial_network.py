"""Recover only the missing default from the controlled APN trial's baseline."""
import argparse
import ipaddress
import json
import pathlib
import re
import subprocess

parser = argparse.ArgumentParser()
parser.add_argument('--serial', required=True)
args = parser.parse_args()
if not re.fullmatch(r'[A-Za-z0-9._:-]+', args.serial):
    raise SystemExit('Invalid serial')
root = pathlib.Path(__file__).resolve().parent.parent
source = root/'tmp'/'ap-standalone-field-1'
before = json.loads((source/'wifi-before.json').read_text(encoding='utf-8-sig'))
after = json.loads((source/'wifi-after.json').read_text(encoding='utf-8-sig'))
if before['status'] != after['status'] or before['networks'] != after['networks'] or after['status']['state'] != 'COMPLETED':
    raise SystemExit('Verified WiFi baseline differs')
status = after['status']
if type(status['networkID']) is not int or not 0 <= status['networkID'] <= 999999 or not 1 <= len(status['ssid'].encode('utf-8')) <= 32:
    raise SystemExit('Verified WiFi baseline invalid')
ipaddress.IPv4Address(status['ipv4'])
private = root/'tmp'/'native-apn-trial-private'/'expected-wifi.txt'
private.write_text('NCCWIFI1\n'+status['ssid'].encode('utf-8').hex()+'\n'+str(status['networkID'])+'\n'+status['ipv4']+'\n', encoding='utf-8')
adb = ['adb', '-s', args.serial]
stage = '/tmp/ncc-native-apn-network-recovery'
for name in ('recover-apn-trial-default.sh', 'recover-apn-trial-default.py'):
    subprocess.run(adb+['push', str(root/'tools'/'device'/name), stage+'/'+name], check=True)
command = adb+['shell', '/bin/sh', stage+'/recover-apn-trial-default.sh']
subprocess.run(command+['prepare'], check=True)
subprocess.run(adb+['push', str(private), '/run/np1-native-apn-network-recovery/expected-wifi.txt'], check=True)
output = subprocess.check_output(command+['restore'], stderr=subprocess.STDOUT).decode('utf-8', errors='replace')
if 'Original WiFi default route restored;' not in output:
    raise SystemExit('Recovery did not produce verified completion')
print('Original WiFi default route restored; same profile, IPv4 and stock services verified')
