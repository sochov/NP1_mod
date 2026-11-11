"""Host filesystem/systemctl fixtures. No device, vendor code or real service actions.

Git Bash on Windows cannot model Linux socket types/flock ownership. The fixture
substitutes a regular readiness token and mock locks; field verification still
checks the actual socket and kernel locks on NP1.
"""
import json
import os
import pathlib
import shutil
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]
BASH = r'C:\Program Files\Git\bin\bash.exe' if pathlib.Path(r'C:\Program Files\Git\bin\bash.exe').exists() else shutil.which('bash')
MOCK = r'''#!/usr/bin/env python
import json,os,pathlib,sys
root=pathlib.Path(os.environ['NCC_UPDATE_FIXTURE'])
name=pathlib.Path(sys.argv[0]).name
args=sys.argv[1:]
if name=='id':print('0');sys.exit(0)
if name=='sleep':sys.exit(0)
if name=='flock':sys.exit(75 if os.environ.get('NCC_UPDATE_BUSY')=='1' and args==['-n','9'] else 0)
with (root/'commands.jsonl').open('a')as f:f.write(json.dumps(args)+'\n')
state=root/'counter';counter=int(state.read_text())
if 'daemon-reload'in args:sys.exit(0)
if 'restart'in args:
 if args!=['--no-block','restart','np1-control-native.service']:sys.exit(90)
 counter+=1;state.write_text(str(counter))
 runtime=root/'run-native'
 for p in runtime.iterdir():
  if p.name!='control.lock':p.unlink()
 shutil_map={'ncc-native-setting-helper':'helper','ncc-native-audio-helper':'audio-helper'}
 (runtime/'ncc').write_bytes((root/'base/bin/ncc').read_bytes())
 for source,target in shutil_map.items():
  p=root/'base/experimental'/source
  if p.exists():(runtime/target).write_bytes(p.read_bytes())
 (runtime/'control.sock').touch()
 sys.exit(0)
if not args or args[0]!='show':sys.exit(91)
units=[a for a in args if a.endswith('.service')]
native=units==['np1-control-native.service']
failed=native and counter==1 and os.environ.get('NCC_UPDATE_FAIL_ONCE')=='1'
pid=0 if failed else 100+counter if native else 500
active='failed'if failed else'active'
inv=('%032x'%(counter+1))if native else 'b'*32
if '--value'in args:
 prop=args[args.index('-p')+1]
 print({'ActiveState':active,'MainPID':str(pid),'InvocationID':inv}[prop]);sys.exit(0)
if native:
 print('MainPID='+str(pid)+'\nInvocationID='+inv);sys.exit(0)
for unit in units:
 actual=501 if counter and os.environ.get('NCC_UPDATE_DRIFT')=='1'and unit=='app-@Voice.service'else 500
 print('Id='+unit+'\nMainPID='+str(actual)+'\nInvocationID='+inv+'\nActiveState=active\nSubState=running\n')
'''


