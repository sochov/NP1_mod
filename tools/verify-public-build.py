"""Build only the Git index in an isolated directory, without private resources."""
import argparse
import json
import os
import pathlib
import shutil
import subprocess
import tempfile

parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--go',default=shutil.which('go'))
args=parser.parse_args()
if not args.go:raise SystemExit('Go not found; pass --go PATH')
root=pathlib.Path(__file__).resolve().parents[1]
go=str(pathlib.Path(args.go).resolve())
destination=pathlib.Path(tempfile.mkdtemp(prefix='public-build-',dir=root/'tmp'))
tree=subprocess.check_output(['git','write-tree'],cwd=root,text=True).strip()
subprocess.run(['git','checkout-index','--all','--prefix='+destination.as_posix()+'/'],cwd=root,check=True)
environment=dict(os.environ)
environment['PATH']=str(pathlib.Path(go).parent)+os.pathsep+environment['PATH']
# Do not accidentally stamp the outer working repository into this source copy.
environment['GIT_CEILING_DIRECTORIES']=str(destination.parent)
if os.name=='nt':
    command=['powershell','-NoProfile','-ExecutionPolicy','Bypass','-File',str(destination/'tools/build.ps1'),'-Go',go]
else:
    command=['sh',str(destination/'tools/build.sh')]
result=subprocess.run(command,cwd=destination,env=environment)
(root/'tmp/public-build-result.json').write_text(json.dumps({'tree':tree,'directory':str(destination),'exitCode':result.returncode},indent=2)+'\n')
if result.returncode:raise SystemExit(result.returncode)
print('PASS: staged public source builds without private documentation, captured firmware, credentials, or prebuilt helpers')
