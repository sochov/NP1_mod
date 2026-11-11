"""Normalize script line endings and hash every install input."""
import hashlib
import pathlib
import sys
root = pathlib.Path(sys.argv[1]).resolve()
for path in root.rglob('*'):
    if path.is_file() and path.suffix in ('.sh', '.py', '.service'):
        original = path.read_bytes()
        normalized = original.replace(b'\r\n', b'\n')
        if normalized != original:
            path.write_bytes(normalized)
lines = []
for path in sorted(root.rglob('*')):
    if path.is_file() and path.name != 'SHA256SUMS':
        lines.append(hashlib.sha256(path.read_bytes()).hexdigest() + '  ' + path.relative_to(root).as_posix())
manifest = ('\n'.join(lines) + '\n').encode('ascii')
target = root / 'SHA256SUMS'
if not target.exists() or target.read_bytes() != manifest:
    target.write_bytes(manifest)
print('Install manifest:', len(lines), 'files')
