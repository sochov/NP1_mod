"""Fixed read-only service/boot capture; stable stopped consumers are respected."""
import json
import contextlib
import pathlib
import re
import subprocess

ROOT = pathlib.Path(__file__).resolve().parents[1]
APPS = ('DriveRec','ImageRecog','Voice','Navigation','Communication','Setting','Update','RemoteMonitoring',
        'VirtualPassenger','InfoMtgEngine','SitAwrEng','Opening','Misc','System')
UNITS = frozenset(tuple('app-@'+name+'.service' for name in APPS) + (
    'app-AppManager.service','qmmf-server.service','NEventWatcherAV.service','NEventWatcherCom.service',
    'NEventWatcherITS.service','NEventWatcherSys.service','QCMAP_ConnectionManagerd.service','loc_launcher.service',
    'location_hal_daemon.service','pulseaudio.service','NPSystemMonitor.service','socketserver.service',
    'np1-control.service','np1-control-broker.service','np1-control-ap.service','np1-control-modem.service',
    'np1-control-apn.service','np1-control-led.service','np1-control-native.service'))


def parse(text):
    blocks = re.split(r'\n\s*\n', text.replace('\r','').strip())
    if not blocks or not re.fullmatch(r'NCC_BOOT=[a-f0-9-]{36}', blocks[0]):
        raise ValueError('boot identity unavailable')
    result = {'boot': blocks[0].split('=',1)[1], 'units': {}}
    for block in blocks[1:]:
        fields = {}
        for line in block.splitlines():
            key, separator, value = line.partition('=')
            if not separator or key in fields:
                raise ValueError('ambiguous service record')
            fields[key] = value
        unit = fields.get('Id')
        if unit not in UNITS or unit in result['units']:
            raise ValueError('unknown or duplicate service')
        if fields.get('LoadState') != 'loaded' or fields.get('Job') != '' or 'Job' not in fields:
            raise ValueError('service unavailable or operation in progress')
        pid = fields.get('MainPID','')
        state = fields.get('ActiveState')
        invocation = fields.get('InvocationID','')
        if state == 'active':
            if not re.fullmatch(r'[1-9][0-9]*',pid) or not re.fullmatch(r'[a-f0-9]{32}',invocation) or fields.get('SubState') != 'running':
                raise ValueError('active service has no stable process')
        elif state == 'inactive':
            if pid != '0' or fields.get('SubState') != 'dead' or (invocation and not re.fullmatch(r'[a-f0-9]{32}',invocation)):
                raise ValueError('stopped service is not stable')
        else:
            raise ValueError('failed or transitional service')
        result['units'][unit] = fields
    if set(result['units']) != UNITS:
        raise ValueError('service inventory incomplete')
    return result


def require_setting_owner(snapshot):
    owner = snapshot['units']['app-@Setting.service']
    if owner['ActiveState'] != 'active' or owner['MainPID'] == '0':
        raise ValueError('Setting is intentionally stopped; no test write or start is allowed')


def capture(serial, directory, phase):
    if not re.fullmatch(r'[A-Za-z0-9._:-]+',serial):
        raise ValueError('invalid serial')
    script = (ROOT/'tools/device/native-web-baseline.sh').read_text(encoding='utf8')
    result = subprocess.run(['adb','-s',serial,'shell',script],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=20)
    (directory/(phase+'-services.txt')).write_bytes(result.stdout)
    if result.returncode:
        raise ValueError('read-only service transport failed')
    snapshot = parse(result.stdout.decode('utf8',errors='replace'))
    (directory/(phase+'-services.json')).write_text(json.dumps(snapshot,indent=2),encoding='utf8')
    return snapshot


def unchanged(before, after):
    if before != after:
        raise ValueError('service PID/Invocation/state or boot changed; no service repair is permitted')


@contextlib.contextmanager
def baseline(serial, directory):
    directory.mkdir(parents=True, mode=0o700, exist_ok=False)
    before = capture(serial, directory, 'before')
    try:
        require_setting_owner(before)
        yield before
    finally:
        after = capture(serial, directory, 'after')
        unchanged(before, after)
