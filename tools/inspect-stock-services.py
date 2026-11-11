"""Collect only read-only stock systemd evidence using an explicit ADB serial.

Raw runtime IDs/unit definitions stay in ignored tmp. The summary excludes PIDs,
command lines, addresses, credentials and device identifiers. Never mutates a unit.
"""
import argparse
import json
import pathlib
import re
import subprocess
import time
import uuid

ROOT = pathlib.Path(__file__).resolve().parents[1]
PUBLIC_FIELDS = frozenset(('Id', 'LoadState', 'ActiveState', 'SubState', 'UnitFileState',
    'Type', 'Restart', 'RestartUSec', 'KillMode', 'KillSignal', 'SendSIGKILL',
    'TimeoutStartUSec', 'TimeoutStopUSec', 'StartLimitIntervalUSec', 'StartLimitBurst',
    'RefuseManualStart', 'RefuseManualStop', 'CanStart', 'CanStop', 'CanReload',
    'StopWhenUnneeded', 'Requires', 'Wants', 'Requisite', 'BindsTo', 'PartOf',
    'After', 'Before', 'Conflicts', 'ConsistsOf', 'BoundBy', 'RequiredBy',
    'RequisiteOf', 'Triggers', 'TriggeredBy'))


def parse_public(text):
    result = []
    for match in re.finditer(r'^=== UNIT ([A-Za-z0-9_@.-]+\.service) ===\n(.*?)(?=^=== UNIT |\Z)', text, re.M | re.S):
        section = match.group(2).split('--- unit definition ---', 1)[0]
        fields = dict(line.split('=', 1) for line in section.splitlines() if '=' in line)
        item = {key: value for key, value in fields.items() if key in PUBLIC_FIELDS}
        item['unit'] = match.group(1)
        item['hasMainPID'] = bool(re.fullmatch(r'[1-9][0-9]*', fields.get('MainPID', '')))
        result.append(item)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--serial', required=True)
    args = parser.parse_args()
    if not re.fullmatch(r'[A-Za-z0-9._:-]+', args.serial):
        parser.error('invalid serial')
    evidence = ROOT / 'tmp/stock-service-evidence' / (time.strftime('%Y%m%d-%H%M%S') + '-' + uuid.uuid4().hex[:8])
    evidence.mkdir(parents=True, mode=0o700)
    script = (ROOT / 'tools/device/inspect-stock-services.sh').read_text(encoding='utf8')
    result = subprocess.run(['adb', '-s', args.serial, 'shell', script], stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    (evidence / 'inventory.txt').write_bytes(result.stdout)
    text = result.stdout.decode('utf8', errors='replace').replace('\r', '')
    summary = parse_public(text)
    (evidence / 'summary.json').write_text(json.dumps(summary, indent=2), encoding='utf8')
    if result.returncode or not summary:
        raise SystemExit('Read-only inventory incomplete; inspect private stock-service evidence')
    print('Read-only stock service inventory captured; units=' + str(len(summary)))
    print('Private evidence: ' + str(evidence))


if __name__ == '__main__':
    main()
