// Package stockbt drives the stock NP1 Bluetooth stack (NEventWatcherCom)
// through the handlers it publishes on the stock session D-Bus. The kernel has
// no Bluetooth; the stock process owns the controller, so classic BT pairing,
// RFCOMM connections and SPP data all go through these calls.
//
// Request layouts come from the stock proxies in libNDeviceLibCom.so
// (NPBTManagerHandlerProxy, NPBTSPPHandlerProxy); see
// docs/android-auto-bluetooth-20261126.md.
package stockbt

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	busFile         = "/tmp/dbus_session_bus_address"
	ownerExecutable = "/usr/local/bin/NEventWatcherCom"
	iface           = "SPF.EventBase.v1_0"

	managerHandler = "NPBTManagerHandler"
	sppHandler     = "NPBTSPPHandler"
)

// Event IDs at +8 of every stock event (the D-Bus path is Evt<decimal id>).
const (
	eventManager   = 0x2a
	eventSPPStatus = 0x210
	eventSPPData   = 0x211
)

// Manager event kinds (u32 at +0x2c of a manager event).
const (
	KindConfirmIncoming = 0x040d // SSP numeric comparison, the peer started pairing
	KindPairingDone     = 0x080d
	KindPairingFailed   = 0x0805
	KindConfirmOutgoing = 0x1405 // SSP numeric comparison while NP1 connects
	KindConnectResult   = 0x2081
)

// Profile codes of the manager's Connect request. The vendor SPP slots get
// their UUIDs from the stock profile table (SetProfile).
const (
	ProfileVSSPP1 = 0x0a
	ProfileVSSPP2 = 0x0b
	ProfileVSSPP3 = 0x0c
	ProfileVSSPP4 = 0x0d
	ProfileVSSPP5 = 0x0e
)

// Addr is a BD address in the byte order the stock events and requests use
// (the order it is written in, 64:49:7D:... is 64 49 7d ...).
type Addr [6]byte

// ParseAddr reads "64:49:7D:E0:DB:42".
func ParseAddr(s string) (Addr, error) {
	var a Addr
	parts := strings.Split(s, ":")
	if len(parts) != 6 {
		return a, fmt.Errorf("stockbt: bad address %q", s)
	}
	for i, p := range parts {
		v, err := strconv.ParseUint(p, 16, 8)
		if err != nil || len(p) != 2 {
			return a, fmt.Errorf("stockbt: bad address %q", s)
		}
		a[i] = byte(v)
	}
	return a, nil
}

func (a Addr) String() string {
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", a[0], a[1], a[2], a[3], a[4], a[5])
}

// Event is one stock event this client cares about.
type Event struct {
	ID   uint32 // eventManager, eventSPPStatus or eventSPPData
	To   uint32 // addressed process (0 for broadcasts)
	Kind uint32 // manager events: the kind at +0x2c
	Addr Addr   // manager events: the peer at +0x30
	Data []byte // whole event body
}

// Manager reports a manager event.
func (e Event) Manager() bool { return e.ID == eventManager }

// Client is one connection to the stock session bus.
type Client struct {
	conn    *dbus.Conn
	owner   string
	pid     uint32
	caller  uint32
	seq     atomic.Uint32
	manager dbus.BusObject
	spp     dbus.BusObject

	mu   sync.Mutex
	subs map[chan Event]struct{}
	done chan struct{}
}

