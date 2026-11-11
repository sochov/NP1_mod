//go:build linux

package modem

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

const nativeAPNOwnerFile = "/usr/local/bin/NEventWatcherCom"
const nativeAPNOwnerHash = "3a57830dbc5e3ad26355bb14ca28141f8e82eba94b10b8b619dabea60901ec06"

var nativeAPNSequence atomic.Uint32

type nativeAPNOwner struct {
	pid, uid       uint32
	start, address string
}

func nativeAPNFileHash(path, want string) error {
	f, e := os.Open(path)
	if e != nil {
		return errors.New("native APN fingerprint unreadable")
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > 4*1024*1024 {
		return errors.New("native APN fingerprint file invalid")
	}
	h := sha256.New()
	if _, e = io.Copy(h, io.LimitReader(f, 4*1024*1024+1)); e != nil || hex.EncodeToString(h.Sum(nil)) != want {
		return errors.New("native APN fingerprint mismatch")
	}
	return nil
}
func nativeAPNReadBounded(path string, maximum int64) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, maximum+1))
	if e != nil || int64(len(raw)) > maximum {
		return nil, errors.New("native owner metadata exceeds bound")
	}
	return raw, nil
}
func nativeAPNProcess(pid uint32) (nativeAPNOwner, error) {
	base := filepath.Join("/proc", strconv.FormatUint(uint64(pid), 10))
	exe, e := os.Readlink(base + "/exe")
	if e != nil || exe != nativeAPNOwnerFile {
		return nativeAPNOwner{}, errors.New("native owner executable changed")
	}
	raw, e := nativeAPNReadBounded(base+"/stat", 4096)
	if e != nil {
		return nativeAPNOwner{}, errors.New("native owner stat unavailable")
	}
	end := bytes.LastIndex(raw, []byte(") "))
	if end < 0 {
		return nativeAPNOwner{}, errors.New("native owner stat invalid")
	}
	fields := strings.Fields(string(raw[end+2:]))
	if len(fields) < 20 {
		return nativeAPNOwner{}, errors.New("native owner stat invalid")
	}
	start := fields[19]
	if _, e = strconv.ParseUint(start, 10, 64); e != nil {
		return nativeAPNOwner{}, errors.New("native owner start time invalid")
	}
	raw, e = nativeAPNReadBounded(base+"/status", 16384)
	if e != nil {
		return nativeAPNOwner{}, errors.New("native owner credentials unavailable")
	}
	var uid uint32
	found := false
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "Uid:") {
			f := strings.Fields(line)
			if len(f) != 5 {
				return nativeAPNOwner{}, errors.New("native owner credentials invalid")
			}
			v, e := strconv.ParseUint(f[1], 10, 32)
			if e != nil || f[1] != f[2] || f[1] != f[3] || f[1] != f[4] {
				return nativeAPNOwner{}, errors.New("native owner credentials invalid")
			}
			uid = uint32(v)
			found = true
		}
	}
	if !found {
		return nativeAPNOwner{}, errors.New("native owner credentials missing")
	}
	return nativeAPNOwner{pid: pid, uid: uid, start: start}, nil
}
func discoverNativeAPNOwner() (nativeAPNOwner, error) {
	for _, fingerprint := range []struct{ path, hash string }{
		{nativeAPNOwnerFile, nativeAPNOwnerHash},
		{"/usr/local/lib/libNDeviceLibCom.so", "2b142e70f34103a59333808de36699cc064e602fae2524d960664c1f3ceeca71"},
		{"/usr/local/lib/libNEventBaseLib.so", "a85381c4b74aad21e83e19cab9cfa5e2e7d5b6f5c5cc98593afab85e9c19efa1"},
		{"/usr/lib/liblteapi.so.0.0.0", "da4fbea491e90625202e851e90524745216ed652fe188b49ed41b3ed6c450245"},
	} {
		if e := nativeAPNFileHash(fingerprint.path, fingerprint.hash); e != nil {
			return nativeAPNOwner{}, e
		}
	}
	entries, e := os.ReadDir("/proc")
	if e != nil || len(entries) > 32768 {
		return nativeAPNOwner{}, errors.New("native owner inventory unavailable")
	}
	var owner nativeAPNOwner
	count := 0
	for _, entry := range entries {
		pid, e := strconv.ParseUint(entry.Name(), 10, 32)
		if e != nil {
			continue
		}
		exe, e := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
		if e != nil || exe != nativeAPNOwnerFile {
			continue
		}
		owner, e = nativeAPNProcess(uint32(pid))
		if e != nil {
			return nativeAPNOwner{}, e
		}
		count++
	}
	if count != 1 {
		return nativeAPNOwner{}, errors.New("native communication owner is not unique")
	}
	raw, e := nativeAPNReadBounded(filepath.Join("/proc", strconv.FormatUint(uint64(owner.pid), 10), "environ"), 65536)
	if e != nil {
		return nativeAPNOwner{}, errors.New("native owner bus metadata unavailable")
	}
	defer clear(raw)
	count = 0
	for _, entry := range bytes.Split(raw, []byte{0}) {
		if bytes.HasPrefix(entry, []byte("DBUS_SESSION_BUS_ADDRESS=")) {
			count++
			owner.address, e = nativeAPNUnixAddress(string(bytes.TrimPrefix(entry, []byte("DBUS_SESSION_BUS_ADDRESS="))))
			if e != nil {
				return nativeAPNOwner{}, e
			}
		}
	}
	if count != 1 {
		return nativeAPNOwner{}, errors.New("native owner bus metadata missing")
	}
	return owner, nil
}

