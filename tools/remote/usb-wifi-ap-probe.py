"""Root-only spare USB Wi-Fi probe; source and secrets arrive over SSH stdin.

No host route, resolver, saved connection, protected interface, or stock AP is
modified. All mutable test files live in a private /run directory.
"""
import contextlib
import ctypes
import datetime
try:
    import fcntl
except ImportError:  # Pure parser fixtures also run on Windows.
    fcntl = None
import hashlib
import http.client
import ipaddress
import json
import os
import pathlib
import re
import select
import signal
import socket
import stat
import ssl
import struct
import subprocess
import sys
import tempfile
import time
import traceback
import uuid

AP = '192.168.226.1'
HOST = 'example.com'


class ProbeError(Exception):
    pass


def require(condition, code):
    if not condition:
        raise ProbeError(code)


def validate_profile(p):
    require(isinstance(p, dict), 'PROFILE_INVALID')
    require(re.fullmatch(r'[A-Za-z][A-Za-z0-9_.-]{0,14}', p.get('interface', '')), 'INTERFACE_INVALID')
    require(re.fullmatch(r'phy[0-9]+', p.get('phy', '')), 'PHY_INVALID')
    require(isinstance(p.get('protectedInterfaces'), list) and len(p['protectedInterfaces']) >= 2, 'PROTECTED_INTERFACES_REQUIRED')
    require(p['interface'] not in p['protectedInterfaces'], 'PROTECTED_INTERFACE_SELECTED')
    require(len(set(p['protectedInterfaces'])) == len(p['protectedInterfaces']), 'PROTECTED_INTERFACES_INVALID')
    require(all(re.fullmatch(r'[A-Za-z][A-Za-z0-9_.-]{0,14}', v) for v in p['protectedInterfaces']), 'PROTECTED_INTERFACES_INVALID')
    path = p.get('usbPath', '')
    require(path.startswith('/sys/devices/') and '/usb' in path and '..' not in path and '\n' not in path, 'USB_PATH_INVALID')
    require(re.fullmatch(r'rtw88_[A-Za-z0-9_]+', p.get('driver', '')), 'DRIVER_INVALID')


def normalized(value):
    if isinstance(value, dict):
        return {k: normalized(v) for k, v in value.items() if k not in ('valid_life_time', 'preferred_life_time', 'expires', 'cacheinfo')}
    if isinstance(value, list):
        return sorted((normalized(v) for v in value), key=lambda v: json.dumps(v, sort_keys=True))
    return value


def dhcp_options(data):
    result, offset = {}, 240
    while offset < len(data):
        code = data[offset]
        offset += 1
        if code == 255:
            return result
        if code == 0:
            continue
        require(offset < len(data), 'DHCP_OPTIONS_TRUNCATED')
        length = data[offset]
        offset += 1
        require(offset + length <= len(data) and code not in result, 'DHCP_OPTIONS_INVALID')
        result[code] = data[offset:offset + length]
        offset += length
    raise ProbeError('DHCP_OPTIONS_UNTERMINATED')


def dhcp_message_type(data, xid, mac):
    require(240 <= len(data) <= 1500, 'DHCP_SIZE_INVALID')
    require(data[:3] == b'\x02\x01\x06' and data[4:8] == xid and data[28:34] == mac and data[236:240] == b'\x63\x82\x53\x63', 'DHCP_IDENTITY_INVALID')
    opts = dhcp_options(data)
    require(len(opts.get(53, b'')) == 1 and opts.get(54) == socket.inet_aton(AP), 'DHCP_SERVER_INVALID')
    return opts[53][0]


def parse_dhcp(data, xid, mac, message_type):
    received_type = dhcp_message_type(data, xid, mac)
    require(received_type != 6, 'DHCP_NAK')
    require(received_type == message_type, 'DHCP_MESSAGE_TYPE_INVALID')
    opts = dhcp_options(data)
    address = ipaddress.IPv4Address(data[16:20])
    require(address in ipaddress.IPv4Network('192.168.226.0/24') and str(address) != AP and int(address) & 255 not in (0, 255), 'DHCP_ADDRESS_INVALID')
    if message_type == 5:
        require(opts.get(1) == socket.inet_aton('255.255.255.0') and opts.get(3) == socket.inet_aton(AP) and opts.get(6) == socket.inet_aton(AP), 'DHCP_GATEWAY_DNS_INVALID')
        require(len(opts.get(51, b'')) == 4 and 120 <= struct.unpack('!I', opts[51])[0] <= 2592000, 'DHCP_LEASE_INVALID')
    return str(address)


