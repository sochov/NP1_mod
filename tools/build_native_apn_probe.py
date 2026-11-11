"""Build the opt-in fixed native APN status probe, independently of the UI bundle."""
import argparse
import os
import pathlib
import shutil
import subprocess
import sys

parser = argparse.ArgumentParser()
parser.add_argument('--go', default='go')
args = parser.parse_args()
root = pathlib.Path(__file__).resolve().parent.parent
go = shutil.which(args.go) or str(pathlib.Path(args.go).resolve())
bundle = root / 'artifacts' / 'ncc-native-apn-readonly'
(bundle / 'bin').mkdir(parents=True, exist_ok=True)
(bundle / 'tools').mkdir(exist_ok=True)
subprocess.run([go, 'test', './cmd/ncc', './internal/modem'], cwd=root / 'backend', check=True)
subprocess.run([go, 'vet', './cmd/ncc', './internal/modem'], cwd=root / 'backend', check=True)
environment = dict(os.environ, CGO_ENABLED='0', GOOS='linux', GOARCH='arm64')
subprocess.run([go, 'build', '-trimpath', '-ldflags', '-s -w', '-o', str(bundle / 'bin' / 'ncc'), './cmd/ncc'], cwd=root / 'backend', env=environment, check=True)
for name in ('run.sh', 'native-apn-status-readonly.sh', 'native-apn-network-readonly.sh', 'native-apn-same-profile-trial.sh', 'native-apn-route-recover.sh'):
    shutil.copy2(root / 'tools' / 'device' / name, bundle / 'tools' / name)
subprocess.run([sys.executable, str(root / 'tools' / 'package.py'), str(bundle)], check=True)