// The generic transport does not negotiate Unix FD passing. Validate each
// binary D-Bus frame's size before godbus allocates or decodes its body.
type nativeAPNBoundedConn struct {
	net.Conn
	framed    atomic.Bool
	header    [16]byte
	pending   []byte
	remaining uint64
}

func (c *nativeAPNBoundedConn) Write(p []byte) (int, error) {
	n, e := c.Conn.Write(p)
	// godbus starts its read goroutine inside Auth, before Auth returns. Switch
	// here, on the final complete authentication line, before that can happen.
	if e == nil && n == len(p) && bytes.Equal(p, []byte("BEGIN\r\n")) {
		c.framed.Store(true)
	}
	return n, e
}

func (c *nativeAPNBoundedConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if !c.framed.Load() {
		return c.Conn.Read(p)
	}
	if len(c.pending) == 0 && c.remaining == 0 {
		if _, e := io.ReadFull(c.Conn, c.header[:]); e != nil {
			return 0, e
		}
		var order binary.ByteOrder
		switch c.header[0] {
		case 'l':
			order = binary.LittleEndian
		case 'B':
			order = binary.BigEndian
		default:
			return 0, errors.New("native D-Bus byte order invalid")
		}
		head := uint64(order.Uint32(c.header[12:]))
		body := uint64(order.Uint32(c.header[4:]))
		total := 16 + ((head + 7) &^ 7) + body
		if c.header[3] != 1 || total > 65536 {
			return 0, errors.New("native D-Bus frame exceeds bound")
		}
		c.pending = c.header[:]
		c.remaining = total - 16
	}
	if len(c.pending) > 0 {
		n := copy(p, c.pending)
		c.pending = c.pending[n:]
		return n, nil
	}
	if uint64(len(p)) > c.remaining {
		p = p[:c.remaining]
	}
	n, e := c.Conn.Read(p)
	c.remaining -= uint64(n)
	return n, e
}

func nativeAPNBusOwner(ctx context.Context, conn *dbus.Conn) (string, error) {
	var name string
	if e := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", dbus.FlagNoAutoStart, nativeAPNStub).Store(&name); e != nil || !strings.HasPrefix(name, ":") {
		return "", errors.New("native communication D-Bus owner unavailable")
	}
	return name, nil
}
func nativeAPNPeerMatches(ctx context.Context, conn *dbus.Conn, name string, owner nativeAPNOwner) bool {
	var pid, uid uint32
	if conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetConnectionUnixProcessID", dbus.FlagNoAutoStart, name).Store(&pid) != nil {
		return false
	}
	if conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetConnectionUnixUser", dbus.FlagNoAutoStart, name).Store(&uid) != nil {
		return false
	}
	return pid == owner.pid && uid == owner.uid
}

// ReadNativeAPNStatus performs exactly one stock-owner read operation (0x0a).
// It never loads vendor constructors, claims a Proxy name, opens AT/QMI, writes
// an APN profile, or accepts externally supplied bus addresses/commands.
func ReadNativeAPNStatus(parent context.Context) (NativeAPNStatus, error) {
	return nativeAPNRPC(parent, nil)
}

