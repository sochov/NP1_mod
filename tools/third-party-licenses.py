"""Collect upstream license texts, or verify the checked-in inventory offline.

Generation needs npm ci, the Go module cache, and optional Python tools installed.
Only license/notice texts are copied; third-party code and binaries are not vendored.
"""
import argparse
import base64
import hashlib
import importlib.metadata
import io
import json
import pathlib
import re
import subprocess
import tarfile
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parents[1]
OPEN_LICENSES = {
    'MIT', 'ISC', 'BSD-2-Clause', 'BSD-3-Clause', 'Apache-2.0',
    'MPL-2.0', 'LGPL-2.1-or-later', 'Apache-2.0 OR BSD-3-Clause',
    'MIT AND Apache-2.0', 'BSD-2-Clause AND BSD-3-Clause',
    'Unlicense AND MIT', 'BSD-3-Clause AND NCSA',
}


def json_stream(raw):
    decoder = json.JSONDecoder()
    while raw.strip():
        value, end = decoder.raw_decode(raw.lstrip())
        yield value
        raw = raw.lstrip()[end:]


def safe_name(value):
    return re.sub(r'[^a-zA-Z0-9._-]+', '-', value).strip('-')


def notice_name(name):
    return bool(re.search(r'license|licence|copying|notice|patents|copyright', name, re.I))


def input_hashes(root):
    names = ['backend/go.mod', 'backend/go.sum', 'frontend/package.json', 'frontend/package-lock.json']
    return {name: hashlib.sha256((root / name).read_bytes().replace(b'\r\n', b'\n')).hexdigest() for name in names}


def package_notices(directory):
    return [(p.name, p.read_bytes()) for p in sorted(directory.iterdir())
            if p.is_file() and notice_name(p.name)]


def detect_go_license(module, directory):
    text = (directory / 'LICENSE').read_text(encoding='utf8')
    if module == 'gopkg.in/yaml.v3':
        return 'MIT AND Apache-2.0'
    if module == 'github.com/hirochachacha/go-smb2':
        return 'BSD-2-Clause AND BSD-3-Clause'
    if 'Permission is hereby granted, free of charge' in text:
        return 'MIT'
    if text.startswith('ISC License'):
        return 'ISC'
    if 'Neither the name' in text or 'neither the name' in text:
        return 'BSD-3-Clause'
    if 'Redistribution and use in source and binary forms' in text:
        return 'BSD-2-Clause'
    raise ValueError('Review Go module license: ' + module)


def store_notices(root, ecosystem, name, version, notices):
    if not notices or not any('licen' in n.lower() or 'copying' in n.lower() for n, _ in notices):
        raise ValueError('Missing license text: ' + name)
    destination = root / 'licenses' / ecosystem / (safe_name(name) + '-' + safe_name(version))
    destination.mkdir(parents=True, exist_ok=True)
    records = []
    for original, data in notices:
        data = data.replace(b'\r\n', b'\n')
        data.decode('utf8')
        path = destination / (safe_name(original) + '.txt')
        path.write_bytes(data)
        records.append({'path': path.relative_to(root).as_posix(),
                        'upstreamFile': original, 'sha256': hashlib.sha256(data).hexdigest()})
    return records


def npm_entries(root):
    lock = json.loads((root / 'frontend/package-lock.json').read_text(encoding='utf8'))
    for location, value in sorted(lock['packages'].items()):
        if not location:
            continue
        name, version = location.removeprefix('node_modules/'), value['version']
        license_id = value.get('license')
        if license_id not in OPEN_LICENSES:
            raise ValueError('Unknown or unreviewed npm license: ' + name)
        directory = root / 'frontend' / location
        source = 'npm distribution'
        if directory.is_dir():
            installed = json.loads((directory / 'package.json').read_text(encoding='utf8'))
            if installed['version'] != version:
                raise ValueError('Installed version differs from lock: ' + name)
            notices = package_notices(directory)
        else:
            notices = []
        if not notices and (name.startswith('@rolldown/binding-') or name.startswith('lightningcss-')):
            # Native platform packages use the same project's license. Include
            # its complete notices, including bundled third-party notices.
            parent = 'rolldown' if name.startswith('@rolldown/') else 'lightningcss'
            parent_location = 'node_modules/' + parent
            if lock['packages'][parent_location]['version'] != version:
                raise ValueError('Platform package version differs from project: ' + name)
            if lock['packages'][parent_location]['license'] != license_id:
                raise ValueError('Platform package license differs from project: ' + name)
            notices = package_notices(root / 'frontend' / parent_location)
            source = parent + ' project notices (same version; optional native platform package)'
        if not notices and name == 'fsevents':
            # This optional macOS-only package is absent from Windows npm ci.
            url = value['resolved']
            if not url.startswith('https://registry.npmjs.org/fsevents/-/'):
                raise ValueError('Unexpected fsevents registry URL')
            data = urllib.request.urlopen(url, timeout=30).read(2 * 1024 * 1024)
            algorithm, expected = value['integrity'].split('-', 1)
            if algorithm != 'sha512' or base64.b64encode(hashlib.sha512(data).digest()).decode() != expected:
                raise ValueError('fsevents integrity mismatch')
            with tarfile.open(fileobj=io.BytesIO(data), mode='r:gz') as archive:
                notices = [(pathlib.PurePosixPath(p.name).name, archive.extractfile(p).read())
                           for p in archive.getmembers() if p.isfile()
                           and len(pathlib.PurePosixPath(p.name).parts) == 2 and notice_name(p.name)]
            source = 'npm distribution (lockfile SHA-512 verified)'
        yield {'ecosystem': 'npm', 'name': name, 'version': version, 'license': license_id,
               'scope': 'build/test' if value.get('dev') else 'UI runtime',
               'optional': value.get('optional', False),
               'upstream': 'https://www.npmjs.com/package/' + name + '/v/' + version,
               'evidence': source, 'notices': store_notices(root, 'npm', name, version, notices)}


