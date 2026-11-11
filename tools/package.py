"""Normalize script line endings and hash every install input."""
import hashlib
import pathlib
import sys
root = pathlib.Path(sys.argv[1]).resolve()
for path in root.rglob('*'):
    if path.is_file() and path.suffix in ('.sh', '.py', '.service'):
        path.write_bytes(path.read_bytes().replace(b'\r\n', b'\n'))
lines = []
for path in sorted(root.rglob('*')):
    if path.is_file() and path.name != 'SHA256SUMS':
        lines.append(hashlib.sha256(path.read_bytes()).hexdigest() + '  ' + path.relative_to(root).as_posix())
(root / 'SHA256SUMS').write_bytes(('\n'.join(lines) + '\n').encode('ascii'))
print('Install manifest:', len(lines), 'files')
