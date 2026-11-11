"""Exercise the same NAS API as the UI; private targets/credentials stay in tmp.

All NAS writes are confined to the explicitly configured target directory.
This tool never changes a PC network setting or deletes an SD recording.
"""
import argparse
import importlib.util
import json
import pathlib
import subprocess
import sys
import time
import uuid

ROOT = pathlib.Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--action', choices=('configure', 'probe', 'once', 'resume', 'cancel-resume', 'worker-resume', 'automatic-once', 'ssid-gate', 'auto-on', 'auto-off', 'status'), required=True)
    parser.add_argument('--serial', help='Explicit ADB target, required only for the Web/broker restart recovery trial')
    parser.add_argument('--access-file', type=pathlib.Path, default=ROOT/'tmp/nas-test-access.json')
    parser.add_argument('--kind', choices=('video', 'event', 'both'), default='video')
    parser.add_argument('--route', choices=('wifi', 'tailscale'), default='wifi')
    parser.add_argument('--timing', choices=('realtime', 'ssid'), default='realtime')
    parser.add_argument('--url', help='Existing HTTPS management origin; pinned TLS required')
    parser.add_argument('--out', type=pathlib.Path, required=True)
    args = parser.parse_args()
    out = args.out.resolve()
    if out.exists() or not out.is_relative_to(ROOT/'tmp'):
        parser.error('A fresh directory under ignored tmp is required')
    out.mkdir(parents=True)
    spec = importlib.util.spec_from_file_location('stock_services', ROOT/'tools/stock-services.py')
    module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
    client = module.ServiceClient(args.url)
    before = client.services()
    (out/'services-before.private.json').write_text(json.dumps(before))
    evidence = {}
    def record(name, value):
        (out/(name+'.private.json')).write_text(json.dumps(value, indent=2), encoding='utf8')
        return value
    def state(): return record('state', client.call('/nas-backup'))['data']
    def wait_done(job_id, cancel_when_uploading=False):
        deadline, next_notice = time.monotonic()+1950, 0
        while time.monotonic() < deadline:
            current = state()
            job = current.get('job', {})
            if job.get('id') != job_id:
                raise RuntimeError('NAS job identity changed')
            if not job['active']:
                return job
            if cancel_when_uploading and job['progress']['stage'] == 'uploading' and int(job['progress']['bytes']) > 0:
                record('cancel', client.call('/nas-backup/jobs/'+job_id, 'DELETE'))
                cancel_when_uploading = False
                evidence['cancelRequested'] = True
            if time.monotonic() >= next_notice:
                print(json.dumps({'stage':job['status'],'bytes':job['progress']['bytes'],'total':job['progress']['totalBytes']}), flush=True)
                next_notice = time.monotonic()+15
            time.sleep(.5)
        raise RuntimeError('NAS field deadline exceeded')
    try:
        current = state()
        kinds = ('video','event') if args.kind == 'both' else (args.kind,)
        if args.action in ('configure','auto-on','auto-off'):
            policies = {}
            if args.action == 'configure':
                access = json.loads(args.access_file.read_text(encoding='utf8'))
                target = {key:access[key] for key in ('protocol','host','port','share','directory','scheme','hostKeySHA256','username','password') if key in access}
                target.setdefault('protocol','smb')
                target['directory'] = target.get('directory','').replace('\\','/').strip('/')
                for kind in kinds:
                    policies[kind] = {'route':args.route,'timing':args.timing,'ssid':access.get('ssid',''),'targets':{args.route:target}}
            else:
                policies = {kind:{'enabled':args.action=='auto-on'} for kind in kinds}
            result = client.call('/nas-backup/policies','PUT',{'expectedRevision':current['settings']['revision'],'policies':policies})
            record('saved',result)
            evidence['saved'] = True
        elif args.action == 'probe':
            for kind in kinds:
                record('probe-'+kind,client.call('/nas-backup/test','POST',{'expectedRevision':current['settings']['revision'],'kind':kind,'route':args.route}))
            evidence['connectionVerified'] = True
        elif args.action in ('automatic-once','ssid-gate'):
            if args.kind == 'both' or current['settings']['policies'][args.kind]['enabled'] or current.get('job',{}).get('active'):
                raise ValueError('Automatic trial requires one initially disabled kind and no active transfer')
            previous_id = current.get('job',{}).get('id')
            previous_policy=current['settings']['policies'][args.kind]
            trial={'enabled':True,'route':args.route,'timing':args.timing}
            if args.action=='ssid-gate':
                trial.update(timing='ssid',ssid='ncc-no-match-'+uuid.uuid4().hex[:12])
            elif args.timing=='ssid':
                access=json.loads(args.access_file.read_text(encoding='utf8'))
                if not access.get('ssid'):raise ValueError('Private access JSON must include the expected SSID')
                trial['ssid']=access['ssid']
            enabled = client.call('/nas-backup/policies','PUT',{'expectedRevision':current['settings']['revision'],'policies':{args.kind:trial}})['data']
            revision = enabled['settings']['revision']
            try:
                deadline = time.monotonic()+40
                while time.monotonic()<deadline:
                    current = state()
                    job = current.get('job',{})
                    if args.action=='ssid-gate':
                        if job.get('id')!=previous_id:raise RuntimeError('A mismatched SSID incorrectly started a transfer')
                        if current.get('automatic',{}).get(args.kind,{}).get('code')=='NAS_SSID_NOT_CONNECTED':break
                        time.sleep(.5);continue
                    if job.get('id') != previous_id and job.get('kind') == args.kind:
                        break
                    time.sleep(.5)
                else:
                    raise RuntimeError('Automatic scheduler did not start an eligible recording')
                if args.action=='ssid-gate':evidence['wrongSSIDBlocked']=True
                else:
                    job = wait_done(job['id'])
                    record('automatic-result',job)
                    if job['status'] not in ('complete','already-complete'):
                        raise RuntimeError(job.get('code') or 'Automatic transfer failed')
                    evidence.update(automaticVerified=True,bytes=job['result']['bytes'])
            finally:
                current = state()
                if current['settings']['revision'] != revision:
                    raise RuntimeError('Concurrent policy change; automatic trial cannot restore settings blindly')
                active = current.get('job',{})
                if active.get('active'):
                    if active.get('kind') != args.kind:
                        raise RuntimeError('Concurrent different-kind transfer; no cancellation attempted')
                    client.call('/nas-backup/jobs/'+active['id'],'DELETE')
                    wait_done(active['id'])
                restore={key:previous_policy[key] for key in ('route','timing','ssid')};restore['enabled']=False
                record('automatic-restored',client.call('/nas-backup/policies','PUT',{'expectedRevision':revision,'policies':{args.kind:restore}}))
                evidence['originalAutomaticOffRestored'] = True
        elif args.action == 'worker-resume':
            if not args.serial or args.kind=='both' or current.get('job',{}).get('active') or any(p['enabled'] for p in current['settings']['policies'].values()):
                raise ValueError('Worker trial requires an explicit ADB target, one kind, automatic OFF, and no active job')
            if current['settings']['policies'][args.kind]['route']!=args.route:
                raise ValueError('The selected saved route must match the requested trial route')
            catalog=client.call('/recordings')['data']
            clips=[clip for clip in catalog['clips'] if clip['kind']==('normal' if args.kind=='video' else 'event') and int(clip['sizeBytes'])>=64*1024*1024]
            if not clips:raise RuntimeError('No sufficiently large finalized recording of this kind is available')
            clip=max(clips,key=lambda item:item['startAt']);record('original',clip)
            result=client.call('/nas-backup/jobs','POST',{'expectedRevision':current['settings']['revision'],'recordingID':clip['id']})['data']
            job_id=result['job']['id'];record('started',result)
            deadline=time.monotonic()+90
            while time.monotonic()<deadline:
                job=state().get('job',{})
                if job.get('id')!=job_id or not job.get('active'):raise RuntimeError('Trial recording did not reach an active upload')
                if job['progress']['stage']=='uploading' and int(job['progress']['bytes'])>=65536:break
                time.sleep(.25)
            else:raise RuntimeError('Trial upload did not start')
            # Use the same checked-in, identity-preserving deployment action as
            # normal updates. It restarts only our Web/broker, not the NP1.
            result=subprocess.run([sys.executable,str(ROOT/'tools/deploy.py'),'--serial',args.serial,'--action','web-worker-update'],capture_output=True,timeout=210)
            (out/'worker-update.private.log').write_bytes(result.stdout+result.stderr)
            if result.returncode:raise RuntimeError('Web/broker recovery trial update failed; inspect private deployment evidence')
            client=module.ServiceClient(args.url)
            restored=state();job=restored.get('job',{});record('interrupted',job)
            if job.get('id')!=job_id or job.get('active') or job.get('status')!='interrupted':
                raise RuntimeError('Interrupted transfer was not recovered from the persistent journal')
            if restored['settings']['revision']!=current['settings']['revision'] or any(p['enabled'] for p in restored['settings']['policies'].values()):
                raise RuntimeError('NAS policy changed during the recovery trial')
            result=client.call('/nas-backup/jobs','POST',{'expectedRevision':restored['settings']['revision'],'recordingID':clip['id']})['data']
            record('resumed',result);job=wait_done(result['job']['id']);record('result',job)
            if job['status'] not in ('complete','already-complete') or int(job.get('result',{}).get('resumedBytes','0'))<=0:
                raise RuntimeError('Persistent worker-restart prefix did not resume and complete')
            evidence.update(workerRestartVerified=True,bytes=job['result']['bytes'],resumedBytes=job['result']['resumedBytes'])
        elif args.action in ('once','resume','cancel-resume'):
            if args.kind == 'both': raise ValueError('Select one recording kind for a transfer')
            catalog = client.call('/recordings')['data']
            clips = [clip for clip in catalog['clips'] if clip['kind'] == ('normal' if args.kind=='video' else 'event')]
            if args.action=='resume':
                previous=current.get('job',{})
                if previous.get('active') or previous.get('kind')!=args.kind or previous.get('route')!=args.route:
                    raise ValueError('Resume requires an inactive job of the selected kind and route')
                clips=[clip for clip in clips if clip['id']==previous.get('recordingID')]
            if not clips: raise RuntimeError('No finalized recording of this kind is available')
            clip = sorted(clips,key=lambda item:int(item['sizeBytes']),reverse=args.action=='cancel-resume')[0]
            record('original',clip)
            result = client.call('/nas-backup/jobs','POST',{'expectedRevision':current['settings']['revision'],'recordingID':clip['id']})
            record('started',result)
            job = wait_done(result['data']['job']['id'], args.action=='cancel-resume')
            record('first-result',job)
            if args.action=='cancel-resume':
                if not evidence.get('cancelRequested') or job['status'] != 'cancelled':
                    raise RuntimeError('Transfer finished before a real cancellation could be confirmed')
                result = client.call('/nas-backup/jobs','POST',{'expectedRevision':current['settings']['revision'],'recordingID':clip['id']})
                record('resumed',result)
                job = wait_done(result['data']['job']['id'])
                if int(job.get('result',{}).get('resumedBytes','0')) <= 0:
                    raise RuntimeError('No verified remote prefix was resumed')
            record('result',job)
            if job['status'] not in ('complete','already-complete'):
                raise RuntimeError(job.get('code') or 'NAS transfer did not complete')
            evidence.update(transferVerified=True, bytes=job['result']['bytes'], resumedBytes=job['result']['resumedBytes'])
        after = client.services(); record('services-after',after)
        identity = lambda rows:{row['id']:(row['state'],row['mainPID'],row['invocationID']) for row in rows}
        evidence['stockIdentitiesPreserved'] = identity(before)==identity(after)
        if not evidence['stockIdentitiesPreserved']: raise RuntimeError('Stock identity changed during test')
        evidence['passed'] = True
    except Exception as error:
        evidence.update(passed=False,errorType=type(error).__name__,reason=str(error))
        record('failure',evidence)
        print('NAS verification failed; inspect private evidence',flush=True)
        return 1
    finally:
        (out/'summary.json').write_text(json.dumps(evidence,indent=2)+'\n')
    print(json.dumps(evidence),flush=True)
    return 0


if __name__ == '__main__': raise SystemExit(main())