def go_entries(root, go):
    goroot = pathlib.Path(subprocess.check_output([go, 'env', 'GOROOT'], text=True).strip())
    data = subprocess.check_output([go, '-C', str(root / 'backend'), 'mod', 'download', '-json', 'all'], text=True)
    for module in json_stream(data):
        directory = pathlib.Path(module['Dir'])
        name, version = module['Path'], module['Version']
        license_id = detect_go_license(name, directory)
        notices = [(p.relative_to(directory).as_posix(), p.read_bytes())
                   for p in sorted(directory.rglob('*')) if p.is_file() and notice_name(p.name)]
        if name == 'github.com/hirochachacha/go-smb2':
            headers = []
            for source in sorted(directory.rglob('*.go')):
                lines = [line for line in source.read_text(encoding='utf8').splitlines()
                         if line.startswith(('// Copyright ', '// Portions Copyright '))]
                if lines:
                    headers.append(source.relative_to(directory).as_posix() + '\n' + '\n'.join(lines))
            notices += [('GO-LICENSE', (goroot / 'LICENSE').read_bytes()),
                        ('UPSTREAM-COPYRIGHTS', ('\n\n'.join(headers) + '\n').encode())]
        yield {'ecosystem': 'go', 'name': name, 'version': version, 'license': license_id,
               'scope': 'Go module graph (includes transitive/test modules)',
               'upstream': 'https://pkg.go.dev/' + name + '@' + version,
               'evidence': 'Go module distribution',
               'notices': store_notices(root, 'go', name, version, notices)}
    version = (goroot / 'VERSION').read_text(encoding='utf8').splitlines()[0]
    yield {'ecosystem': 'toolchain', 'name': 'Go standard library/runtime', 'version': version,
           'license': 'BSD-3-Clause', 'scope': 'linked Go runtime and standard library',
           'upstream': 'https://go.googlesource.com/go/+/refs/tags/' + version + '/LICENSE',
           'evidence': 'installed Go distribution',
           'notices': store_notices(root, 'toolchain', 'go', version, package_notices(goroot))}


def python_entries(root):
    # These are external, optional host tools, not files embedded in the NP1
    # software. Record the installed, inspected distribution versions exactly.
    reviewed = {'paramiko': 'LGPL-2.1-or-later', 'bcrypt': 'Apache-2.0',
                'cryptography': 'Apache-2.0 OR BSD-3-Clause', 'cffi': 'MIT',
                'pycparser': 'BSD-3-Clause', 'invoke': 'BSD-2-Clause', 'PyNaCl': 'Apache-2.0',
                'pyelftools': 'Unlicense AND MIT', 'capstone': 'BSD-3-Clause AND NCSA', 'protobuf': 'BSD-3-Clause'}
    for name, license_id in reviewed.items():
        distribution = importlib.metadata.distribution(name)
        notices = [(pathlib.PurePosixPath(str(p)).name, pathlib.Path(distribution.locate_file(p)).read_bytes())
                   for p in distribution.files or [] if notice_name(pathlib.PurePosixPath(str(p)).name)]
        if name == 'paramiko':
            source = pathlib.Path(distribution.locate_file('paramiko/__init__.py')).read_text(encoding='utf8')
            header = '\n'.join(line for line in source.split('\n\n', 1)[0].splitlines() if line.startswith('#'))
            notices.append(('UPSTREAM-NOTICE', (header + '\n').encode()))
        if name in ('pyelftools','capstone'):
            if name=='pyelftools':
                if distribution.version!='0.33': raise ValueError('Review updated pyelftools bundled construct license')
                url='https://raw.githubusercontent.com/eliben/pyelftools/v0.33/elftools/construct/LICENSE'
                filename='CONSTRUCT-LICENSE'
            else:
                if distribution.version!='5.0.7': raise ValueError('Review updated Capstone LLVM license')
                url='https://raw.githubusercontent.com/capstone-engine/capstone/5.0.7/LICENSE_LLVM.TXT'
                filename='LICENSE_LLVM.TXT'
            notices.append((filename,urllib.request.urlopen(url,timeout=30).read(1024*1024)))
        yield {'ecosystem': 'python', 'name': name, 'version': distribution.version,
               'license': license_id, 'scope': 'optional host analysis/SSH tooling; not bundled',
               'upstream': 'https://pypi.org/project/' + name + '/' + distribution.version + '/',
               'evidence': 'installed Python distribution',
               'notices': store_notices(root, 'python', name, distribution.version, notices)}


