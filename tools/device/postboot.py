#!/usr/bin/env python3
"""Python 3.5 compatible. Only edit our exact block, using atomic same-filesystem rename."""
import os
import errno
import sys
BEGIN = '# BEGIN NP1_CONTROL_CENTER_V1'
END = '# END NP1_CONTROL_CENTER_V1'
BLOCK = '''# BEGIN NP1_CONTROL_CENTER_V1
if test -f /overlay/owner-apps/np1-control/enabled; then
    systemctl daemon-reload
    systemctl restart np1-control-broker.service || true
    systemctl restart np1-control.service || true
fi
# END NP1_CONTROL_CENTER_V1'''

VPN_BLOCK = '''# BEGIN NP1_CONTROL_CENTER_VPN_V1
if test -f /overlay/owner-apps/np1-control/vpn-enabled; then
    systemctl daemon-reload
    systemctl restart np1-control-tailscale.service || true
fi
# END NP1_CONTROL_CENTER_VPN_V1'''

def transform(text, action, block=BLOCK):
    begin, end = block.splitlines()[0], block.splitlines()[-1]
    if text.count(begin) != text.count(end) or text.count(begin) > 1:
        raise ValueError('Ambiguous post-boot markers; manual inspection required')
    if begin in text:
        start = text.index(begin)
        finish = text.index(end, start) + len(end)
        if text[start:finish] != block:
            raise ValueError('Existing owner block differs; refusing to overwrite')
        text = text[:start] + text[finish:]
    if action == 'add':
        return text.rstrip('\n') + '\n\n' + block + '\n'
    if action == 'remove':
        return text
    raise ValueError('Invalid action')

if __name__ == '__main__':
    filename = '/etc/init.post_boot.sh'
    if os.path.islink(filename):
        raise RuntimeError('Refusing symlink post-boot target')
    with open(filename, 'r') as stream:
        before = stream.read()
    action = sys.argv[1]
    after = transform(before, action.replace('-vpn', ''), VPN_BLOCK if action.endswith('-vpn') else BLOCK)
    if before != after:
        stat = os.stat(filename)
        temporary = filename + '.ncc-new'
        with open(temporary, 'w') as stream:
            stream.write(after)
            stream.flush()
            os.fsync(stream.fileno())
        os.chmod(temporary, stat.st_mode & 0o777)
        os.chown(temporary, stat.st_uid, stat.st_gid)
        try:
            os.rename(temporary, filename)
        except OSError as error:
            if error.errno != errno.ESTALE:
                raise
            # NP1 overlayfs may expose a disconnected lower inode (nlink=0), refusing rename.
            # Installer already saved the original. Preserve the existing inode, verify no
            # concurrent change, write+fsync then truncate, and read back through the live view.
            with open(filename, 'r+') as stream:
                if stream.read() != before:
                    raise RuntimeError('Post-boot file changed concurrently')
                stream.seek(0)
                stream.write(after)
                stream.flush()
                os.fsync(stream.fileno())
                stream.truncate()
                os.fsync(stream.fileno())
            os.unlink(temporary)
        with open(filename, 'r') as stream:
            if stream.read() != after:
                raise RuntimeError('Post-boot read-back mismatch')
