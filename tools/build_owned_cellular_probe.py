"""Build the isolated own-data prototype. Does not install or execute it."""
import os
import pathlib
import shutil
import subprocess
import sys

root = pathlib.Path(__file__).resolve().parents[1]
bundle = root / 'artifacts/ncc-owned-cellular-probe'
(bundle / 'bin').mkdir(parents=True, exist_ok=True)
(bundle / 'tools').mkdir(exist_ok=True)
go = root / 'resources/toolchain/go/bin' / ('go.exe' if os.name == 'nt' else 'go')
env = dict(os.environ, GOOS='linux', GOARCH='arm64', CGO_ENABLED='0')
subprocess.run([str(go), 'build', '-trimpath', '-ldflags=-s -w', '-o', str(bundle / 'bin/ncc'), './cmd/ncc'], cwd=root / 'backend', env=env, check=True)
for name in ('owned-cellular-preflight.sh', 'owned-cellular-session-probe.sh'):
    shutil.copy2(root / 'tools/device' / name, bundle / 'tools')
subprocess.run([sys.executable, str(root / 'tools/package.py'), str(bundle)], check=True)
print('Isolated owned-cellular metadata/session prototype built; no device operation performed')
