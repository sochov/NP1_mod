"""Forward local HTTPS and record the certificate fingerprint obtained over physical ADB."""
import argparse
import hashlib
import json
import pathlib
import re
import ssl
import subprocess
parser = argparse.ArgumentParser()
parser.add_argument('--serial', required=True)
parser.add_argument('--port', type=int, default=443)
args = parser.parse_args()
if not re.fullmatch(r'[A-Za-z0-9._:-]+', args.serial) or not 1 <= args.port <= 65535:
    raise SystemExit('Invalid serial or port')
adb = ['adb', '-s', args.serial]
subprocess.run(adb + ['forward', 'tcp:' + str(args.port), 'tcp:443'], check=True)
subprocess.run(adb + ['forward', 'tcp:80', 'tcp:80'], check=True)
pem = subprocess.check_output(adb + ['shell', 'cat', '/overlay/owner-apps/np1-control/config/tls.pem']).decode().replace('\r', '')
fingerprint = hashlib.sha256(ssl.PEM_cert_to_DER_cert(pem)).hexdigest()
root = pathlib.Path(__file__).resolve().parent.parent
data = {'url': 'https://127.0.0.1' + ((':' + str(args.port)) if args.port != 443 else ''), 'httpURL':'http://127.0.0.1', 'fingerprint': fingerprint}
(root / 'tmp').mkdir(exist_ok=True)
(root / 'tmp/device-access.json').write_text(json.dumps(data, indent=2), encoding='utf8')
print(data['url'])
print('TLS SHA256:', fingerprint)
print('HTTP: http://127.0.0.1 (no Web password)')
