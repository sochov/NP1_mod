"""Build only our fixed read-only profile helper bundle, without touching the UI bundle."""
import argparse
import os
import pathlib
import shutil
import subprocess
import sys

parser=argparse.ArgumentParser()
parser.add_argument('--go',default='go')
args=parser.parse_args()
root=pathlib.Path(__file__).resolve().parent.parent
go=shutil.which(args.go) or str(pathlib.Path(args.go).resolve())
bundle=root/'artifacts'/'ncc-profiles-readonly'
(bundle/'bin').mkdir(parents=True,exist_ok=True)
(bundle/'tools').mkdir(exist_ok=True)
subprocess.run([go,'test','./cmd/ncc','./internal/modem'],cwd=root/'backend',check=True)
environment=dict(os.environ,CGO_ENABLED='0',GOOS='linux',GOARCH='arm64')
subprocess.run([go,'build','-trimpath','-ldflags','-s -w','-o',str(bundle/'bin'/'ncc'),'./cmd/ncc'],cwd=root/'backend',env=environment,check=True)
for name in ('run.sh','gobinet-profiles-readonly.sh','gobinet-profile-list-readonly.sh','apn-editor-seed.sh'):
    shutil.copy2(root/'tools'/'device'/name,bundle/'tools'/name)
subprocess.run([sys.executable,str(root/'tools'/'package.py'),str(bundle)],check=True)
