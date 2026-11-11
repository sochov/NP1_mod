package adapters

import "errors"

// Proven by Voice's AlexaAMAFunc.proto and the 0x109950 reply branch. This is
// the stock wake-word setting, not Amazon account authentication or playback.
const AlexaFunctionID uint16 = 0x0b00
const AlexaChangeCommand uint16 = 3
const AlexaGetCommand uint16 = 4
const AlexaChangeResponse uint16 = 0x8003
const AlexaGetResponse uint16 = 0x8004
const AlexaNotifyCommand uint16 = 0x8005

type AlexaWakeWordState struct {
	Value          int   `json:"value"`
	Enabled        bool  `json:"enabled"`
	Fixed          bool  `json:"fixed"`
	ChangeAccepted *bool `json:"changeAccepted,omitempty"`
}

// AlexaWakeWordRequest excludes ON_FIXED, which is a read-only result produced
// by stock linkage state. The caller must still use stock's guarded handler.
func AlexaWakeWordRequest(enabled bool) []byte {
	if enabled {
		return nil
	} // proto3 default enum ON=0
	return []byte{8, 1}
}

// ParseAlexaWakeWordResponse validates the bounded stock protobuf payload.
// This function does not claim an actual state unless called with a matching,
// independently correlated response from the owner IPC helper.
func ParseAlexaWakeWordResponse(command uint16, payload []byte) (AlexaWakeWordState, error) {
	if len(payload) > 32 || (command != AlexaChangeResponse && command != AlexaGetResponse && command != AlexaNotifyCommand) {
		return AlexaWakeWordState{}, errors.New("ALEXA_RESPONSE_INVALID")
	}
	value := uint64(0)
	accepted := false
	seen := map[uint64]bool{}
	for pos := 0; pos < len(payload); {
		key, n, ok := alexaVarint(payload[pos:])
		if !ok {
			return AlexaWakeWordState{}, errors.New("ALEXA_RESPONSE_INVALID")
		}
		pos += n
		field := key >> 3
		if key&7 != 0 || field == 0 || seen[field] || (command != AlexaChangeResponse && field != 1) || (command == AlexaChangeResponse && field > 2) {
			return AlexaWakeWordState{}, errors.New("ALEXA_RESPONSE_INVALID")
		}
		seen[field] = true
		v, n, ok := alexaVarint(payload[pos:])
		if !ok {
			return AlexaWakeWordState{}, errors.New("ALEXA_RESPONSE_INVALID")
		}
		pos += n
		if command == AlexaChangeResponse && field == 1 {
			if v > 1 {
				return AlexaWakeWordState{}, errors.New("ALEXA_RESPONSE_INVALID")
			}
			accepted = v == 1
		} else {
			value = v
		}
	}
	if value > 2 {
		return AlexaWakeWordState{}, errors.New("ALEXA_ENUM_UNKNOWN")
	}
	out := AlexaWakeWordState{Value: int(value), Enabled: value != 1, Fixed: value == 2}
	if command == AlexaChangeResponse {
		out.ChangeAccepted = &accepted
	}
	return out, nil
}

func alexaVarint(data []byte) (uint64, int, bool) {
	var value uint64
	for i, b := range data {
		if i == 10 || (i == 9 && b > 1) {
			return 0, 0, false
		}
		value |= uint64(b&127) << uint(7*i)
		if b < 128 {
			return value, i + 1, true
		}
	}
	return 0, 0, false
}
