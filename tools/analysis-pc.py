"""Run a checked-in read-only inventory on the user-provided analysis PC.

Load credentials and a host-specific TOFU key from ignored tmp only. Inventory
is sent on stdin, never copied onto its low-space internal storage. No Wi-Fi or
Bluetooth configuration is changed. Output remains private beneath tmp.
"""
import argparse
import datetime
import hashlib
import json
import pathlib
import time

import paramiko

ROOT = pathlib.Path(__file__).resolve().parents[1]


def connect(config_path):
    config_path = pathlib.Path(config_path).resolve()
    if ROOT / 'tmp' not in config_path.parents:
        raise ValueError('SSH credentials must stay in ignored tmp')
    config = json.loads(config_path.read_text(encoding='utf8'))
    known = ROOT / 'tmp/analysis-pc-known-hosts'
    client = paramiko.SSHClient()
    if known.exists():
        client.load_host_keys(str(known))
    class FirstHostKey(paramiko.MissingHostKeyPolicy):
        def missing_host_key(self, session, hostname, key):
            expected = config['host'] if config.get('port',22) == 22 else '[{}]:{}'.format(config['host'],config['port'])
            if hostname != expected:
                raise paramiko.SSHException('Unexpected SSH host')
            session.get_host_keys().add(hostname, key.get_name(), key)
            session.save_host_keys(str(known))
            (ROOT/'tmp/analysis-pc-host-key.json').write_text(json.dumps({'type':key.get_name(),'sha256':hashlib.sha256(key.asbytes()).hexdigest()},indent=2)+'\n')
    client.set_missing_host_key_policy(FirstHostKey())
    client.connect(config['host'], port=config.get('port',22), username=config['username'], password=config['password'],
                   timeout=10, auth_timeout=10, banner_timeout=10, look_for_keys=False, allow_agent=False)
    return client


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--access',type=pathlib.Path,default=ROOT/'tmp/analysis-pc-access.json')
    parser.add_argument('--out',type=pathlib.Path,required=True)
    args=parser.parse_args()
    out=args.out.resolve()
    if ROOT/'tmp' not in out.parents or out.exists():
        parser.error('fresh ignored tmp output directory required')
    out.mkdir(parents=True)
    client=connect(args.access)
    try:
        script=(ROOT/'tools/remote/analysis-pc-inventory.sh').read_bytes()
        stdin,stdout,stderr=client.exec_command('/bin/sh -s',timeout=45)
        stdin.write(script.decode('utf8'));stdin.channel.shutdown_write()
        (out/'inventory.txt').write_bytes(stdout.read())
        (out/'stderr.txt').write_bytes(stderr.read())
        status=stdout.channel.recv_exit_status()
        (out/'summary.json').write_text(json.dumps({'connected':True,'inventoryExitCode':status,'readOnly':True,'remoteFilesCreated':False,'at':datetime.datetime.now(datetime.timezone.utc).isoformat()},indent=2)+'\n')
        if status:
            raise SystemExit('Remote inventory incomplete; inspect private records')
        print('Connected; read-only interface/storage inventory saved privately, no remote files created')
    finally:
        client.close()


if __name__=='__main__':
    main()