def verify(root):
    inventory = json.loads((root / 'licenses/inventory.json').read_text(encoding='utf8'))
    packages = inventory['packages']
    if inventory.get('inputHashes') != input_hashes(root):
        raise ValueError('Dependency inputs changed; regenerate notices')
    expected = {p.removeprefix('node_modules/'): value['version'] for p, value in
                json.loads((root / 'frontend/package-lock.json').read_text(encoding='utf8'))['packages'].items() if p}
    actual = {p['name']: p['version'] for p in packages if p['ecosystem'] == 'npm'}
    if expected != actual:
        raise ValueError('npm inventory differs from package-lock.json; regenerate notices')
    go_text = (root / 'backend/go.mod').read_text(encoding='utf8')
    go_actual = {p['name']: p['version'] for p in packages if p['ecosystem'] == 'go'}
    for name, version in re.findall(r'^\s*(?:require\s+)?([^\s()]+)\s+(v\S+)', go_text, re.M):
        if go_actual.get(name) != version:
            raise ValueError('Go inventory differs from go.mod: ' + name)
    for package in packages:
        if package['license'] not in OPEN_LICENSES:
            raise ValueError('Unknown license: ' + package['name'])
        if not package['notices']:
            raise ValueError('Missing notices: ' + package['name'])
        for notice in package['notices']:
            path = (root / notice['path']).resolve()
            if not path.is_relative_to((root / 'licenses').resolve()):
                raise ValueError('Notice path escapes licenses directory')
            data = path.read_bytes().replace(b'\r\n', b'\n')
            if hashlib.sha256(data).hexdigest() != notice['sha256']:
                raise ValueError('Notice text changed: ' + notice['path'])
    print('PASS: third-party license inventory and original notice hashes (' + str(len(packages)) + ' entries)')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--generate', action='store_true')
    parser.add_argument('--go', default='go')
    parser.add_argument('--reference-license', type=pathlib.Path)
    parser.add_argument('--tailscale-license', type=pathlib.Path)
    args = parser.parse_args()
    if args.generate:
        entries = list(npm_entries(ROOT)) + list(go_entries(ROOT, args.go)) + list(python_entries(ROOT))
        if not args.reference_license or not args.tailscale_license:
            parser.error('Generation requires --reference-license and --tailscale-license')
        entries += [
            {'ecosystem': 'reference', 'name': 'radio-map', 'version': '2026', 'license': 'MIT',
             'scope': 'design reference; independently implemented', 'upstream': '../licenses/reference/radio-map-2026/LICENSE.txt',
             'evidence': 'reference project LICENSE',
             'notices': store_notices(ROOT, 'reference', 'radio-map', '2026', [('LICENSE', args.reference_license.read_bytes())])},
            {'ecosystem': 'external', 'name': 'Tailscale', 'version': '1.104.6', 'license': 'BSD-3-Clause',
             'scope': 'optional official download; binary/source not in this Git repository',
             'upstream': 'https://github.com/tailscale/tailscale/tree/v1.104.6',
             'evidence': 'upstream v1.104.6 LICENSE; official binary has its own dependency notices',
             'notices': store_notices(ROOT, 'external', 'tailscale', '1.104.6', [('LICENSE', args.tailscale_license.read_bytes())])},
        ]
        (ROOT / 'licenses/inventory.json').write_text(json.dumps({'schemaVersion': 1, 'inputHashes': input_hashes(ROOT), 'packages': entries}, indent=2) + '\n', encoding='utf8')
        lines = ['<!-- Generated package table; license texts are unmodified upstream notices. -->',
                 '| Component | Version | License | Scope | Notices |', '| --- | --- | --- | --- | --- |']
        for package in entries:
            links = ', '.join('[' + n['upstreamFile'] + '](../' + n['path'] + ')' for n in package['notices'])
            lines.append('| [' + package['name'] + '](' + package['upstream'] + ') | ' + package['version'] + ' | ' +
                         package['license'] + ' | ' + package['scope'] + ' | ' + links + ' |')
        (ROOT / 'licenses/COMPONENTS.md').write_text('\n'.join(lines) + '\n', encoding='utf8')
    verify(ROOT)


if __name__ == '__main__':
    main()
