package modem

import (
	"encoding/binary"
	"errors"
	"net/netip"
)

// This is a separate capability from the read-only observer allowlist. Every
// operation is fixed; no public API accepts a service, profile, mux or raw TLV.
type ownedDataOperation uint8

const (
	ownedBindMux ownedDataOperation = iota + 1
	ownedSetIPv4
	ownedStatusBefore
	ownedStart
	ownedCurrent
	ownedStop
	ownedStatusAfter
)

type ownedPacketHandle struct{ value uint32 }

func (ownedPacketHandle) String() string               { return "[private own packet handle]" }
func (v ownedPacketHandle) GoString() string           { return v.String() }
func (ownedPacketHandle) MarshalJSON() ([]byte, error) { return []byte(`{"private":true}`), nil }

func ownedOperationIDs(op ownedDataOperation) (uint16, uint16, error) {
	switch op {
	case ownedBindMux:
		return 101, 0x00a2, nil
	case ownedSetIPv4:
		return 102, 0x004d, nil
	case ownedStatusBefore:
		return 103, 0x0022, nil
	case ownedStart:
		return 104, 0x0020, nil
	case ownedCurrent:
		return 105, 0x002d, nil
	case ownedStop:
		return 106, 0x0021, nil
	case ownedStatusAfter:
		return 107, 0x0022, nil
	}
	return 0, 0, errors.New("OWNED_DATA_OPERATION_NOT_ALLOWED")
}

func ownedDataRequest(op ownedDataOperation, handle ownedPacketHandle) ([]byte, error) {
	tx, message, e := ownedOperationIDs(op)
	if e != nil {
		return nil, e
	}
	var payload []byte
	switch op {
	case ownedBindMux:
		// Endpoint HSUSB, interface 4; own child usb0.2/mux 0x82 only.
		payload = []byte{0x10, 8, 0, 2, 0, 0, 0, 4, 0, 0, 0, 0x11, 1, 0, 0x82, 0x13, 4, 0, 1, 0, 0, 0}
	case ownedSetIPv4:
		payload = []byte{1, 1, 0, 4}
	case ownedStart:
		// Reuse saved 3GPP profile 1. No APN, username, password, auth,
		// autoconnect, DNS, extended technology or profile modification.
		payload = []byte{0x19, 1, 0, 4, 0x30, 1, 0, 1, 0x31, 1, 0, 1}
	case ownedCurrent:
		// Profile ID, DNS, IPv4, gateway/mask, MTU, family. In particular,
		// APN/name/username/auth are not requested for this transport proof.
		payload = []byte{0x10, 4, 0, 0x11, 0xa3, 0, 0}
	case ownedStop:
		if handle.value == 0 || handle.value == 0xffffffff {
			return nil, errors.New("OWNED_DATA_STOP_HANDLE_UNVERIFIED")
		}
		payload = []byte{1, 4, 0, 0, 0, 0, 0}
		binary.LittleEndian.PutUint32(payload[3:], handle.value)
	}
	frame := []byte{0, byte(tx), byte(tx >> 8), byte(message), byte(message >> 8), byte(len(payload)), byte(len(payload) >> 8)}
	return append(frame, payload...), nil
}

func parseOwnedDataResponse(op ownedDataOperation, frame []byte) (map[byte][]byte, error) {
	tx, message, e := ownedOperationIDs(op)
	if e != nil {
		return nil, e
	}
	return parseQMIResponse(frame, tx, message)
}

func parseOwnedPacketHandle(frame []byte) (ownedPacketHandle, error) {
	values, e := parseOwnedDataResponse(ownedStart, frame)
	if e != nil {
		return ownedPacketHandle{}, e
	}
	v, ok := values[1]
	if !ok || len(v) != 4 {
		return ownedPacketHandle{}, errors.New("OWNED_DATA_START_HANDLE_MISSING")
	}
	h := ownedPacketHandle{binary.LittleEndian.Uint32(v)}
	if h.value == 0 || h.value == 0xffffffff {
		return ownedPacketHandle{}, errors.New("OWNED_DATA_START_HANDLE_INVALID")
	}
	return h, nil
}

