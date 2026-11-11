package modem

import (
	"encoding/binary"
	"errors"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const nativeAPNStub = "SPF.EventBase.NPMComHandler.Stub"
const nativeAPNPath = dbus.ObjectPath("/SPF/EventBase/NPMComHandler")
const nativeAPNEventPath = dbus.ObjectPath("/SPF/EventBase/Evt012")
const nativeAPNInterface = "SPF.EventBase.v1_0"
const nativeAPNSignal = nativeAPNInterface + ".SPFEvent"

// NativeAPNStatus contains only the two native state codes. It neither reports
// APN credentials nor infers current PDP connectivity from undocumented enums.
type NativeAPNStatus struct {
	ObservedAt            time.Time `json:"observedAt"`
	Source                string    `json:"source"`
	Accepted              bool      `json:"accepted"`
	ResultReceived        bool      `json:"resultReceived"`
	ResultCode            *uint32   `json:"resultCode"`
	Profile1State         *uint32   `json:"profile1State"`
	Profile2State         *uint32   `json:"profile2State"`
	CompletionDelayMillis *uint32   `json:"completionDelayMillis,omitempty"`
}

func nativeAPNEventClock(body []any) uint32 {
	if len(body) != 3 {
		return 0
	}
	raw, ok := body[2].([]byte)
	if !ok || len(raw) < 44 {
		return 0
	}
	return binary.LittleEndian.Uint32(raw[28:])
}
func nativeAPNConnectClockDelay(ack, event uint32) (uint32, bool) {
	// The pinned transmitter stores CLOCK_MONOTONIC_RAW milliseconds in a
	// 32-bit value, so subtraction intentionally handles the 49-day wrap.
	delay := event - ack
	return delay, ack != 0 && event != 0 && delay >= 150 && delay <= 8000
}

// There is deliberately no public arbitrary operation, endpoint, or payload API.
func nativeAPNGetRequest() []byte {
	raw := make([]byte, 24)
	binary.LittleEndian.PutUint32(raw, 1)
	binary.LittleEndian.PutUint32(raw[8:], 0x0a)
	binary.LittleEndian.PutUint32(raw[12:], 0x13)
	return raw // caller PID is supplied in the D-Bus envelope; Client* is NULL.
}

func nativeAPNMethodAccepted(body []any) bool {
	if len(body) != 2 {
		return false
	}
	raw, ok := body[0].([]byte)
	aux, okAux := body[1].([]uint32)
	return ok && okAux && len(raw) == 1 && raw[0] == 1 && len(aux) == 0
}

func parseNativeAPNSignal(body []any, self uint32) (kind string, values [2]uint32, err error) {
	return parseNativeAPNReply(body, self, 1, false)
}

func parseNativeAPNReply(body []any, self, sequence uint32, connect bool) (kind string, values [2]uint32, err error) {
	if len(body) != 3 {
		return "", values, errors.New("native status signal shape invalid")
	}
	destination, a := body[0].(uint32)
	flags, b := body[1].(uint32)
	raw, c := body[2].([]byte)
	if !a || !b || !c || flags != 0 || len(raw) < 12 || len(raw) > 4096 {
		return "", values, errors.New("native status signal envelope invalid")
	}
	if destination != 0 && destination != self {
		return "", values, nil
	}
	if binary.LittleEndian.Uint32(raw[8:]) != 0x0c {
		return "", values, nil
	}
	event := binary.LittleEndian.Uint64(raw)
	packed := raw[12:]
	switch event {
	case 0x300:
		if destination != self {
			return "", values, nil
		}
		if len(packed) != 56 || binary.LittleEndian.Uint64(packed[24:]) != event {
			return "", values, errors.New("native result size invalid")
		}
		subtype := uint32(0x13)
		if connect {
			subtype = 0x14
		}
		if binary.LittleEndian.Uint32(packed[32:]) != sequence || binary.LittleEndian.Uint32(packed[36:]) != subtype {
			return "", values, nil
		}
		if binary.LittleEndian.Uint64(packed[48:]) != 0 {
			return "", values, errors.New("native result client pointer unexpected")
		}
		values[0] = binary.LittleEndian.Uint32(packed[40:])
		if values[0] > 4 {
			return "", values, errors.New("native result enum unsupported")
		}
		return "result", values, nil
	case 0x3f2:
		if connect {
			return "", values, nil
		}
		if len(packed) != 40 || binary.LittleEndian.Uint64(packed[24:]) != event {
			return "", values, errors.New("native APN status size invalid")
		}
		values[0] = binary.LittleEndian.Uint32(packed[32:])
		values[1] = binary.LittleEndian.Uint32(packed[36:])
		if values[0] > 3 || values[1] > 3 {
			return "", values, errors.New("native APN state enum unsupported")
		}
		return "status", values, nil
	case 0x3f3:
		if !connect {
			return "", values, nil
		}
		if len(packed) != 40 || binary.LittleEndian.Uint64(packed[24:]) != event {
			return "", values, errors.New("native APN connect result size invalid")
		}
		values[0] = binary.LittleEndian.Uint32(packed[32:])
		values[1] = binary.LittleEndian.Uint32(packed[36:])
		if values[0] > 2 || values[1] > 2 {
			return "", values, errors.New("native APN connect result unsupported")
		}
		return "status", values, nil
	}
	return "", values, nil
}

// Accept only the single Unix transport inherited by the pinned stock owner.
// No TCP, autolaunch, address alternatives, or caller supplied address is used.
func nativeAPNUnixAddress(raw string) (string, error) {
	if len(raw) > 512 || !strings.HasPrefix(raw, "unix:") || strings.ContainsAny(raw, ";\x00\r\n") {
		return "", errors.New("native owner bus transport unsupported")
	}
	var address string
	seen := map[string]bool{}
	for _, part := range strings.Split(strings.TrimPrefix(raw, "unix:"), ",") {
		pair := strings.SplitN(part, "=", 2)
		if len(pair) != 2 || seen[pair[0]] || pair[1] == "" {
			return "", errors.New("native owner bus address invalid")
		}
		seen[pair[0]] = true
		value := pair[1]
		for _, r := range value {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/_.-", r)) {
				return "", errors.New("native owner bus address encoding unsupported")
			}
		}
		switch pair[0] {
		case "abstract":
			if address != "" || len(value) > 106 {
				return "", errors.New("native owner bus endpoint invalid")
			}
			address = "@" + value
		case "path":
			if address != "" || !strings.HasPrefix(value, "/") || len(value) > 107 || strings.Contains(value, "..") {
				return "", errors.New("native owner bus endpoint invalid")
			}
			address = value
		case "guid":
			if len(value) != 32 || strings.Trim(value, "0123456789abcdefABCDEF") != "" {
				return "", errors.New("native owner bus guid invalid")
			}
		default:
			return "", errors.New("native owner bus option unsupported")
		}
	}
	if address == "" {
		return "", errors.New("native owner bus endpoint missing")
	}
	return address, nil
}