def internet_checksum(value):
    if len(value) % 2:
        value += b'\0'
    total = sum(struct.unpack('!' + 'H' * (len(value) // 2), value))
    while total >> 16:
        total = (total & 65535) + (total >> 16)
    return (~total) & 65535


def dhcp_frame(payload, mac):
    # Before a lease exists, normal IPv4 receive can discard a broadcast OFFER
    # despite a successful L2 association. A packet socket is independent of
    # local IPv4 address/route configuration, like a conventional DHCP client.
    udp = struct.pack('!HHHH', 68, 67, len(payload) + 8, 0) + payload
    ip = struct.pack('!BBHHHBBH4s4s', 0x45, 0, 20 + len(udp), 0, 0, 64, 17, 0, bytes(4), b'\xff' * 4)
    ip = ip[:10] + struct.pack('!H', internet_checksum(ip)) + ip[12:]
    return b'\xff' * 6 + mac + b'\x08\x00' + ip + udp


def dhcp_from_frame(frame, mac):
    if len(frame) < 42 or len(frame) > 1514 or frame[:6] not in (mac, b'\xff' * 6) or frame[12:14] != b'\x08\x00':
        return None
    ip = frame[14:]
    header_length = (ip[0] & 15) * 4
    if ip[0] >> 4 != 4 or header_length < 20 or len(ip) < header_length + 8 or ip[9] != 17:
        return None
    length, fragment = struct.unpack('!HH', ip[2:4] + ip[6:8])
    if length > len(ip) or length < header_length + 8 or fragment & 0x3fff or internet_checksum(ip[:header_length]) != 0 or ip[12:16] != socket.inet_aton(AP):
        return None
    udp = ip[header_length:length]
    source, target, size, checksum = struct.unpack('!HHHH', udp[:8])
    if (source, target) != (67, 68) or size < 8 or size > len(udp):
        return None
    if checksum and internet_checksum(ip[12:20] + b'\0\x11' + struct.pack('!H', size) + udp[:size]) != 0:
        return None
    return udp[8:size]


def dhcp_request(iface, mac, stats=None):
    stats = stats if stats is not None else {}
    stats.update(discoverSent=0, requestSent=0, framesReceived=0, ignoredFrames=0, offerReceived=0, ackReceived=0)
    xid = os.urandom(4)
    header = struct.pack('!BBBB4sHH4s4s4s4s16s64s128s', 1, 1, 6, 0, xid, 0, 0x8000, bytes(4), bytes(4), bytes(4), bytes(4), mac + bytes(10), bytes(64), bytes(128)) + b'\x63\x82\x53\x63'
    with socket.socket(socket.AF_PACKET, socket.SOCK_RAW, socket.htons(0x0800)) as s:
        s.bind((iface, 0))
        s.settimeout(1)
        address = None
        deadline = time.monotonic() + 12
        for kind in (1, 3):
            packet = header + bytes([53, 1, kind, 61, 7, 1]) + mac + bytes([55, 4, 1, 3, 6, 51])
            if kind == 3:
                packet += bytes([50, 4]) + socket.inet_aton(address) + bytes([54, 4]) + socket.inet_aton(AP)
            packet += b'\xff'
            while time.monotonic() < deadline:
                s.send(dhcp_frame(packet, mac))
                stats['discoverSent' if kind == 1 else 'requestSent'] += 1
                try:
                    until = min(deadline, time.monotonic() + 1)
                    while True:
                        s.settimeout(max(.01, until - time.monotonic()))
                        raw = s.recv(2048)
                        stats['framesReceived'] += 1
                        data = dhcp_from_frame(raw, mac)
                        if data is not None and len(data) >= 34 and data[4:8] == xid and data[28:34] == mac:
                            received = dhcp_message_type(data, xid, mac)
                            stats['lastMessageType'] = received
                            require(received != 6, 'DHCP_NAK')
                            if received == (2 if kind == 1 else 5):
                                break
                        stats['ignoredFrames'] += 1
                        if time.monotonic() >= until:
                            raise socket.timeout()
                    offered = parse_dhcp(data, xid, mac, 2 if kind == 1 else 5)
                    stats['offerReceived' if kind == 1 else 'ackReceived'] += 1
                    require(address is None or address == offered, 'DHCP_ADDRESS_CHANGED')
                    address = offered
                    break
                except socket.timeout:
                    continue
            else:
                raise ProbeError('DHCP_TIMEOUT')
        return address


def public_ipv4(value):
    address = ipaddress.ip_address(value)
    return address.version == 4 and address.is_global and not (address.is_multicast or address.is_reserved or address.is_loopback or address.is_link_local)


def dns_answer(data, token):
    require(12 <= len(data) <= 4096 and data[:2] == token, 'DNS_ID_INVALID')
    flags, questions, count = struct.unpack('!HHH', data[2:8])
    require(flags & 0xf80f == 0x8000 and not flags & 0x0200 and questions == 1 and 1 <= count <= 64, 'DNS_HEADER_INVALID')

    def name(offset):
        labels, seen, end = [], set(), None
        for _ in range(64):
            require(offset < len(data) and offset not in seen, 'DNS_NAME_INVALID')
            seen.add(offset)
            size = data[offset]
            offset += 1
            if size & 0xc0 == 0xc0:
                require(offset < len(data), 'DNS_NAME_INVALID')
                if end is None:
                    end = offset + 1
                offset = ((size & 63) << 8) | data[offset]
                continue
            require(size < 64 and offset + size <= len(data), 'DNS_NAME_INVALID')
            if size == 0:
                value = b'.'.join(labels).decode('ascii').lower()
                require(len(value) <= 253, 'DNS_NAME_INVALID')
                return value, end if end is not None else offset
            labels.append(data[offset:offset + size])
            offset += size
        raise ProbeError('DNS_NAME_INVALID')

    question, offset = name(12)
    require(question == HOST and data[offset:offset + 4] == b'\0\1\0\1', 'DNS_QUESTION_INVALID')
    offset += 4
    aliases, addresses = {}, {}
    for _ in range(count):
        owner, offset = name(offset)
        require(offset + 10 <= len(data), 'DNS_RECORD_INVALID')
        kind, cls, _, length = struct.unpack('!HHIH', data[offset:offset + 10])
        offset += 10
        end = offset + length
        require(end <= len(data), 'DNS_RECORD_INVALID')
        if cls == 1 and kind == 1:
            require(length == 4, 'DNS_A_INVALID')
            address = socket.inet_ntoa(data[offset:end])
            require(public_ipv4(address), 'DNS_NONPUBLIC_A')
            addresses.setdefault(owner, []).append(address)
        if cls == 1 and kind == 5:
            alias, following = name(offset)
            require(following == end and owner not in aliases, 'DNS_CNAME_INVALID')
            aliases[owner] = alias
        offset = end
    for _ in range(16):
        if question in addresses:
            return addresses[question][0]
        require(question in aliases, 'DNS_A_MISSING')
        question = aliases[question]
    raise ProbeError('DNS_CNAME_CYCLE')


def bound_socket(iface, source, socktype):
    s = socket.socket(socket.AF_INET, socktype)
    s.setsockopt(socket.SOL_SOCKET, socket.SO_BINDTODEVICE, iface.encode() + b'\0')
    s.bind((source, 0))
    s.settimeout(3)
    return s


def query_dns(iface, source, tcp=False):
    token = os.urandom(2)
    query = token + b'\x01\x00\x00\x01\x00\x00\x00\x00\x00\x00\x07example\x03com\x00\x00\x01\x00\x01'
    with bound_socket(iface, source, socket.SOCK_STREAM if tcp else socket.SOCK_DGRAM) as s:
        s.connect((AP, 53))
        if tcp:
            s.sendall(struct.pack('!H', len(query)) + query)
            def receive(n):
                out = b''
                while len(out) < n:
                    value = s.recv(n - len(out))
                    require(value, 'DNS_TCP_TRUNCATED')
                    out += value
                return out
            size = struct.unpack('!H', receive(2))[0]
            require(size <= 4096, 'DNS_SIZE_INVALID')
            answer = receive(size)
        else:
            s.send(query)
            answer = s.recv(4097)
        return dns_answer(answer, token)


class RemoteProbe:
    def __init__(self, payload):
        self.payload, self.profile = payload, payload['profile']
        validate_profile(self.profile)
        self.iface, self.phy = self.profile['interface'], self.profile['phy']
        self.runtime = None
        self.ns = 'ncc-usb-' + uuid.uuid4().hex[:12]
        self.hostns = os.open('/proc/self/ns/net', os.O_RDONLY)
        self.hostpid = os.getpid()
        self.moved = self.created = self.unmanaged = self.autoconnect_changed = False
        self.wpa = self.lock = None
        self.dhcp_stats = {}
        self.stage = 'initialized'
        self.testns = self.ns_identity = self.wpa_pidfd = None
        self.libc = ctypes.CDLL(None, use_errno=True)

    def emit(self, phase, **values):
        print(json.dumps(dict(phase=phase, **values)), flush=True)

    def enter_child(self):
        if self.testns is None or self.libc.setns(self.testns, 0x40000000) != 0:
            os._exit(126)

    def command(self, args, timeout=5, namespace=False):
        options = dict(preexec_fn=self.enter_child, pass_fds=(self.testns,)) if namespace else {}
        process = subprocess.run(args, capture_output=True, timeout=timeout, **options)
        if self.runtime:
            with (self.runtime / 'commands.log').open('ab') as log:
                log.write(process.stderr[:8192])
        require(process.returncode == 0, 'COMMAND_FAILED_' + pathlib.Path(args[0]).name.upper())
        return process.stdout.decode().strip()

    def identity(self):
        base = pathlib.Path('/sys/class/net') / self.iface
        require(str((base / 'device').resolve()) == self.profile['usbPath'], 'USB_IDENTITY_CHANGED')
        require((base / 'device/driver').resolve().name == self.profile['driver'] and (base / 'phy80211').resolve().name == self.phy, 'PHY_DRIVER_CHANGED')

    def nm(self, field):
        return self.command(['nmcli', '-g', 'GENERAL.' + field, 'device', 'show', self.iface])

    def host_snapshot(self):
        links = json.loads(self.command(['ip', '-j', 'address', 'show']))
        others = [row for row in links if row['ifname'] != self.iface]
        routes = [v for v in json.loads(self.command(['ip', '-j', 'route', 'show', 'table', 'all'])) if v.get('dev') != self.iface]
        routes6 = [v for v in json.loads(self.command(['ip', '-j', '-6', 'route', 'show', 'table', 'all'])) if v.get('dev') != self.iface]
        rules = self.command(['ip', 'rule', 'show']) + self.command(['ip', '-6', 'rule', 'show'])
        resolver = pathlib.Path('/etc/resolv.conf')
        protected = {}
        for iface in self.profile['protectedInterfaces']:
            require(any(v['ifname'] == iface for v in others), 'PROTECTED_INTERFACE_MISSING')
            raw = self.command(['iw', 'dev', iface, 'link'])
            protected[iface] = [line.strip() for line in raw.splitlines() if line.startswith('Connected to ') or line.strip().startswith('SSID:') or line.startswith('Not connected.')]
        connections = pathlib.Path('/etc/NetworkManager/system-connections')
        hashes = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in connections.iterdir() if p.is_file()} if connections.exists() else {}
        return normalized(dict(links=others, routes=routes, routes6=routes6, rules=rules, resolver=hashlib.sha256(resolver.read_bytes()).hexdigest(), resolverPath=str(resolver.resolve()), protected=protected, profiles=hashes))

    def preflight(self):
        require(os.geteuid() == 0, 'ROOT_REQUIRED')
        require(all(hasattr(os, name) for name in ('pidfd_open', 'P_PIDFD', 'WNOWAIT')) and hasattr(signal, 'pidfd_send_signal'), 'PIDFD_REQUIRED')
        require(pathlib.Path('/proc/mounts').read_text().find(' /run tmpfs ') >= 0, 'RUN_NOT_TMPFS')
        self.identity()
        info = self.command(['iw', 'dev', self.iface, 'info'])
        require('\ttype managed' in info and 'Not connected.' in self.command(['iw', 'dev', self.iface, 'link']), 'SPARE_NOT_IDLE_MANAGED')
        require(self.nm('STATE').startswith('30 ') and self.nm('NM-MANAGED') == 'yes', 'SPARE_NOT_NM_DISCONNECTED')
        self.original_autoconnect = self.nm('AUTOCONNECT')
        require(self.original_autoconnect in ('yes', 'no'), 'AUTOCONNECT_UNKNOWN')
        addresses = json.loads(self.command(['ip', '-j', 'addr', 'show', 'dev', self.iface]))
        require(len(addresses) == 1 and not addresses[0].get('addr_info'), 'SPARE_HAS_ADDRESSES')
        self.original_up = 'UP' in addresses[0]['flags']
        self.original_mac, self.original_mtu = addresses[0]['address'], addresses[0]['mtu']
        self.original_target_routes = self.target_routes()
        phy_interfaces = [p.name for p in pathlib.Path('/sys/class/net').iterdir() if (p / 'phy80211').exists() and (p / 'phy80211').resolve().name == self.phy]
        require(phy_interfaces == [self.iface], 'PHY_HAS_OTHER_INTERFACES')
        # Known tools referring to this phy/IF indicate another operator. Never
        # stop or adopt those processes, even when the interface is disconnected.
        for p in pathlib.Path('/proc').iterdir():
            if not p.name.isdigit() or int(p.name) == os.getpid():
                continue
            try:
                args = (p / 'cmdline').read_bytes().split(b'\0')
                exe = pathlib.Path(args[0].decode()).name if args else ''
                busy = exe in ('wpa_supplicant', 'hostapd', 'iw', 'tcpdump', 'airodump-ng', 'dumpcap', 'tshark')
                require(not (busy and any(self.iface.encode() in a or self.phy.encode() in a for a in args)), 'SPARE_REFERENCED_BY_PROCESS')
            except (OSError, UnicodeDecodeError):
                continue
        self.before = self.host_snapshot()

    def target_routes(self):
        return normalized([json.loads(self.command(['ip', '-j', family, 'route', 'show', 'table', 'all', 'dev', self.iface])) for family in ('-4', '-6')])

    def enter(self):
        require(self.testns is not None and self.libc.setns(self.testns, 0x40000000) == 0, 'SETNS_FAILED')

    def leave(self):
        require(self.libc.setns(self.hostns, 0x40000000) == 0, 'HOST_SETNS_FAILED')

    @contextlib.contextmanager
    def in_namespace(self):
        self.enter()
        try:
            yield
        finally:
            self.leave()

    def start(self):
        self.stage = 'authorization'
        require(self.payload.get('allowSpareUSBWiFiChanges') is True, 'USB_WIFI_MUTATION_NOT_AUTHORIZED')
        ssid = self.payload['ap']['ssid'].encode('utf-8')
        password = self.payload['ap']['password'].encode('utf-8')
        require(1 <= len(ssid) <= 32 and 8 <= len(password) <= 63, 'AP_ACCESS_INVALID')
        self.stage = 'own-lock'
        lockfd = os.open('/run/ncc-usb-wifi-probe.lock', os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
        self.lock = os.fdopen(lockfd, 'a+')
        metadata = os.fstat(lockfd)
        require(metadata.st_uid == 0 and stat.S_ISREG(metadata.st_mode) and metadata.st_nlink == 1 and metadata.st_size == 0 and stat.S_IMODE(metadata.st_mode) == 0o600, 'FOREIGN_LOCK_PRESERVED')
        fcntl.flock(self.lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        self.stage = 'preflight'
        self.preflight()
        self.stage = 'private-runtime'
        self.runtime = pathlib.Path(tempfile.mkdtemp(prefix='ncc-usb-probe-', dir='/run'))
        os.chmod(self.runtime, 0o700)
        recovery = dict(profile=self.profile, namespace=self.ns, hostPID=self.hostpid,
                        originalUp=self.original_up, originalMAC=self.original_mac,
                        originalMTU=self.original_mtu, originalAutoconnect=self.original_autoconnect,
                        originalTargetRoutes=self.original_target_routes, hostSnapshot=self.before)
        (self.runtime / 'recovery.json').write_text(json.dumps(recovery))
        self.autoconnect_changed = True
        self.stage = 'nm-autoconnect'
        self.command(['nmcli', 'device', 'set', self.iface, 'autoconnect', 'no'])
        self.unmanaged = True
        self.stage = 'nm-managed'
        self.command(['nmcli', 'device', 'set', self.iface, 'managed', 'no'])
        self.command(['ip', 'link', 'set', self.iface, 'down'])
        self.identity()
        require(not pathlib.Path('/run/netns', self.ns).exists(), 'NAMESPACE_ALREADY_EXISTS')
        self.created = True
        self.stage = 'netns-create'
        self.command(['ip', 'netns', 'add', self.ns])
        self.testns = os.open('/run/netns/' + self.ns, os.O_RDONLY)
        namespace_metadata = os.fstat(self.testns)
        self.ns_identity = namespace_metadata.st_dev, namespace_metadata.st_ino
        require(self.named_namespace_owned(), 'NAMESPACE_IDENTITY_CHANGED')
        self.moved = True
        self.stage = 'phy-move'
        self.command(['iw', 'phy', self.phy, 'set', 'netns', 'name', self.ns])
        psk = hashlib.pbkdf2_hmac('sha1', password, ssid, 4096, 32).hex()
        config = self.runtime / 'wpa.conf'
        config.write_text('ctrl_interface=' + str(self.runtime / 'ctrl') + '\nupdate_config=0\nnetwork={\nssid=' + ssid.hex() + '\npsk=' + psk + '\nkey_mgmt=WPA-PSK\nproto=RSN\npairwise=CCMP\ngroup=CCMP\n}\n')
        config.chmod(0o600)
        self.command(['ip', 'link', 'set', 'lo', 'up'], namespace=True)
        self.command(['ip', 'link', 'set', self.iface, 'up'], namespace=True)
        self.stage = 'wpa-start'
        self.wpa = subprocess.Popen(['wpa_supplicant', '-D', 'nl80211', '-i', self.iface, '-c', str(config), '-f', str(self.runtime / 'wpa.log')], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, preexec_fn=self.enter_child, pass_fds=(self.testns,))
        self.wpa_pidfd = os.pidfd_open(self.wpa.pid)
        # waitid(P_PIDFD) proves this reference belongs to our child even if a
        # very early exit and unrelated PID reuse occurred before pidfd_open.
        require(os.waitid(os.P_PIDFD, self.wpa_pidfd, os.WEXITED | os.WNOHANG | os.WNOWAIT) is None, 'OWN_WPA_EXITED')
        (self.runtime / 'own-wpa.json').write_text(json.dumps(dict(pid=self.wpa.pid, namespace=self.ns, startStat=pathlib.Path('/proc', str(self.wpa.pid), 'stat').read_text())))
        deadline = time.monotonic() + 35
        self.stage = 'association'
        while time.monotonic() < deadline:
            require(self.wpa.poll() is None, 'OWN_WPA_EXITED')
            try:
                status = self.command(['wpa_cli', '-p', str(self.runtime / 'ctrl'), '-i', self.iface, 'status'])
                if 'wpa_state=COMPLETED' in status:
                    break
            except ProbeError:
                pass
            time.sleep(.3)
        else:
            raise ProbeError('AP_ASSOCIATION_TIMEOUT')
        with self.in_namespace():
            self.stage = 'dhcp'
            # Namespace-specific netlink is authoritative; /sys may still show
            # the host namespace after setns.
            self.mac = json.loads(self.command(['ip', '-j', 'link', 'show', 'dev', self.iface]))[0]['address']
            self.address = dhcp_request(self.iface, bytes.fromhex(self.mac.replace(':', '')), self.dhcp_stats)
            self.command(['ip', 'addr', 'add', self.address + '/24', 'dev', self.iface])
            self.command(['ip', 'route', 'add', 'default', 'via', AP, 'dev', self.iface])
        # All DNS configuration remains local to this process; /etc/resolv.conf
        # is never edited, including its view inside the namespace.
        self.require_host_unchanged()
        self.stage = 'ap-readback'
        until = time.monotonic() + 10
        while True:
            try:
                self.http_status(check_client=True)
                break
            except ProbeError as error:
                if str(error) != 'AP_STATION_NOT_OBSERVED' or time.monotonic() >= until:
                    raise
                time.sleep(.4)

    def require_host_unchanged(self):
        require(self.host_snapshot() == self.before, 'HOST_NETWORK_CHANGED')

    def named_namespace_owned(self):
        try:
            stat = os.stat('/run/netns/' + self.ns)
            return self.ns_identity == (stat.st_dev, stat.st_ino)
        except OSError:
            return False

    def namespace_target_owned(self):
        link = json.loads(self.command(['ip', '-j', 'link', 'show', 'dev', self.iface], namespace=True))
        info = self.command(['iw', 'dev', self.iface, 'info'], namespace=True)
        require(len(link) == 1 and link[0].get('address') == self.original_mac and '\twiphy ' + self.phy[3:] in info, 'FOREIGN_NAMESPACE_INTERFACE_PRESERVED')

    def stop_own_wpa(self):
        if self.wpa is None or self.wpa.poll() is not None:
            return
        require(self.wpa_pidfd is not None, 'OWN_WPA_PIDFD_UNAVAILABLE')
        try:
            state = os.waitid(os.P_PIDFD, self.wpa_pidfd, os.WEXITED | os.WNOHANG | os.WNOWAIT)
        except ChildProcessError:
            raise ProbeError('OWN_WPA_CHILD_IDENTITY_CHANGED')
        if state is None:
            signal.pidfd_send_signal(self.wpa_pidfd, signal.SIGTERM)
        try:
            self.wpa.wait(timeout=5)
        except subprocess.TimeoutExpired:
            # pidfd targets the original child, never a reused numeric PID.
            signal.pidfd_send_signal(self.wpa_pidfd, signal.SIGKILL)
            self.wpa.wait(timeout=3)

    def http_status(self, check_client=False):
        with self.in_namespace():
            connection = http.client.HTTPConnection(AP, 80, timeout=3, source_address=(self.address, 0))
            try:
                connection.request('GET', '/api/v1/network/hotspot')
                response = connection.getresponse()
                raw = response.read(131073)
                require(response.status == 200 and len(raw) <= 131072, 'AP_HTTP_FAILED')
                observation = json.loads(raw)
            finally:
                connection.close()
        require(observation.get('status') == 'available', 'AP_STATUS_UNAVAILABLE')
        data = observation['data']
        require(data.get('enabled') is True and data.get('dhcpReady') is True, 'AP_NOT_READY')
        if check_client:
            require(any(v.get('mac', '').lower() == self.mac and v.get('authorized') is True for v in data.get('clients', [])), 'AP_STATION_NOT_OBSERVED')
        return data

    def measure(self, kind):
        require(kind in ('wifi', 'cellular'), 'UPSTREAM_INVALID')
        started = datetime.datetime.now(datetime.timezone.utc).isoformat()
        before = self.http_status()['sharing']
        require(before.get('ready') is True and before.get('upstream', {}).get('kind') == kind, 'UPSTREAM_MISMATCH')
        with self.in_namespace():
            address = query_dns(self.iface, self.address)
            query_dns(self.iface, self.address, tcp=True)
            with bound_socket(self.iface, self.address, socket.SOCK_STREAM) as plain:
                plain.settimeout(6)
                plain.connect((address, 443))
                require(plain.getsockname()[0] == self.address and plain.getpeername() == (address, 443), 'TLS_SOCKET_IDENTITY_CHANGED')
                with ssl.create_default_context().wrap_socket(plain, server_hostname=HOST) as secure:
                    secure.sendall(b'GET / HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n')
                    data = b''
                    while b'\r\n\r\n' not in data and len(data) <= 32768:
                        part = secure.recv(4096)
                        require(part, 'HTTPS_RESPONSE_TRUNCATED')
                        data += part
                    require(len(data) <= 32768 and data.split(b'\r\n', 1)[0].split()[1:2] == [b'200'], 'HTTPS_STATUS_FAILED')
        after = self.http_status()['sharing']
        require(after.get('ready') is True and after.get('upstream', {}).get('kind') == kind and after.get('upstream', {}).get('interface') == before['upstream'].get('interface'), 'UPSTREAM_CHANGED')
        self.require_host_unchanged()
        return dict(passed=True, upstream=kind, dnsUDP=True, dnsTCP=True, standardTLSHTTPS=True, interfaceBound=True, trafficStartedAt=started, trafficCompletedAt=datetime.datetime.now(datetime.timezone.utc).isoformat())

    def cleanup(self):
        errors = []
        def attempt(action):
            try:
                action()
            except Exception as error:
                errors.append(str(error) if isinstance(error, ProbeError) else 'RESTORE_STEP_FAILED')
        attempt(self.leave)
        attempt(self.stop_own_wpa)
        if self.moved:
            # Pinned namespace FD is authoritative even if someone replaced the
            # name. No operation is run inside a foreign replacement namespace.
            namespace_target_owned = True
            try:
                self.namespace_target_owned()
            except Exception:
                namespace_target_owned = False
                errors.append('NAMESPACE_TARGET_OWNERSHIP_UNCONFIRMED')
            if namespace_target_owned:
                attempt(lambda: self.command(['ip', 'addr', 'flush', 'dev', self.iface], namespace=True))
                attempt(lambda: self.command(['ip', 'link', 'set', self.iface, 'down'], namespace=True))
                # Name lookup through /sys would still see the host namespace.
                # iw's numeric PHY selector uses netlink without that lookup.
                attempt(lambda: self.command(['iw', 'phy#' + self.phy[3:], 'set', 'netns', str(self.hostpid)], namespace=True))
        if self.created:
            if self.named_namespace_owned():
                attempt(lambda: self.command(['ip', 'netns', 'delete', self.ns]))
            else:
                errors.append('FOREIGN_NAMESPACE_NAME_PRESERVED')
        target_owned = True
        if self.unmanaged:
            try:
                self.identity()
                # A newly observed external connection is not ours to undo.
                state = self.nm('STATE')
                require(not state.startswith(('100 ', '70 ', '60 ', '50 ', '40 ')), 'TARGET_FOREIGN_CONNECTION')
            except Exception:
                target_owned = False
                errors.append('FOREIGN_NM_STATE_PRESERVED')
        if self.unmanaged and target_owned:
            attempt(lambda: self.command(['nmcli', 'device', 'set', self.iface, 'managed', 'yes']))
        if self.autoconnect_changed and target_owned:
            attempt(lambda: self.command(['nmcli', 'device', 'set', self.iface, 'autoconnect', self.original_autoconnect]))
        if self.unmanaged and target_owned:
            attempt(lambda: self.command(['ip', 'link', 'set', self.iface, 'up' if self.original_up else 'down']))
            attempt(self.identity)
            def verify_nm():
                until = time.monotonic() + 8
                while time.monotonic() < until:
                    if self.nm('NM-MANAGED') == 'yes' and self.nm('STATE').startswith('30 ') and self.nm('AUTOCONNECT') == self.original_autoconnect:
                        return
                    time.sleep(.2)
                raise ProbeError('NM_RESTORE_UNCONFIRMED')
            attempt(verify_nm)
            def verify_link():
                links = json.loads(self.command(['ip', '-j', 'addr', 'show', 'dev', self.iface]))
                require(len(links) == 1 and not links[0].get('addr_info') and ('UP' in links[0]['flags']) == self.original_up, 'LINK_RESTORE_UNCONFIRMED')
                require(links[0]['address'] == self.original_mac and links[0]['mtu'] == self.original_mtu and self.target_routes() == self.original_target_routes, 'TARGET_STATE_CHANGED')
                require('Not connected.' in self.command(['iw', 'dev', self.iface, 'link']) and '\ttype managed' in self.command(['iw', 'dev', self.iface, 'info']), 'TARGET_MODE_CHANGED')
            attempt(verify_link)
        if hasattr(self, 'before'):
            attempt(self.require_host_unchanged)
        # Remove only entries in our freshly-created, root-private tmpfs tree.
        if self.runtime and not errors:
            import shutil
            attempt(lambda: shutil.rmtree(self.runtime))
        if self.lock:
            self.lock.close()
        if self.wpa_pidfd is not None:
            os.close(self.wpa_pidfd)
        if self.testns is not None:
            os.close(self.testns)
        os.close(self.hostns)
        self.cleanup_errors = errors
        return not errors


def read_gate(timeout):
    ready, _, _ = select.select([sys.stdin], [], [], timeout)
    require(ready, 'COORDINATOR_GATE_TIMEOUT')
    line = sys.stdin.readline(4097)
    require(line and len(line) <= 4096, 'COORDINATOR_LOST')
    return json.loads(line)


def safe_diagnostic(probe, error):
    # No exception text, locals, source lines, command argv, SSID or credential
    # is copied into diagnostics, including failures before /run creation.
    return dict(stage=getattr(probe, 'stage', 'unknown'), exceptionType=type(error).__name__, dhcpStats=getattr(probe, 'dhcp_stats', {}),
                frames=[dict(file=pathlib.Path(frame.filename).name, function=frame.name, line=frame.lineno)
                        for frame in traceback.extract_tb(error.__traceback__)][-12:])


def main(payload):
    os.umask(0o077)
    probe = RemoteProbe(payload)
    if payload.get('preflightOnly') is True:
        try:
            probe.preflight()
            probe.emit('preflight', passed=True, readOnly=True)
        finally:
            os.close(probe.hostns)
        return
    result = dict(passed=False)
    prepared = cleanup_confirmed = cleanup_gate_received = False
    for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(signum, lambda *_: (_ for _ in ()).throw(ProbeError('TERMINATED')))
    signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(ProbeError('WATCHDOG_TIMEOUT')))
    signal.alarm(240)
    try:
        probe.start()
        probe.emit('prepared', prepared=True, association=True, dhcpGateway=True, dhcpDNS=True, httpUI=True, clientObserved=True)
        prepared = True
        gate = read_gate(90)
        require(gate.get('proceed') is True, 'MEASUREMENT_NOT_AUTHORIZED')
        if gate.get('localOnly') is True:
            require(gate.get('expectedUpstream') in (None, ''), 'LOCAL_ONLY_UPSTREAM_CONFLICT')
            result = dict(passed=True, scope='ap-lan-only', association=True, dhcpGateway=True, dhcpDNS=True, httpUI=True, clientObserved=True)
        else:
            expires = datetime.datetime.fromisoformat(gate.get('expiresAt', '').replace('Z', '+00:00'))
            require(expires.tzinfo is not None, 'MEASUREMENT_EXPIRY_REQUIRED')
            remaining = (expires - datetime.datetime.now(datetime.timezone.utc)).total_seconds()
            require(1 < remaining <= 60, 'MEASUREMENT_WINDOW_INVALID')
            signal.alarm(max(1, min(12, int(remaining))))
            try:
                result = probe.measure(gate.get('expectedUpstream'))
                require(datetime.datetime.now(datetime.timezone.utc) < expires, 'MEASUREMENT_EXPIRED')
            finally:
                signal.alarm(120)
        probe.emit('traffic', **result)
        gate = read_gate(110)
        cleanup_gate_received = True
        require(gate.get('cleanup') is True and gate.get('restored') is True, 'UPSTREAM_RESTORE_UNCONFIRMED')
        cleanup_confirmed = True
    except Exception as error:
        result = dict(passed=False, code=str(error) if isinstance(error, ProbeError) else 'USB_PROBE_FAILED', diagnostic=safe_diagnostic(probe, error))
        probe.emit('failure', **result)
        if prepared and not cleanup_confirmed and not cleanup_gate_received:
            try:
                signal.alarm(115)
                gate = read_gate(110)
                require(gate.get('cleanup') is True and gate.get('restored') is True, 'UPSTREAM_RESTORE_UNCONFIRMED')
            except Exception:
                pass
    finally:
        signal.alarm(0)
        for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
            signal.signal(signum, signal.SIG_IGN)
        restored = probe.cleanup()
        probe.emit('complete', passed=result['passed'] and restored, restored=restored, needsReview=not restored, cleanupErrors=getattr(probe, 'cleanup_errors', []))
