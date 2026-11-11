"""Build a separate guarded STA owner trial bundle; never deploy or run it."""
import argparse
import pathlib
import shutil
import subprocess
import sys

root = pathlib.Path(__file__).resolve().parents[1]
p = argparse.ArgumentParser()
p.add_argument('--clang', default=shutil.which('clang') or r'C:\Program Files\LLVM\bin\clang.exe')
a = p.parse_args()
subprocess.run([sys.executable, str(root / 'tools/build-native-wifi-sta-helper.py'), '--clang', a.clang], check=True)
subprocess.run([sys.executable, str(root / 'tools/build_stock_cm_probe.py')], check=True)
bundle = root / 'artifacts/ncc-native-wifi-sta'
(bundle / 'bin').mkdir(parents=True, exist_ok=True)
(bundle / 'tools').mkdir(exist_ok=True)
if not (bundle / 'bin/ncc-native-wifi-sta-helper').is_file():
    raise SystemExit('Standalone helper builder did not produce its fixed target')
shutil.copy2(root / 'artifacts/ncc-stock-cm-provenance/bin/ncc', bundle / 'bin/ncc-stock-cm-proof')
for name in ('native-wifi-sta-trial.sh', 'native-wifi-sta-trial.py'):
    shutil.copy2(root / 'tools/device' / name, bundle / 'tools' / name)
subprocess.run([sys.executable, str(root / 'tools/package.py'), str(bundle)], check=True)
print('Separate STA trial bundle built; no device operation performed')
