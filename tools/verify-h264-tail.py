"""Remux an existing private stock recording and independently verify AVC reuse.

This is a host-only test: no ADB, camera, encoder, or device mutation. Generated
media and ffprobe packet hashes are saved only under ignored tmp evidence.
"""
import argparse
import json
import os
import pathlib
import shutil
import subprocess
import time
import uuid

ROOT=pathlib.Path(__file__).resolve().parents[1]

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--recording',type=pathlib.Path,required=True)
    args=parser.parse_args()
    source=args.recording.resolve(strict=True)
    if not source.is_file():
        parser.error('private recording must be a regular file')
    evidence=ROOT/'tmp/h264-tail-verification'/(time.strftime('%Y%m%d-%H%M%S')+'-'+uuid.uuid4().hex[:8])
    evidence.mkdir(parents=True)
    output=evidence/'reused-avc.mp4'
    go=ROOT/'resources/toolchain/go/bin'/('go.exe' if os.name=='nt' else 'go')
    if not go.is_file():
        found=shutil.which('go')
        if not found:
            raise SystemExit('Go toolchain unavailable')
        go=pathlib.Path(found)
    env=dict(os.environ,NCC_H264_PRIVATE_FIXTURE=str(source),NCC_H264_PRIVATE_OUTPUT=str(output))
    result=subprocess.run([str(go),'test','./internal/h264tail','-run','^TestPrivateRecordingRoundTrip$','-count=1','-v'],cwd=ROOT/'backend',env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
    (evidence/'go-private.txt').write_bytes(result.stdout)
    if result.returncode:
        raise SystemExit('Remux test failed; private evidence '+str(evidence))
    packets=[]
    for filename,label in ((source,'original'),(output,'reused')):
        result=subprocess.run(['ffprobe','-v','error','-select_streams','v:0','-show_packets','-show_data_hash','sha256','-show_entries','packet=data_hash','-of','json',str(filename)],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        (evidence/(label+'-packets.private.json')).write_bytes(result.stdout)
        (evidence/(label+'-probe-error.private.txt')).write_bytes(result.stderr)
        if result.returncode:
            raise SystemExit('Independent packet extraction failed; private evidence '+str(evidence))
        packets.append([item['data_hash'] for item in json.loads(result.stdout)['packets']])
    decoded=subprocess.run(['ffmpeg','-v','error','-i',str(output),'-map','0:v:0','-f','null','-'],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
    (evidence/'decode.private.txt').write_bytes(decoded.stderr)
    metadata=subprocess.run(['ffprobe','-v','error','-show_streams','-show_entries','stream=codec_type,codec_name','-of','json',str(output)],stdout=subprocess.PIPE,stderr=subprocess.PIPE,check=True)
    streams=json.loads(metadata.stdout)['streams']
    report={'videoPackets':len(packets[1]),'compressedPacketsIdentical':packets[0]==packets[1] and bool(packets[0]),'completeDecodeWithoutError':decoded.returncode==0 and not decoded.stderr,'videoOnly':len(streams)==1 and streams[0]['codec_type']=='video' and streams[0]['codec_name']=='h264'}
    (evidence/'verification.json').write_text(json.dumps(report,indent=2)+'\n',encoding='utf8')
    print(json.dumps(report))
    print('Private evidence: '+str(evidence))
    if not all(report[key] for key in ('compressedPacketsIdentical','completeDecodeWithoutError','videoOnly')):
        raise SystemExit('Independent AVC reuse verification failed')

if __name__=='__main__':
    main()
