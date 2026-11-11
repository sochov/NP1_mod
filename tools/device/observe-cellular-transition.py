"""Observe a fixed short STA/4G transition without changing a connection."""
import os
import signal
import subprocess
import time

BASE='/run/np1-cellular-transition-observe'
if os.geteuid()!=0 or os.path.islink(BASE):
    raise SystemExit('Observation requires root-owned runtime')
os.umask(0o077)
os.makedirs(BASE,mode=0o700,exist_ok=True)
os.chmod(BASE,0o700)
stop=False
child=None
def stopping(signum,frame):
    global stop
    stop=True
    if child is not None and child.poll() is None:
        child.kill();child.wait(timeout=3)
for number in (signal.SIGHUP,signal.SIGINT,signal.SIGTERM):
    signal.signal(number,stopping)
def command(args,timeout=2):
    global child
    child=subprocess.Popen(args,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
    try:
        raw=child.communicate(timeout=timeout)[0]
    except subprocess.TimeoutExpired:
        child.kill();raw=child.communicate(timeout=3)[0]
    return child.returncode,raw.decode('utf-8','replace')
def save(path,raw):
    with open(BASE+'/'+path,'w') as output:
        output.write(raw)
known=set()
tested=False
until=time.monotonic()+150
index=0
print('Read-only cellular transition observation started',flush=True)
while not stop and time.monotonic()<until:
    stamp=time.time()
    _,routes=command(['/sbin/ip','-4','route','show','table','main','default'])
    _,addresses=command(['/sbin/ip','-o','-4','address','show'])
    _,pids=command(['pidof','quectel-CM'])
    save('%04d-routes.txt'%index,str(stamp)+'\n'+routes)
    save('%04d-addresses.txt'%index,str(stamp)+'\n'+addresses)
    save('%04d-cm.txt'%index,str(stamp)+'\n'+pids)
    for pid in pids.split():
        if not pid.isdigit() or pid in known:
            continue
        known.add(pid)
        for leaf in ('cmdline','status','stat'):
            try:
                with open('/proc/'+pid+'/'+leaf,'rb') as source:
                    data=source.read(65536)
                with open(BASE+'/cm-'+pid+'-'+leaf+'.private','wb') as output:
                    output.write(data)
            except OSError:
                pass
        _,digest=command(['sha256sum','/proc/'+pid+'/exe'])
        save('cm-'+pid+'-digest.private',digest)
        try:
            save('cm-'+pid+'-exe.private',os.readlink('/proc/'+pid+'/exe'))
            for fd in os.listdir('/proc/'+pid+'/fd'):
                try:
                    target=os.readlink('/proc/'+pid+'/fd/'+fd)
                    if target=='/dev/qcqmi0':
                        save('cm-'+pid+'-qmi-owned.private','verified-qcqmi0-descriptor-present\n')
                except OSError:
                    pass
        except OSError:
            pass
    cellular_default=any(line.startswith('default ') and ('dev usb0 ' in line+' ' or 'dev usb0.1 ' in line+' ' or 'dev usb0.2 ' in line+' ') for line in routes.splitlines())
    if cellular_default and not tested:
        tested=True
        try:
            with open('/etc/resolv.conf') as source:save('cellular-resolver.private',source.read(16384))
        except OSError:
            pass
        result,output=command(['/usr/bin/wget','-q','-O','/dev/null','-T','3','https://example.com/'],timeout=5)
        save('cellular-https.private',str(stamp)+'\nexit='+str(result)+'\n'+output)
        print('Cellular default observed; fixed HTTPS test recorded privately',flush=True)
    index+=1
    time.sleep(0.5)
print('Read-only transition observation finished; samples='+str(index),flush=True)
