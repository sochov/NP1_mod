"""Read bounded GNSS evidence; keep all positions and raw device data in tmp."""
import argparse
import collections
import importlib.util
import json
import pathlib
import re
import subprocess

ROOT=pathlib.Path(__file__).resolve().parents[1]

def summarize(raw):
    kinds=collections.Counter();rmc=collections.Counter();gga=collections.Counter();satellites=[];bad=0
    for line in raw.decode('ascii',errors='replace').splitlines():
        line=line.strip()
        if not line.startswith('$') or '*' not in line:continue
        text,checksum=line[1:].rsplit('*',1)
        actual=0
        for byte in text.encode('ascii',errors='replace'):actual^=byte
        try:valid=actual==int(checksum[:2],16)
        except ValueError:valid=False
        if not valid:bad+=1;continue
        fields=text.split(',');kind=fields[0][-3:];kinds[kind]+=1
        if kind=='RMC' and len(fields)>2:rmc[fields[2] or 'empty']+=1
        if kind=='GGA' and len(fields)>7:
            gga[fields[6] or 'empty']+=1
            if fields[7].isdigit():satellites.append(int(fields[7]))
    return {'sentences':dict(kinds),'rmcStatus':dict(rmc),'ggaQuality':dict(gga),'maxUsedSatellites':max(satellites,default=None),'badChecksums':bad}

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--serial',required=True);parser.add_argument('--url');parser.add_argument('--out',type=pathlib.Path,required=True)
    args=parser.parse_args();out=args.out.resolve()
    if not re.fullmatch(r'[A-Za-z0-9._:-]+',args.serial) or out.exists() or not out.is_relative_to(ROOT/'tmp'):parser.error('Explicit target and fresh ignored tmp output required')
    out.mkdir(parents=True)
    adb=['adb','-s',args.serial,'exec-out']
    def read(*command,input=None):
        result=subprocess.run(adb+list(command),input=input,capture_output=True,timeout=20)
        if result.returncode:raise RuntimeError('Read-only device command failed')
        return result.stdout
    directory='/mnt/sdcard/DRIVERECDATA/Video/'
    entries=read('ls','-1t',directory).decode().splitlines()
    files=[directory+name.strip() for name in entries if re.fullmatch(r'FILE[ME]_[0-9]{6}_[0-9]{6}_[0-9]+\.NMEA',name.strip())][:4]
    reports=[]
    for index,name in enumerate(files):
        name=name.strip()
        if not re.fullmatch(r'/mnt/sdcard/DRIVERECDATA/Video/FILE[ME]_[0-9]{6}_[0-9]{6}_[0-9]+\.NMEA',name):raise RuntimeError('Unexpected sidecar path')
        raw=read('head','-c','2097152',name);(out/(str(index)+'.NMEA.private')).write_bytes(raw)
        reports.append(summarize(raw))
    units=['loc_launcher.service','location_hal_daemon.service','app-@System.service','app-@DriveRec.service','NEventWatcherSys.service']
    services=[]
    for index,unit in enumerate(units):
        raw=read('systemctl','show',unit,'-p','Id','-p','MainPID','-p','ActiveState')
        (out/('service-'+str(index)+'.private')).write_bytes(raw)
        values=dict(line.split('=',1) for line in raw.decode().splitlines() if '=' in line)
        pid=values.get('MainPID','0').strip();threads=None
        if pid.isdigit() and int(pid)>1:
            status=read('cat','/proc/'+pid+'/status');(out/('process-'+str(index)+'.private')).write_bytes(status)
            for line in status.decode().splitlines():
                if line.startswith('Threads:'):threads=int(line.split(':',1)[1])
        services.append({'unit':unit,'state':values.get('ActiveState'),'threads':threads})
    (out/'location-journal.private').write_bytes(read('journalctl','-u','loc_launcher.service','-u','location_hal_daemon.service','-n','120','--no-pager'))
    spec=importlib.util.spec_from_file_location('stock',ROOT/'tools/stock-services.py');module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    gps=module.ServiceClient(args.url).call('/device/gps');(out/'gps-api.private.json').write_text(json.dumps(gps),encoding='utf8')
    summary={'sidecars':reports,'services':services,'apiStatus':gps.get('status'),'apiFix':gps.get('data',{}).get('fix') if isinstance(gps.get('data'),dict) else None}
    (out/'summary.json').write_text(json.dumps(summary,indent=2)+'\n');print(json.dumps(summary))

if __name__=='__main__':main()
