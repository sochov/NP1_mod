"""Stage a fixed start-barrier helper and run only preflight or one Notify.

Raw process identities and logs remain in ignored tmp evidence. No service action.
"""
import argparse
import hashlib
import json
import pathlib
import re
import shutil
import subprocess
import time
import uuid

ROOT=pathlib.Path(__file__).resolve().parents[1]
IDS=('drive-rec','image-recog','system','remote-monitoring','virtual-passenger')

def parse_arguments(argv=None):
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--serial',required=True)
    parser.add_argument('--action',choices=('preflight','notify'),default='preflight')
    parser.add_argument('--id',choices=IDS,required=True)
    args=parser.parse_args(argv)
    if not re.fullmatch(r'[A-Za-z0-9._:-]+',args.serial):
        parser.error('invalid serial')
    return args

def main():
    args=parse_arguments()
    material=ROOT/'artifacts/stock-start-helper'
    helper=material/'ncc-stock-start-helper'
    info=json.loads((material/'build-info.json').read_text(encoding='utf8'))
    if hashlib.sha256(helper.read_bytes()).hexdigest()!=info['sha256']:
        raise SystemExit('Build helper first; hash mismatch')
    bundle=ROOT/'artifacts/stock-start-stage'
    files={'bin/ncc-stock-start-helper':helper,'build-info.json':material/'build-info.json','tools/stock-start-trial.sh':ROOT/'tools/device/stock-start-trial.sh','tools/native-web-baseline.sh':ROOT/'tools/device/native-web-baseline.sh'}
    for name,source in files.items():
        target=bundle/name
        target.parent.mkdir(parents=True,exist_ok=True)
        shutil.copyfile(source,target)
    if any(p.is_file() and p.relative_to(bundle).as_posix() not in set(files)|{'SHA256SUMS'} for p in bundle.rglob('*')):
        raise SystemExit('Unexpected stage file')
    (bundle/'SHA256SUMS').write_text(''.join(hashlib.sha256((bundle/name).read_bytes()).hexdigest()+'  '+name+'\n' for name in files),encoding='ascii',newline='\n')
    token=time.strftime('%Y%m%d-%H%M%S')+'-'+uuid.uuid4().hex[:8]
    evidence=ROOT/'tmp/stock-start-evidence'/token
    evidence.mkdir(parents=True)
    remote='/tmp/np1-stock-start-stage.'+uuid.uuid4().hex
    adb=['adb','-s',args.serial]
    def run(arguments):
        proc=subprocess.Popen(adb+arguments,stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
        while True:
            try:
                raw,_=proc.communicate(timeout=30)
                break
            except subprocess.TimeoutExpired:
                print('Waiting for controlled helper; never killing vendor code.',flush=True)
        with (evidence/'transport.txt').open('ab') as log:
            log.write(raw)
        if proc.returncode:
            raise SystemExit('ADB transport failed; inspect private evidence '+str(evidence))
        return raw
    run(['push',str(bundle)+'/.',remote])
    raw=run(['shell','/bin/sh',remote+'/tools/stock-start-trial.sh',args.action,args.id])
    match=re.search(rb'Private stock start trial records: (/run/np1-stock-start-trial\.[A-Za-z0-9]+)',raw)
    if not match:
        raise SystemExit('Trial did not create complete records; inspect '+str(evidence))
    path=match.group(1).decode('ascii')
    run(['pull',path,str(evidence)])
    actual=evidence/pathlib.PurePosixPath(path).name
    if (actual/'helper-sha256.txt').read_text(encoding='ascii').split()[0]!=info['sha256']:
        raise SystemExit('Executed helper hash mismatch')
    if (actual/'stock-before.txt').read_bytes()!=(actual/'stock-after.txt').read_bytes():
        raise SystemExit('Stock/own processes or boot changed; inspect '+str(evidence))
    if not re.search(rb'NCC_EXIT_STATUS=0\s*$',raw):
        raise SystemExit('Trial failed or incomplete; inspect '+str(evidence))
    expected='start-barrier=waiting; owner=matched; vendor-not-loaded\n'
    if (actual/'preflight.txt').read_text(encoding='ascii')!=expected:
        raise SystemExit('Target was not at the unnotified start barrier; inspect '+str(evidence))
    if args.action=='notify' and (actual/'notify.txt').read_text(encoding='ascii')!='start-barrier=notified; owner=matched; threads=multiple; cleanup=matched\n':
        raise SystemExit('Barrier release proof missing')
    print('PASS: '+args.action+'; service/boot baseline unchanged; private evidence '+str(evidence))

if __name__=='__main__':
    main()
