package modem

import (
	"bytes"
	"encoding/binary"
	"errors"
	"time"
)

const ownedRMNetInfoSize = 164

// ownedQMAPInfo is private, live driver metadata, not permission to start a
// session. No caller may choose a different endpoint, profile or mux ID.
type ownedQMAPInfo struct {
	rxURBSize, endpoint, usbInterface, mode, version, padding uint32
	interfaces                                                [2]string
	mux                                                       [2]byte
}

func (ownedQMAPInfo) String() string               { return "[private QMAP binding]" }
func (v ownedQMAPInfo) GoString() string           { return v.String() }
func (ownedQMAPInfo) MarshalJSON() ([]byte, error) { return []byte(`{"private":true}`), nil }

func parseOwnedRMNetInfo(raw []byte) (ownedQMAPInfo, error) {
	var out ownedQMAPInfo
	bad := errors.New("OWNED_DATA_QMAP_METADATA_UNVERIFIED")
	if len(raw) != ownedRMNetInfoSize || binary.LittleEndian.Uint32(raw[0:4]) != ownedRMNetInfoSize {
		return out, bad
	}
	out.rxURBSize = binary.LittleEndian.Uint32(raw[4:8])
	out.endpoint = binary.LittleEndian.Uint32(raw[8:12])
	out.usbInterface = binary.LittleEndian.Uint32(raw[12:16])
	out.mode = binary.LittleEndian.Uint32(raw[16:20])
	out.version = binary.LittleEndian.Uint32(raw[20:24])
	out.padding = binary.LittleEndian.Uint32(raw[24:28])
	// These limits are the inspected NP1 driver ABI, not a generic modem API.
	if out.rxURBSize != 4096 || out.endpoint != 2 || out.usbInterface != 4 || out.mode != 2 || (out.version != 5 && out.version != 9) || out.padding != 0 {
		return ownedQMAPInfo{}, bad
	}
	for i, want := range []string{"usb0.1", "usb0.2"} {
		name := raw[28+16*i : 28+16*(i+1)]
		if !bytes.Equal(name, append([]byte(want), make([]byte, 16-len(want))...)) || raw[156+i] != byte(0x81+i) {
			return ownedQMAPInfo{}, bad
		}
		out.interfaces[i], out.mux[i] = want, raw[156+i]
	}
	// The pinned driver zero-initializes this structure. More active children
	// would mean the live binding differs from the two-context fixture.
	if !bytes.Equal(raw[60:156], make([]byte, 96)) {
		return ownedQMAPInfo{}, bad
	}
	return out, nil
}

// OwnedCellularPreflight exposes only non-identifying firmware capabilities.
// Available never means that a packet data call or Linux egress was created.
type OwnedCellularPreflight struct {
	ObservedAt     time.Time `json:"observedAt"`
	Source         string    `json:"source"`
	Status         string    `json:"status"`
	CMAbsent       bool      `json:"cmAbsent"`
	DriverVerified bool      `json:"driverVerified"`
	QMAPVerified   bool      `json:"qmapVerified"`
	QMAPVersion    uint32    `json:"qmapVersion,omitempty"`
	BindingCount   int       `json:"bindingCount"`
	SessionStarted bool      `json:"sessionStarted"`
}

func newOwnedPreflight() OwnedCellularPreflight {
	return OwnedCellularPreflight{ObservedAt: time.Now().UTC(), Source: "gobinet-qmap-metadata-readonly", Status: "unknown"}
}