// A nonnil profile is an internal, fully validated profile1-only connect.
// Public read operations cannot supply a payload or choose an operation.
func nativeAPNRPC(parent context.Context, profile *savedAPNProfile) (NativeAPNStatus, error) {
	out := NativeAPNStatus{Source: "stock-npmcom-owner-status"}
	connect := profile != nil
	sequence := nativeAPNSequence.Add(1)
	if sequence == 0 {
		sequence = nativeAPNSequence.Add(1)
	}
	command := uint32(0x0a)
	request := nativeAPNGetRequest()
	if connect {
		command = 0x0b
		var e error
		request, e = nativeAPNProfile1Request(*profile)
		if e != nil {
			return out, e
		}
		out.Source = "stock-npmcom-owner-connect-result"
	}
	defer clear(request)
	binary.LittleEndian.PutUint32(request, sequence)
	if os.Geteuid() != 0 {
		return out, errors.New("native APN status requires root")
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	owner, e := discoverNativeAPNOwner()
	if e != nil {
		return out, e
	}
	dialer := net.Dialer{}
	raw, e := dialer.DialContext(ctx, "unix", owner.address)
	if e != nil {
		return out, errors.New("native owner bus connection failed")
	}
	defer raw.Close()
	deadline, _ := ctx.Deadline()
	if e = raw.SetDeadline(deadline); e != nil {
		return out, errors.New("native bus deadline unavailable")
	}
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()
	unixConnection, ok := raw.(*net.UnixConn)
	if !ok {
		return out, errors.New("native bus is not Unix")
	}
	control, e := unixConnection.SyscallConn()
	if e != nil {
		return out, errors.New("native bus peer unavailable")
	}
	var peer *unix.Ucred
	var peerError error
	if e = control.Control(func(fd uintptr) { peer, peerError = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); e != nil || peerError != nil || peer == nil || (peer.Uid != 0 && peer.Uid != owner.uid) {
		return out, errors.New("native bus peer credentials invalid")
	}
	bounded := &nativeAPNBoundedConn{Conn: raw}
	conn, e := dbus.NewConn(bounded, dbus.WithContext(ctx))
	if e != nil {
		return out, errors.New("native bus client creation failed")
	}
	defer conn.Close()
	if e = conn.Auth([]dbus.Auth{dbus.AuthExternal("0")}); e != nil {
		return out, errors.New("native bus authentication failed")
	}
	if !bounded.framed.Load() {
		return out, errors.New("native bus authentication framing unavailable")
	}
	if e = conn.Hello(); e != nil {
		return out, errors.New("native bus Hello failed")
	}
	unique, e := nativeAPNBusOwner(ctx, conn)
	if e != nil {
		return out, e
	}
	if !nativeAPNPeerMatches(ctx, conn, unique, owner) {
		return out, errors.New("native RPC owner identity mismatch")
	}
	signals := make(chan *dbus.Signal, 32)
	conn.Signal(signals)
	defer conn.RemoveSignal(signals)
	if e = conn.AddMatchSignal(dbus.WithMatchObjectPath(nativeAPNEventPath), dbus.WithMatchInterface(nativeAPNInterface), dbus.WithMatchMember("SPFEvent")); e != nil {
		return out, errors.New("native status signal subscription failed")
	}
	current, e := nativeAPNProcess(owner.pid)
	if e != nil || current.start != owner.start {
		return out, errors.New("native owner changed before request")
	}
	requestCtx, requestCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	call := conn.Object(unique, nativeAPNPath).CallWithContext(requestCtx, nativeAPNInterface+".CallFunction", dbus.FlagNoAutoStart, uint32(1), uint32(os.Getpid()), command, []uint32{}, request, uint32(1))
	requestCancel()
	if call.Err != nil {
		return out, errors.New("native APN status call failed")
	}
	if !nativeAPNMethodAccepted(call.Body) {
		return out, errors.New("native APN status was not accepted")
	}
	out.Accepted = true
	verifiedSenders := map[string]bool{}
	var acknowledgedAt, completedAt uint32
	received := 0
	for {
		select {
		case <-ctx.Done():
			return out, errors.New("native APN status reply timed out")
		case signal, open := <-signals:
			if !open {
				return out, errors.New("native bus disconnected")
			}
			received++
			if received > 128 {
				return out, errors.New("native status signal count exceeds bound")
			}
			if signal.Path != nativeAPNEventPath || signal.Name != nativeAPNSignal {
				continue
			}
			if !verifiedSenders[signal.Sender] {
				if !strings.HasPrefix(signal.Sender, ":") || len(verifiedSenders) > 8 || !nativeAPNPeerMatches(ctx, conn, signal.Sender, owner) {
					return out, errors.New("native status signal owner mismatch")
				}
				verifiedSenders[signal.Sender] = true
			}
			kind, value, e := parseNativeAPNReply(signal.Body, uint32(os.Getpid()), sequence, connect)
			if e != nil {
				return out, e
			}
			switch kind {
			case "result":
				acknowledgedAt = nativeAPNEventClock(signal.Body)
				out.ResultReceived = true
				v := value[0]
				out.ResultCode = &v
				if v != 0 {
					return out, errors.New("native status result rejected")
				}
			case "status":
				completedAt = nativeAPNEventClock(signal.Body)
				a, b := value[0], value[1]
				out.Profile1State = &a
				out.Profile2State = &b
			}
			if out.ResultReceived && out.Profile1State != nil {
				if connect {
					delay, valid := nativeAPNConnectClockDelay(acknowledgedAt, completedAt)
					if !valid {
						out.Profile1State = nil
						out.Profile2State = nil
						continue
					}
					out.CompletionDelayMillis = &delay
				}
				after, e := nativeAPNBusOwner(ctx, conn)
				if e != nil || after != unique {
					return out, errors.New("native owner changed after request")
				}
				current, e = nativeAPNProcess(owner.pid)
				if e != nil || current.start != owner.start {
					return out, errors.New("native process changed after request")
				}
				out.ObservedAt = time.Now().UTC()
				return out, nil
			}
		}
	}
}
