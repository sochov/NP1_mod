#!/usr/bin/env python3
"""Read-only ownership and interface descriptor inventory; no serial port opens."""
import os
for pid in os.listdir('/proc'):
    if not pid.isdigit():
        continue
    try:
        fds=os.listdir('/proc/'+pid+'/fd')
    except OSError:
        continue
    for fd in fds:
        try:
            path=os.readlink('/proc/'+pid+'/fd/'+fd)
            if path in ('/dev/ttyUSB2','/dev/ttyUSB3','/dev/qcqmi0'):
                with open('/proc/'+pid+'/comm') as stream:
                    print(pid,stream.read().strip(),path)
        except OSError:
            pass
for name in ('ttyUSB2','ttyUSB3'):
    entry=os.path.realpath('/sys/class/tty/'+name+'/device')
    while entry!='/':
        try:
            with open(entry+'/bInterfaceNumber') as stream:
                print(name,'USB interface',stream.read().strip())
                break
        except OSError:
            entry=os.path.dirname(entry)
