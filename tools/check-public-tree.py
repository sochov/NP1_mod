"""Check tracked and candidate source files before publishing (no secret output)."""
import argparse
import json
import pathlib
import subprocess
import sys

parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--known-private-values',type=pathlib.Path,
                    help='Ignored local JSON array of individual identifiers or secrets')
args=parser.parse_args()
root=pathlib.Path(__file__).resolve().parents[1]
paths=subprocess.check_output(['git','ls-files','--cached','--others','--exclude-standard','-z'],cwd=root).decode().split('\0')
values=[]
if args.known_private_values:
    values=json.loads(args.known_private_values.read_text(encoding='utf8'))
    if not isinstance(values,list) or not all(isinstance(v,str) and len(v)>=6 for v in values):
        raise SystemExit('Private values must be a JSON array of strings (at least six characters)')
failures=[]
for relative in sorted(set(filter(None,paths))):
    path=root/relative
    private=relative.startswith(('docs_private/','tmp/')) or (
        relative.startswith(('resources/','artifacts/')) and not relative.endswith('/.gitkeep'))
    private=private or relative=='docs/NP1-root-ADB-application-development-guide-20261111.md'
    if private:
        failures.append((relative,'private path'));continue
    if path.suffix.lower() in {'.pem','.key','.pfx','.mp4','.dat','.so','.dump'}:
        failures.append((relative,'private material or unreviewed binary'));continue
    if not path.is_file():continue
    data=path.read_bytes()
    for value in values:
        if value.encode() in data:
            failures.append((relative,'known private value'));break
for path,reason in failures:
    print(f'FAIL {path}: {reason}')
if failures:sys.exit(1)
print(f'PASS: {len(set(filter(None,paths)))} public source paths; private materials excluded; known values checked without disclosure')
