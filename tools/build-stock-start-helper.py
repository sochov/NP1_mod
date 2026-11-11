"""Build the separate, fixed AppManager start-barrier Notify helper (no deploy)."""
import argparse
import hashlib
import json
import pathlib
import shutil
import subprocess
from elftools.elf.elffile import ELFFile

ROOT = pathlib.Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--clang', default=shutil.which('clang') or r'C:\Program Files\LLVM\bin\clang.exe')
parser.add_argument('--test-only', action='store_true')
args = parser.parse_args()
common = [args.clang, '-O2', '-Wall', '-Wextra', '-Werror', '-ffreestanding', '-fno-builtin', '-fno-stack-protector', '-fuse-ld=lld']
wasm = ROOT / 'tmp/stock-start-core.wasm'
exports = ['buffer', 'number', 'time', 'threads', 'header', 'invocation', 'hex', 'map', 'futex', 'waiting']
subprocess.run(common + ['--target=wasm32', '-nostdlib', str(ROOT/'tools/native/test-stock-start.c'), '-Wl,--no-entry', '-Wl,--export-memory'] + ['-Wl,--export=ncc_start_'+name for name in exports] + ['-o',str(wasm)],check=True)
subprocess.run(['node',str(ROOT/'tools/native/test-stock-start.cjs'),str(wasm)],check=True)
if args.test_only:
    raise SystemExit(0)
material=ROOT/'resources/native-sysroot'
output=ROOT/'artifacts/stock-start-helper'
output.mkdir(parents=True,exist_ok=True)
binary=output/'ncc-stock-start-helper'
subprocess.run(common+['--target=aarch64-linux-gnu','-nostdlib','-fPIE','-pie',str(ROOT/'tools/native/stock-start-helper.c'),str(ROOT/'tools/native/start-aarch64.S'),'-Wl,--no-as-needed']+[str(material/name) for name in ('libdl.so.2','libpthread.so.0','libc.so.6')]+['-Wl,--allow-shlib-undefined','-Wl,--export-dynamic','-Wl,-e,_start','-Wl,--dynamic-linker=/lib/ld-linux-aarch64.so.1','-Wl,-z,relro','-Wl,-z,now','-o',str(binary)],check=True)
with binary.open('rb') as stream:
    image=ELFFile(stream)
    needed=[item.needed for item in image.get_section_by_name('.dynamic').iter_tags('DT_NEEDED')]
    assert image['e_machine']=='EM_AARCH64'
    assert not any(any(x in name for x in ('NStandard','AIPI','AplLib')) for name in needed)
    symbols={s.name for s in image.get_section_by_name('.dynsym').iter_symbols() if s['st_value']}
    for name in ('pthread_mutex_lock','pthread_mutex_unlock','pthread_mutex_timedlock','pthread_cond_wait','pthread_create','open','semop','unlink','ftruncate','fchmod','_ZN17NProcSharedMemory4OpenEPKDs','_ZN17NProcSharedMemory10FreeMemoryEv','_ZN17NProcSharedMemory5AllocEPKDsl','_Z23RemoveAllTemporaryFilesv'):
        assert name in symbols, name
sources=[ROOT/'tools/native'/name for name in ('stock-start-helper.c','stock-start-core.h','native-setting-helper.c','native-core.h','vendor-fingerprints.h','vendor-fingerprints.json','vendor-slots.h','start-aarch64.S')]
info={'sha256':hashlib.sha256(binary.read_bytes()).hexdigest(),'architecture':'aarch64-linux-gnu','needed':needed,'sources':{str(p.relative_to(ROOT)):hashlib.sha256(p.read_bytes()).hexdigest() for p in sources},'executedOnDevice':False}
(output/'build-info.json').write_text(json.dumps(info,indent=2)+'\n',encoding='utf8')
print('PASS: fixed start-barrier helper built, vendor-free startup and required guards; no deployment')