// Dial connects to the stock bus and checks that the handlers belong to the
// stock NEventWatcherCom.
func Dial(ctx context.Context) (*Client, error) {
	raw, err := os.ReadFile(busFile)
	if err != nil {
		return nil, fmt.Errorf("stockbt: stock session bus: %w", err)
	}
	addr := busAddress(string(raw))
	if addr == "" {
		return nil, errors.New("stockbt: stock session bus address not found")
	}
	conn, err := dbus.Connect(addr, dbus.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("stockbt: %w", err)
	}
	c := &Client{conn: conn, caller: uint32(os.Getpid()), subs: map[chan Event]struct{}{}, done: make(chan struct{})}
	c.seq.Store(uint32(time.Now().UnixNano()>>10) & 0xffff)
	if err = c.resolve(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	if err = conn.AddMatchSignalContext(ctx, dbus.WithMatchInterface(iface), dbus.WithMatchMember("SPFEvent")); err != nil {
		conn.Close()
		return nil, fmt.Errorf("stockbt: subscribe: %w", err)
	}
	signals := make(chan *dbus.Signal, 256)
	conn.Signal(signals)
	go c.dispatch(signals)
	return c, nil
}

func busAddress(contents string) string {
	for _, line := range strings.Split(contents, "\n") {
		if v, ok := strings.CutPrefix(line, "DBUS_SESSION_BUS_ADDRESS='"); ok {
			a, _, _ := strings.Cut(v, "'")
			return a
		}
	}
	return ""
}

func (c *Client) resolve(ctx context.Context) error {
	var owner string
	if err := c.conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, "SPF.EventBase."+managerHandler+".Stub").Store(&owner); err != nil {
		return fmt.Errorf("stockbt: manager handler: %w", err)
	}
	var sppOwner string
	if err := c.conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, "SPF.EventBase."+sppHandler+".Stub").Store(&sppOwner); err != nil {
		return fmt.Errorf("stockbt: SPP handler: %w", err)
	}
	var pid uint32
	if err := c.conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetConnectionUnixProcessID", 0, owner).Store(&pid); err != nil {
		return fmt.Errorf("stockbt: handler owner: %w", err)
	}
	if exe, _ := os.Readlink("/proc/" + strconv.FormatUint(uint64(pid), 10) + "/exe"); exe != ownerExecutable {
		return fmt.Errorf("stockbt: handler owner is %q, not the stock process", exe)
	}
	c.owner, c.pid = owner, pid
	c.manager = c.conn.Object(owner, dbus.ObjectPath("/SPF/EventBase/"+managerHandler))
	c.spp = c.conn.Object(sppOwner, dbus.ObjectPath("/SPF/EventBase/"+sppHandler))
	return nil
}

// PID is the stock process.
func (c *Client) PID() uint32 { return c.pid }

// Close ends the connection.
func (c *Client) Close() error {
	c.mu.Lock()
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	c.mu.Unlock()
	return c.conn.Close()
}

func (c *Client) dispatch(signals chan *dbus.Signal) {
	for s := range signals {
		if len(s.Body) != 3 {
			continue
		}
		to, _ := s.Body[0].(uint32)
		data, _ := s.Body[2].([]byte)
		if len(data) < 12 {
			continue
		}
		ev := Event{ID: binary.LittleEndian.Uint32(data[8:]), To: to, Data: data}
		switch ev.ID {
		case eventManager:
			if len(data) >= 0x36 {
				ev.Kind = binary.LittleEndian.Uint32(data[0x2c:])
				copy(ev.Addr[:], data[0x30:0x36])
			}
		case eventSPPStatus, eventSPPData:
		default:
			continue
		}
		c.mu.Lock()
		for ch := range c.subs {
			select {
			case ch <- ev:
			default: // a slow subscriber loses events rather than blocking the bus
			}
		}
		c.mu.Unlock()
	}
}

// Subscribe returns a channel of stock events until cancel is called.
func (c *Client) Subscribe(buffer int) (<-chan Event, func()) {
	ch := make(chan Event, buffer)
	c.mu.Lock()
	c.subs[ch] = struct{}{}
	c.mu.Unlock()
	return ch, func() {
		c.mu.Lock()
		if _, ok := c.subs[ch]; ok {
			delete(c.subs, ch)
			close(ch)
		}
		c.mu.Unlock()
	}
}

func (c *Client) callRaw(ctx context.Context, obj dbus.BusObject, fn uint32, payload []byte, resp uint32) ([]byte, error) {
	var result []byte
	var ret []uint32
	err := obj.CallWithContext(ctx, iface+".CallFunction", 0, uint32(1), c.caller, fn, []uint32{}, payload, resp).Store(&result, &ret)
	if err != nil {
		return nil, fmt.Errorf("stockbt: function 0x%x: %w", fn, err)
	}
	return result, nil
}