func parseOwnedConnectionStatus(op ownedDataOperation, frame []byte) (byte, error) {
	if op != ownedStatusBefore && op != ownedStatusAfter {
		return 0, errors.New("OWNED_DATA_STATUS_OPERATION_INVALID")
	}
	values, e := parseOwnedDataResponse(op, frame)
	if e != nil {
		return 0, e
	}
	v, ok := values[1]
	if !ok || len(v) != 1 || v[0] < 1 || v[0] > 4 {
		return 0, errors.New("OWNED_DATA_STATUS_INVALID")
	}
	return v[0], nil
}

type ownedCurrentSettings struct {
	address, gateway netip.Addr
	prefixBits       int
	dns              []netip.Addr
	mtu              uint32
}

func (ownedCurrentSettings) String() string               { return "[private own data settings]" }
func (v ownedCurrentSettings) GoString() string           { return v.String() }
func (ownedCurrentSettings) MarshalJSON() ([]byte, error) { return []byte(`{"private":true}`), nil }

// QMI IPv4 values are little-endian uint32, whose numerical value is the IPv4
// address in network order. They are not four on-wire network-order bytes.
func ownedIPv4(raw []byte) (netip.Addr, error) {
	if len(raw) != 4 {
		return netip.Addr{}, errors.New("OWNED_DATA_IPV4_LENGTH_INVALID")
	}
	v := binary.LittleEndian.Uint32(raw)
	addr := netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
	if !addr.IsGlobalUnicast() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return netip.Addr{}, errors.New("OWNED_DATA_IPV4_INVALID")
	}
	return addr, nil
}

func parseOwnedCurrentSettings(frame []byte) (ownedCurrentSettings, error) {
	var out ownedCurrentSettings
	values, e := parseOwnedDataResponse(ownedCurrent, frame)
	if e != nil {
		return out, e
	}
	if p := values[0x1f]; len(p) != 2 || p[0] != 0 || p[1] != 1 {
		return out, errors.New("OWNED_DATA_CURRENT_PROFILE_UNVERIFIED")
	}
	if p := values[0x2b]; len(p) != 1 || p[0] != 4 {
		return out, errors.New("OWNED_DATA_CURRENT_FAMILY_UNVERIFIED")
	}
	out.address, e = ownedIPv4(values[0x1e])
	if e != nil {
		return ownedCurrentSettings{}, e
	}
	out.gateway, e = ownedIPv4(values[0x20])
	if e != nil {
		return ownedCurrentSettings{}, e
	}
	mask := values[0x21]
	if len(mask) != 4 {
		return ownedCurrentSettings{}, errors.New("OWNED_DATA_NETMASK_MISSING")
	}
	v := binary.LittleEndian.Uint32(mask)
	for out.prefixBits < 32 && v&(1<<31) != 0 {
		out.prefixBits++
		v <<= 1
	}
	if v != 0 || out.prefixBits == 0 {
		return ownedCurrentSettings{}, errors.New("OWNED_DATA_NETMASK_INVALID")
	}
	if raw := values[0x29]; len(raw) == 4 {
		out.mtu = binary.LittleEndian.Uint32(raw)
	} else {
		return ownedCurrentSettings{}, errors.New("OWNED_DATA_MTU_MISSING")
	}
	if out.mtu < 576 || out.mtu > 1500 {
		return ownedCurrentSettings{}, errors.New("OWNED_DATA_MTU_UNSUPPORTED")
	}
	for _, kind := range []byte{0x15, 0x16} {
		if v, ok := values[kind]; ok {
			addr, e := ownedIPv4(v)
			if e != nil {
				return ownedCurrentSettings{}, e
			}
			if len(out.dns) == 0 || out.dns[0] != addr {
				out.dns = append(out.dns, addr)
			}
		}
	}
	return out, nil
}
