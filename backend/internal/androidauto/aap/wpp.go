package aap

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// The WiFi Projection Protocol runs on the Bluetooth RFCOMM channel the head
// unit advertises with UUID 4de17a00-52cb-11e6-bdf4-0002a5d5c51b. The head unit
// hosts the WiFi network; the phone asks for its credentials, joins it and then
// opens the projection session over TCP to the address the head unit named.
//
// Frames are [payload length:2][type:2][protobuf payload], big endian.

// WirelessUUID is the RFCOMM service UUID of wireless projection on a head unit.
const WirelessUUID = "4de17a00-52cb-11e6-bdf4-0002a5d5c51b"

const (
	wppStartRequest   = 1
	wppInfoRequest    = 2
	wppInfoResponse   = 3
	wppVersionRequest = 4
	wppVersionResp    = 5
	wppConnectStatus  = 6
	wppStartResponse  = 7
	wppPingRequest    = 8
	wppPingResponse   = 9
)

// WifiCredentials is the head unit's network.
type WifiCredentials struct {
	SSID     string `json:"ssid"`
	Key      string `json:"-"`
	BSSID    string `json:"bssid"`
	Security int    `json:"security"`
	APType   int    `json:"apType"`
}

// WifiStart is where the projection session goes once the phone is on the network.
type WifiStart struct {
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

func writeWPP(w io.Writer, t uint16, payload []byte) error {
	if len(payload) > 0xffff {
		return errors.New("aap: WPP payload too large")
	}
	b := binary.BigEndian.AppendUint16(nil, uint16(len(payload)))
	b = binary.BigEndian.AppendUint16(b, t)
	_, e := w.Write(append(b, payload...))
	return e
}

func readWPP(r io.Reader) (uint16, []byte, error) {
	var h [4]byte
	if _, e := io.ReadFull(r, h[:]); e != nil {
		return 0, nil, e
	}
	p := make([]byte, binary.BigEndian.Uint16(h[:]))
	if _, e := io.ReadFull(r, p); e != nil {
		return 0, nil, e
	}
	return binary.BigEndian.Uint16(h[2:]), p, nil
}

// JoinFunc joins the head unit's network and returns the phone's address on it.
type JoinFunc func(ctx context.Context, c WifiCredentials) (localIP string, err error)

// PhoneWireless runs the phone side of the WiFi handshake on an RFCOMM stream.
// It returns once the phone is on the head unit's network, with the endpoint to
// open the projection session on.
func PhoneWireless(ctx context.Context, rw io.ReadWriter, join JoinFunc, logf func(string, ...any)) (WifiStart, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	stop := context.AfterFunc(ctx, func() {
		if c, ok := rw.(io.Closer); ok {
			c.Close()
		}
	})
	defer stop()
	var start WifiStart
	for {
		t, p, e := readWPP(rw)
		if e != nil {
			return WifiStart{}, fmt.Errorf("aap: wireless handshake: %w", e)
		}
		m, _ := pbDecode(p)
		switch t {
		case wppVersionRequest:
			major, _ := m.uint(1)
			minor, _ := m.uint(2)
			logf("wireless protocol %d.%d", major, minor)
			var w pbWriter
			w.uint(1, max(major, 1))
			w.uint(2, minor)
			w.int(4, 0)
			if e = writeWPP(rw, wppVersionResp, w.b); e != nil {
				return WifiStart{}, e
			}
		case wppStartRequest:
			start.IP = m.str(1)
			port, _ := m.uint(2)
			start.Port = int(port)
			logf("head unit projection endpoint %s:%d", start.IP, start.Port)
			if e = writeWPP(rw, wppInfoRequest, nil); e != nil {
				return WifiStart{}, e
			}
		case wppInfoResponse:
			if start.IP == "" {
				return WifiStart{}, errors.New("aap: credentials before the projection endpoint")
			}
			sec, _ := m.uint(4)
			apType, _ := m.uint(5)
			creds := WifiCredentials{SSID: m.str(1), Key: m.str(2), BSSID: m.str(3), Security: int(sec), APType: int(apType)}
			logf("joining head unit network %q", creds.SSID)
			ip, err := join(ctx, creds)
			var w pbWriter
			if err != nil {
				w.int(1, -1)
				_ = writeWPP(rw, wppConnectStatus, w.b)
				return WifiStart{}, fmt.Errorf("aap: joining %q: %w", creds.SSID, err)
			}
			w.string(1, ip)
			w.int(3, 0)
			if e = writeWPP(rw, wppStartResponse, w.b); e != nil {
				return WifiStart{}, e
			}
			w = pbWriter{}
			w.int(1, 0)
			if e = writeWPP(rw, wppConnectStatus, w.b); e != nil {
				return WifiStart{}, e
			}
			return start, nil
		case wppPingRequest:
			if e = writeWPP(rw, wppPingResponse, p); e != nil {
				return WifiStart{}, e
			}
		}
	}
}

// KeepWireless answers keepalives on the RFCOMM stream until it closes.
func KeepWireless(ctx context.Context, rw io.ReadWriter) error {
	stop := context.AfterFunc(ctx, func() {
		if c, ok := rw.(io.Closer); ok {
			c.Close()
		}
	})
	defer stop()
	for {
		t, p, e := readWPP(rw)
		if e != nil {
			return e
		}
		if t == wppPingRequest {
			if e = writeWPP(rw, wppPingResponse, p); e != nil {
				return e
			}
		}
	}
}