// managerRequest builds a manager request: u32 sequence, u32 0, u32 function,
// u32 reply number, u64 client, then the arguments.
func (c *Client) managerRequest(fn uint32, size int) []byte {
	p := make([]byte, size)
	binary.LittleEndian.PutUint32(p[0:], c.seq.Add(1))
	binary.LittleEndian.PutUint32(p[8:], fn)
	binary.LittleEndian.PutUint32(p[12:], fn-1)
	binary.LittleEndian.PutUint64(p[16:], 1)
	return p
}

// callManager sends a manager request; the stock reply is one byte, 1 when
// the request was accepted (the result arrives later as an event).
func (c *Client) callManager(ctx context.Context, p []byte) error {
	fn := binary.LittleEndian.Uint32(p[8:])
	r, err := c.callRaw(ctx, c.manager, fn, p, 1)
	if err != nil {
		return err
	}
	if len(r) < 1 || r[0] != 1 {
		return fmt.Errorf("stockbt: function 0x%x rejected (reply %x)", fn, r)
	}
	return nil
}

// Waiting sets the scan mode (2: discoverable and connectable).
func (c *Client) Waiting(ctx context.Context, mode uint32) error {
	p := c.managerRequest(0x09, 0x20)
	binary.LittleEndian.PutUint32(p[0x18:], mode)
	return c.callManager(ctx, p)
}

// CancelWaiting ends discoverable mode.
func (c *Client) CancelWaiting(ctx context.Context) error {
	return c.callManager(ctx, c.managerRequest(0x0a, 0x18))
}

// ReplyConfirm answers an SSP numeric comparison.
func (c *Client) ReplyConfirm(ctx context.Context, a Addr, accept bool) error {
	p := c.managerRequest(0x12, 0x20)
	copy(p[0x18:], a[:])
	if accept {
		p[0x1e] = 1
	}
	return c.callManager(ctx, p)
}

// SetLinkkey hands the stack a stored link key for a; the stack forgets keys
// on reboot, and the stock communication app only restores its own devices.
func (c *Client) SetLinkkey(ctx context.Context, a Addr, key [16]byte) error {
	p := c.managerRequest(0x10, 0x30)
	copy(p[0x18:], a[:])
	copy(p[0x1e:], key[:])
	return c.callManager(ctx, p)
}

// ClearLinkkey drops the stack's key for a.
func (c *Client) ClearLinkkey(ctx context.Context, a Addr) error {
	p := c.managerRequest(0x11, 0x20)
	copy(p[0x18:], a[:])
	return c.callManager(ctx, p)
}

// connectRequest is Connect (0x19) or Disconnect (0x1b): address, count and
// one 0x18-byte profile entry per profile (profile, 0xffff, u64 0, timeout in
// ms, 1). The stock proxy always sends room for the fixed array.
func (c *Client) connectRequest(fn uint32, a Addr, profiles []uint32) []byte {
	p := c.managerRequest(fn, 0x2c8+0x18*len(profiles))
	copy(p[0x18:], a[:])
	binary.LittleEndian.PutUint32(p[0x20:], uint32(len(profiles)))
	for i, prof := range profiles {
		e := p[0x24+0x18*i:]
		binary.LittleEndian.PutUint32(e[0:], prof)
		binary.LittleEndian.PutUint16(e[4:], 0xffff)
		binary.LittleEndian.PutUint32(e[0x10:], 10000)
		e[0x14] = 1
	}
	return p
}

// Connect asks the stack to connect the profiles of a. The result arrives as
// a KindConnectResult manager event.
func (c *Client) Connect(ctx context.Context, a Addr, profiles ...uint32) error {
	return c.callManager(ctx, c.connectRequest(0x19, a, profiles))
}

// Disconnect drops the profiles of a.
func (c *Client) Disconnect(ctx context.Context, a Addr, profiles ...uint32) error {
	return c.callManager(ctx, c.connectRequest(0x1b, a, profiles))
}

