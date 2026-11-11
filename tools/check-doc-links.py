"""Check relative Markdown links in public documentation, without external requests."""
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import unquote, urlsplit

root = Path(__file__).resolve().parents[1]
paths = subprocess.check_output(['git', 'ls-files', '--cached', '--others', '--exclude-standard', '-z'], cwd=root).decode().split('\0')
failures = []
checked = 0
for relative in sorted(set(filter(None, paths))):
    path = root / relative
    if path.suffix.lower() != '.md' or not path.is_file():
        continue
    if relative.startswith(('docs_private/', 'tmp/')) or relative == 'AGENTS.md':
        continue
    content = re.sub(r'```[^\n]*\n.*?```', '', path.read_text(encoding='utf8'), flags=re.S)
    for match in re.finditer(r'!?\[[^\]\n]*\]\((<[^>]+>|[^\s)]+)(?:\s+"[^"]*")?\)', content):
        url = urlsplit(match[1].strip('<>'))
        if url.scheme or url.netloc or not url.path:
            continue
        target = (path.parent / unquote(url.path)).resolve()
        checked += 1
        if not target.is_relative_to(root.resolve()) or not target.exists():
            failures.append(f'{relative}: missing or out-of-tree target {url.path}')
for failure in failures:
    print('FAIL ' + failure)
if failures:
    sys.exit(1)
print(f'PASS: {checked} local documentation links resolve')
