"""Fixed read-only post-transaction checks; private output, no response bodies printed."""
import os
import signal
import subprocess

BASE='/run/np1-native-apn-observe'
if os.geteuid()!=0:
    raise SystemExit('Root required')
if os.path.islink(BASE):
    raise SystemExit('Observe directory invalid')
os.makedirs(BASE,mode=0o700,exist_ok=True)
os.chmod(BASE,0o700)
os.umask(0o077)
active=None
def stop_child(signum=None,frame=None):
    if active is not None and active.poll() is None:
        active.kill();active.wait(timeout=5)
    if signum is not None:
        raise SystemExit('Read-only observation interrupted')
for number in (signal.SIGHUP,signal.SIGINT,signal.SIGTERM):
    signal.signal(number,stop_child)
def run(name,args,timeout=8):
    global active
    with open(BASE+'/'+name+'.txt','w') as output:
        try:
            active=subprocess.Popen(args,stdout=output,stderr=output)
        except OSError:
            return False
        try:
            result=active.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            stop_child();return False
    return result==0
run('routes',['/sbin/ip','-4','route','show','table','all'])
run('addresses',['/sbin/ip','-o','-4','address','show'])
run('rules',['/sbin/ip','-4','rule','show'])
run('firewall',['/usr/sbin/iptables-save'])
run('resolver',['/bin/cat','/etc/resolv.conf'])
print('cm_process_present='+str(run('cm-process',['/bin/pidof','quectel-CM'])))
print('loopback_http='+str(run('loopback-http',['/usr/bin/wget','-q','-O','/dev/null','-T','5','http://127.0.0.1/'])))
print('ap_address_http='+str(run('ap-http',['/usr/bin/wget','-q','-O','/dev/null','-T','5','http://192.168.226.1/'])))
print('external_https='+str(run('external-https',['/usr/bin/wget','-q','-O','/dev/null','-T','5','https://example.com/'])))
print('summary_captured='+str(run('summary-fetch',['/usr/bin/wget','-q','-O',BASE+'/summary.json','-T','5','http://127.0.0.1/api/v1/summary'])))
print('tailscale_captured='+str(run('tailscale',['/run/np1-tailscale/tailscale','--socket=/run/np1-tailscale/tailscaled.sock','status','--json'])))
print('Read-only evidence saved privately; no network mutation')