// MaxSPPWrite is the largest SPP write the stock handler takes.
const MaxSPPWrite = 0x7ed

// SPPWrite sends data on the connected vendor SPP profile. The request is
// u32 sequence, u32 0, u64 0, the data (room for 0x7f0 bytes), u32 length at
// +0x800 and u32 profile at +0x804.
func (c *Client) SPPWrite(ctx context.Context, profile uint32, data []byte) error {
	for len(data) > 0 {
		n := min(len(data), MaxSPPWrite)
		p := make([]byte, 0x808)
		binary.LittleEndian.PutUint32(p[0:], c.seq.Add(1))
		copy(p[0x10:], data[:n])
		binary.LittleEndian.PutUint32(p[0x800:], uint32(n))
		binary.LittleEndian.PutUint32(p[0x804:], profile)
		r, err := c.callRaw(ctx, c.spp, 0, p, 1)
		if err != nil {
			return err
		}
		if len(r) < 1 || r[0] != 1 {
			return fmt.Errorf("stockbt: SPP write rejected (reply %x)", r)
		}
		data = data[n:]
	}
	return nil
}

// SPPData is the payload of an SPP data event. The event carries a 0x40-byte
// header and then the stock buffer (0x7ed bytes); the header holds the
// length and the profile.
type SPPData struct {
	Profile uint32
	Data    []byte
}

// ParseSPPData decodes an SPP data event. Wire offsets: +0x20 and +0x30 are
// the two u32 fields of NPBTSppDataEvent (length and profile).
func ParseSPPData(e Event) (SPPData, bool) {
	if e.ID != eventSPPData || len(e.Data) < 0x40 {
		return SPPData{}, false
	}
	n := binary.LittleEndian.Uint32(e.Data[0x20:])
	prof := binary.LittleEndian.Uint32(e.Data[0x30:])
	body := e.Data[0x40:]
	if n > uint32(len(body)) {
		return SPPData{}, false
	}
	return SPPData{Profile: prof, Data: body[:n]}, true
}

// SPPStatus is an SPP status event: a flag at +0x20, the status at +0x24 and
// the profile at +0x28.
type SPPStatus struct {
	Flag    byte
	Status  uint32
	Profile uint32
}

// ParseSPPStatus decodes an SPP status event.
func ParseSPPStatus(e Event) (SPPStatus, bool) {
	if e.ID != eventSPPStatus || len(e.Data) < 0x2c {
		return SPPStatus{}, false
	}
	return SPPStatus{Flag: e.Data[0x20], Status: binary.LittleEndian.Uint32(e.Data[0x24:]), Profile: binary.LittleEndian.Uint32(e.Data[0x28:])}, true
}

// SPPConn is a byte stream over one vendor SPP profile.
type SPPConn struct {
	c       *Client
	profile uint32
	events  <-chan Event
	cancel  func()
	buf     []byte
	closed  chan struct{}
	once    sync.Once
}

// OpenSPP returns a stream over profile; it must already be connected.
func (c *Client) OpenSPP(profile uint32) *SPPConn {
	ev, cancel := c.Subscribe(64)
	return &SPPConn{c: c, profile: profile, events: ev, cancel: cancel, closed: make(chan struct{})}
}

func (s *SPPConn) Read(p []byte) (int, error) {
	for len(s.buf) == 0 {
		select {
		case <-s.closed:
			return 0, io.EOF
		case e, ok := <-s.events:
			if !ok {
				return 0, io.EOF
			}
			if d, ok := ParseSPPData(e); ok && d.Profile == s.profile {
				s.buf = append(s.buf, d.Data...)
			}
		}
	}
	n := copy(p, s.buf)
	s.buf = s.buf[n:]
	return n, nil
}

func (s *SPPConn) Write(p []byte) (int, error) {
	select {
	case <-s.closed:
		return 0, io.ErrClosedPipe
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.c.SPPWrite(ctx, s.profile, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close stops reading; it does not disconnect the profile.
func (s *SPPConn) Close() error {
	s.once.Do(func() { close(s.closed); s.cancel() })
	return nil
}