@unittest.skipUnless(pathlib.Path(BASH).exists() or shutil.which(BASH), 'Bash required for shell fixture')
class NativeWorkerUpdateTests(unittest.TestCase):
    def run_fixture(self, scenario='success'):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        root = pathlib.Path(temp.name)
        for directory in ('base/bin','base/experimental','base/recovery','run-native','etc','stage/bin','stage/systemd','stage/experimental','stage/tools','mock'):
            (root/directory).mkdir(parents=True,exist_ok=True)
        originals={'base/bin/ncc':b'#!/bin/sh\n# old fixture\nexit 0\n','base/experimental/ncc-native-setting-helper':b'original-helper','etc/native.service':b'original-unit'}
        inputs={'bin/ncc':b'#!/bin/sh\n# new fixture\nexit 0\n','systemd/np1-control-native.service':b'new-native-unit','experimental/ncc-native-setting-helper':b'new-helper','experimental/ncc-native-audio-helper':b'new-audio'}
        for name,data in originals.items():(root/name).write_bytes(data)
        for name,data in inputs.items():(root/'stage'/name).write_bytes(data)
        import hashlib
        (root/'stage/SHA256SUMS').write_text(''.join(hashlib.sha256(data).hexdigest()+'  '+name+'\n'for name,data in inputs.items()),encoding='ascii',newline='\n')
        (root/'counter').write_text('0')
        (root/'boot').write_text('synthetic-fixed-boot\n')
        (root/'run-native/control.sock').touch()
        for name in ('systemctl','id','sleep','flock'):
            p=root/'mock'/name;p.write_text(MOCK,encoding='utf8',newline='\n');p.chmod(0o755)
        script=(ROOT/'tools/device/native-worker-update.sh').read_text(encoding='utf8')
        replacements={'BASE=/overlay/owner-apps/np1-control':'BASE='+ (root/'base').as_posix(),
                      'RUN=/run/np1-control-native':'RUN='+ (root/'run-native').as_posix(),
                      'UNIT=/etc/systemd/system/np1-control-native.service':'UNIT='+ (root/'etc/native.service').as_posix(),
                      'UPDATE_LOCK=/run/np1-native-control-update.lock':'UPDATE_LOCK='+ (root/'update.lock').as_posix(),
                      '/proc/sys/kernel/random/boot_id':(root/'boot').as_posix(),
                      'test -S "$RUN/control.sock"':'test -f "$RUN/control.sock"'}
        for before,after in replacements.items():
            self.assertIn(before,script);script=script.replace(before,after)
        script=script.replace('set -eu','set -eu\nPATH="$(CDPATH= cd -- "$(dirname -- "$0")/../../mock" && pwd):$PATH"\nexport PATH',1)
        entry=root/'stage/tools/native-worker-update.sh';entry.write_text(script,encoding='utf8',newline='\n')
        env=os.environ.copy();env['NCC_UPDATE_FIXTURE']=str(root)
        if scenario=='activation-fails':env['NCC_UPDATE_FAIL_ONCE']='1'
        if scenario=='preserved-service-changes':env['NCC_UPDATE_DRIFT']='1'
        if scenario=='native-busy':env['NCC_UPDATE_BUSY']='1'
        command=[BASH]+(['-x']if os.environ.get('NCC_UPDATER_FIXTURE_TRACE')else[])+[entry.as_posix()]
        # Git Bash launches many Python mock commands for the stability checks.
        # Allow host startup overhead without relaxing any production deadline.
        result=subprocess.run(command,env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=120 if os.name=='nt' else 30)
        commands=[json.loads(s)for s in (root/'commands.jsonl').read_text().splitlines()]if(root/'commands.jsonl').exists()else[]
        mutations=[c for c in commands if 'restart'in c or 'start'in c or 'stop'in c]
        self.assertTrue(all(c==['--no-block','restart','np1-control-native.service']for c in mutations),mutations)
        return root,result,originals,inputs,mutations

    def test_update_changes_only_native_and_preserves_private_originals(self):
        root,result,originals,inputs,mutations=self.run_fixture()
        self.assertEqual(result.returncode,0,result.stderr.decode(errors='replace'))
        self.assertIn(b'PASS: native worker updated',result.stdout)
        self.assertEqual(len(mutations),1)
        self.assertEqual((root/'base/bin/ncc').read_bytes(),inputs['bin/ncc'])
        backups=list((root/'base/recovery').glob('native-worker-*'));self.assertEqual(len(backups),1)
        self.assertEqual((backups[0]/'original/bin/ncc').read_bytes(),originals['base/bin/ncc'])

    def test_failed_activation_restores_exact_old_files_and_original_absence(self):
        root,result,originals,_,mutations=self.run_fixture('activation-fails')
        self.assertNotEqual(result.returncode,0)
        self.assertEqual(len(mutations),2)
        for name,data in originals.items():self.assertEqual((root/name).read_bytes(),data)
        self.assertFalse((root/'base/experimental/ncc-native-audio-helper').exists())

    def test_busy_native_refuses_before_any_file_or_service_change(self):
        root,result,originals,_,mutations=self.run_fixture('native-busy')
        self.assertNotEqual(result.returncode,0);self.assertEqual(mutations,[])
        self.assertIn(b'A native operation is active',result.stderr)
        for name,data in originals.items():self.assertEqual((root/name).read_bytes(),data)
        self.assertEqual(list((root/'base/recovery').iterdir()),[])

    def test_unrelated_service_change_never_gets_repaired_by_restarting_it(self):
        root,result,originals,_,mutations=self.run_fixture('preserved-service-changes')
        self.assertNotEqual(result.returncode,0);self.assertEqual(len(mutations),2)
        for name,data in originals.items():self.assertEqual((root/name).read_bytes(),data)


if __name__=='__main__':unittest.main()
