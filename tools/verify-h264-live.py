"""Read original H.264 live responses and independently decode private captures.

No stock service or PC network setting is changed. Only ephemeral Web camera
sessions are created. Media, identities and raw diagnostics stay in ignored tmp.
"""
import argparse
import concurrent.futures
import importlib.util
import json
import pathlib
import subprocess
import time
import urllib.error
import urllib.request

ROOT=pathlib.Path(__file__).resolve().parents[1]

def complete_fragment_prefix(data):
    at=complete=0
    initialized=pending=False
    while at+8<=len(data):
        size=int.from_bytes(data[at:at+4],'big');kind=data[at+4:at+8]
        header=8
        if size==1:
            if at+16>len(data):break
            size=int.from_bytes(data[at+8:at+16],'big');header=16
        if size<header or size>64<<20:raise ValueError('Invalid live MP4 box')
        if at+size>len(data):break
        if kind==b'moov':initialized=True;complete=at+size
        elif kind==b'moof':
            if not initialized or pending:raise ValueError('Invalid live fragment sequence')
            pending=True
        elif kind==b'mdat':
            if not pending:raise ValueError('Media without fragment header')
            pending=False;complete=at+size
        elif kind!=b'ftyp':raise ValueError('Unexpected live MP4 box')
        at+=size
    if not initialized:raise ValueError('No live initialization segment')
    return data[:complete]


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source',choices=('front','in','both'),default='both')
    parser.add_argument('--seconds',type=int,default=12,choices=range(5,241))
    parser.add_argument('--out',type=pathlib.Path,required=True)
    parser.add_argument('--url',help='Existing HTTPS management origin, such as the device Tailscale address; pinned TLS still required')
    args=parser.parse_args()
    out=args.out.resolve()
    if not out.is_relative_to(ROOT/'tmp') or out.exists():parser.error('Fresh ignored tmp output required')
    out.mkdir(parents=True)
    spec=importlib.util.spec_from_file_location('stock_services',ROOT/'tools/stock-services.py')
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    client=module.ServiceClient(args.url)
    before=client.services();(out/'services-before.private.json').write_text(json.dumps(before))
    (out/'recordings-before.private.json').write_text(json.dumps(client.call('/recordings')))
    def one(source):
        session=client.call('/camera/sessions','POST',{'source':source,'color':'source'})['id']
        target=out/(source+'.mp4')
        try:
            until=time.monotonic()+10
            while True:
                try:
                    response=client.opener.open(client.base+'/api/v1/camera/sessions/'+session+'/h264',timeout=45)
                    break
                except urllib.error.HTTPError as error:
                    if error.code not in (429,503) or time.monotonic()>=until:raise
                    error.close();time.sleep(.5)
            started=time.monotonic();size=0
            with response, target.open('wb') as output:
                headers=dict(response.headers)
                if response.headers.get_content_type()!='video/mp4':raise RuntimeError('Unexpected live content type')
                while time.monotonic()-started<args.seconds:
                    data=response.read(32768)
                    if not data:break
                    output.write(data);size+=len(data)
            elapsed=time.monotonic()-started
            # A bounded HTTP read can stop in the middle of the last fragment.
            # Decode only complete moof/mdat pairs; source bytes are unchanged.
            target.write_bytes(complete_fragment_prefix(target.read_bytes()))
            (out/(source+'-headers.private.json')).write_text(json.dumps(headers,indent=2))
            probe=subprocess.run(['ffprobe','-v','error','-count_frames','-show_streams','-show_entries','stream=codec_type,codec_name,width,height,nb_read_frames,avg_frame_rate','-of','json',str(target)],capture_output=True,timeout=40)
            (out/(source+'-probe.private.json')).write_bytes(probe.stdout)
            (out/(source+'-probe-error.private')).write_bytes(probe.stderr)
            if probe.returncode:raise RuntimeError('Captured live media did not parse')
            streams=json.loads(probe.stdout)['streams'];video=next(value for value in streams if value['codec_type']=='video')
            decode=subprocess.run(['ffmpeg','-v','error','-i',str(target),'-map','0:v:0','-f','null','-'],capture_output=True,timeout=90)
            (out/(source+'-decode.private')).write_bytes(decode.stderr)
            frames=int(video.get('nb_read_frames','0'))
            result={'source':source,'bytes':size,'receivedSeconds':round(elapsed,2),'frames':frames,'width':video['width'],'height':video['height'],'videoOnly':len(streams)==1,'h264':video['codec_name']=='h264','decodeWithoutError':decode.returncode==0 and not decode.stderr}
            result['passed']=elapsed>=args.seconds-.1 and frames>=args.seconds*8 and result['h264'] and result['videoOnly'] and result['decodeWithoutError']
            return result
        finally:
            client.call('/camera/sessions/'+session,'DELETE')
    sources=('front','in') if args.source=='both' else (args.source,)
    results=[]
    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
        tasks={pool.submit(one,source):source for source in sources}
        for task in concurrent.futures.as_completed(tasks):
            try:results.append(task.result())
            except Exception as error:
                (out/(tasks[task]+'-error.private')).write_text(type(error).__name__+': '+str(error))
                results.append({'source':tasks[task],'passed':False,'error':type(error).__name__})
    after=client.services();(out/'services-after.private.json').write_text(json.dumps(after))
    (out/'recordings-after.private.json').write_text(json.dumps(client.call('/recordings')))
    states=lambda items:{value['id']:(value['state'],value['mainPID'],value['invocationID']) for value in items}
    report={'passed':all(value['passed'] for value in results) and states(before)==states(after),'stockIdentitiesPreserved':states(before)==states(after),'streams':sorted(results,key=lambda value:value['source'])}
    (out/'summary.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps(report))
    return 0 if report['passed'] else 1


if __name__=='__main__':raise SystemExit(main())
