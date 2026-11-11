"""Read small MP4/index metadata only; raw paths/headers stay in ignored tmp."""
import argparse
import ast
import json
import pathlib
import re
import subprocess
import time
import uuid

ROOT=pathlib.Path(__file__).resolve().parents[1]

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--serial',required=True)
    args=parser.parse_args()
    if not re.fullmatch(r'[A-Za-z0-9._:-]+',args.serial):
        parser.error('invalid serial')
    evidence=ROOT/'tmp/native-recording-layout'/(time.strftime('%Y%m%d-%H%M%S')+'-'+uuid.uuid4().hex[:8])
    evidence.mkdir(parents=True)
    remote='/tmp/ncc-recording-inspect-'+uuid.uuid4().hex+'.py'
    adb=['adb','-s',args.serial]
    for index,command in enumerate((['push',str(ROOT/'tools/device/inspect-recording-layout.py'),remote],['shell','python3',remote])):
        result=subprocess.run(adb+command,stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
        (evidence/('transport-'+str(index)+'.txt')).write_bytes(result.stdout)
        if result.returncode:
            raise SystemExit('Read-only transport failed; inspect private evidence '+str(evidence))
    actual=ast.literal_eval(result.stdout.decode('utf8').strip())
    (evidence/'layout.private.json').write_text(json.dumps(actual,indent=2),encoding='utf8')
    public={'mp4Samples':len(actual['mp4']),'writingMP4Observed':any(item.get('sizeAfter',0)>item['size'] for item in actual['mp4']),
        'indexExtensionPresent':any(key in actual['extensions'] for key in ('.moov','.idx','.index','.sps','.pps','.avc','.h264')),
        'truncated':actual['truncated']}
    print(json.dumps(public))
    print('Private evidence: '+str(evidence))

if __name__=='__main__':
    main()
