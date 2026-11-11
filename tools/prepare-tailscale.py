"""Fetch the pinned official static build and verify its published SHA-256.
The third-party binaries remain ignored; no credentials are downloaded.
"""
import hashlib
import json
import pathlib
import tarfile
import urllib.request
VERSION = '1.104.6'
root = pathlib.Path(__file__).resolve().parent.parent
filename = 'tailscale_' + VERSION + '_arm64.tgz'
url = 'https://pkgs.tailscale.com/stable/' + filename
directory = root / 'resources/tailscale'
directory.mkdir(parents=True, exist_ok=True)
archive = directory / filename
expected = urllib.request.urlopen(url + '.sha256', timeout=30).read().decode().split()[0]
if not archive.exists() or hashlib.sha256(archive.read_bytes()).hexdigest() != expected:
    with urllib.request.urlopen(url, timeout=60) as response:
        archive.write_bytes(response.read())
assert hashlib.sha256(archive.read_bytes()).hexdigest() == expected, 'Tailscale checksum mismatch'
destination = root / 'artifacts/ncc-vpn'
(destination / 'bin').mkdir(parents=True, exist_ok=True)
with tarfile.open(archive) as bundle:
    for name in ('tailscale', 'tailscaled'):
        member = bundle.getmember('tailscale_' + VERSION + '_arm64/' + name)
        (destination / 'bin' / name).write_bytes(bundle.extractfile(member).read())
manifest = {'version': VERSION, 'source': url, 'sha256': expected}
(destination / 'source.json').write_text(json.dumps(manifest, indent=2), encoding='utf8')
print('Verified official Tailscale', VERSION, 'sha256', expected)
