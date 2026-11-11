"""Compile only the standalone vendor trial helper. Does not deploy or invoke it.

The minimal glibc link files must come from the owner device into resources/
(native-sysroot/libc.so.6 and libdl.so.2). Never redistribute those files.
"""
import argparse
import pathlib
import shutil
import subprocess
import sys

ROOT=pathlib.Path(__file__).resolve().parents[1]
p=argparse.ArgumentParser()
p.add_argument('--clang',default=shutil.which('clang') or r'C:\Program Files\LLVM\bin\clang.exe')
p.add_argument('--test-only',action='store_true')
a=p.parse_args()
source=ROOT/'tools/native/native-setting-choice-helper.c'
output=ROOT/'artifacts/native-choice-helper';output.mkdir(parents=True,exist_ok=True)
common=[a.clang,'-O2','-Wall','-Wextra','-Werror','-ffreestanding','-fno-builtin','-fno-stack-protector','-fuse-ld=lld']
wasm=ROOT/'tmp/native-choice-core.wasm'
subprocess.run(common+['--target=wasm32','-nostdlib','-DNCC_NATIVE_CORE_ONLY',str(source),'-Wl,--no-entry','-Wl,--export-memory','-Wl,--initial-memory=262144','-Wl,--export=ncc_scratch_ptr','-Wl,--export=ncc_validate_request','-Wl,--export=ncc_choice_persistent','-Wl,--export=ncc_choice_apply_number','-Wl,--export=ncc_choice_baseline','-Wl,--export=ncc_choice_disk_value','-o',str(wasm)],check=True)
subprocess.run(['node',str(ROOT/'tools/native/test-native-choice.cjs'),str(wasm)],check=True)
if a.test_only:sys.exit(0)

# Cross-check ordinal -> persistent key against the pinned private ELF table.
# This does not execute vendor code or copy the ELF into the output bundle.
import re,struct
from elftools.elf.elffile import ELFFile
import hashlib
if hashlib.sha256((ROOT/'resources/libAplLib.so').read_bytes()).hexdigest()!='83bcfe30838001b382e58771fd28eabf17e43e6e72568ee5ad80656d223eae00':raise SystemExit('Pinned AplLib proof material mismatch')
with (ROOT/'resources/libAplLib.so').open('rb') as f:
 image=ELFFile(f);section=next(s for s in image.iter_sections() if s['sh_addr']<=0x189a80<s['sh_addr']+s['sh_size'])
 offset=0x189a80-section['sh_addr'];keys=struct.unpack('<86I',section.data()[offset:offset+344])
for index,key in re.findall(r'\{"[a-z-]+",(\d+),(\d+)\}',(ROOT/'tools/native/native-choice-core.h').read_text()):
 if keys[int(index)]!=int(key):raise SystemExit('Choice persistent-key proof mismatch')

material=ROOT/'resources/native-sysroot'
for name in ['libc.so.6','libdl.so.2','libpthread.so.0']:
 if not (material/name).is_file():raise SystemExit('Missing owner-device link material: resources/native-sysroot/'+name)
elf=output/'ncc-native-setting-choice-helper'
subprocess.run(common+['--target=aarch64-linux-gnu','-nostdlib','-fPIE','-pie',str(source),str(ROOT/'tools/native/start-aarch64.S'),'-Wl,--no-as-needed',str(material/'libdl.so.2'),str(material/'libpthread.so.0'),str(material/'libc.so.6'),'-Wl,--allow-shlib-undefined','-Wl,--export-dynamic','-Wl,-e,_start','-Wl,--dynamic-linker=/lib/ld-linux-aarch64.so.1','-Wl,-z,relro','-Wl,-z,now','-o',str(elf)],check=True)
from elftools.elf.elffile import ELFFile
with elf.open('rb') as f:
 image=ELFFile(f)
 needed=[t.needed for t in image.get_section_by_name('.dynamic').iter_tags('DT_NEEDED')]
 if any('NStandard' in n or 'AplLib' in n or 'AIPI' in n for n in needed):raise SystemExit('Vendor library linked before preflight')
 symbols={s.name for s in image.get_section_by_name('.dynsym').iter_symbols() if s['st_value']}
 for name in ['_Z23RemoveAllTemporaryFilesv','_ZN17AL_SettingService13EnableSettingEv','open','semget','semctl','semop','usleep','pthread_cond_wait','pthread_cond_timedwait','pthread_mutex_lock','pthread_mutex_unlock','pthread_mutex_timedlock','pthread_create','_Z12NSystemErrorjPKcmmmb','_ZN17NProcSharedMemory4OpenEPKDs','_ZN17NProcSharedMemory10FreeMemoryEv']:
  if name not in symbols:raise SystemExit('Required process-local guard not exported: '+name)
 if image['e_machine']!='EM_AARCH64':raise SystemExit('Incorrect helper architecture')
 print('PASS: AArch64 ELF, vendor-free startup dependencies, exported initialization guards')
print('Built standalone helper; not deployed or executed: '+str(elf))


import hashlib,json
sources=[ROOT/'tools/native'/n for n in ['native-setting-choice-helper.c','native-choice-core.h','native-setting-helper.c','native-core.h','vendor-fingerprints.h','vendor-fingerprints.json','vendor-slots.h','start-aarch64.S']]
info={'architecture':'aarch64-linux-gnu','sha256':hashlib.sha256(elf.read_bytes()).hexdigest(),'needed':needed,'sources':{str(f.relative_to(ROOT)):hashlib.sha256(f.read_bytes()).hexdigest() for f in sources},'compiler':subprocess.check_output([a.clang,'--version'],text=True).splitlines()[0],'executedOnDevice':False}
(output/'build-info.json').write_text(json.dumps(info,indent=2)+'\n')
