package adapters

import (
	"bytes"
	"testing"
)

func TestAlexaProvenRequestEnum(t *testing.T) {
	if len(AlexaWakeWordRequest(true)) != 0 || !bytes.Equal(AlexaWakeWordRequest(false), []byte{8, 1}) {
		t.Fatal("wrong stock ON/OFF protobuf mapping")
	}
}
func TestAlexaGuardedStateAndChangeResult(t *testing.T) {
	for _, test := range []struct {
		command  uint16
		payload  []byte
		value    int
		accepted *bool
	}{
		{AlexaGetResponse, nil, 0, nil}, {AlexaGetResponse, []byte{8, 1}, 1, nil}, {AlexaNotifyCommand, []byte{8, 2}, 2, nil},
		{AlexaChangeResponse, []byte{8, 1, 16, 1}, 1, alexaBool(true)}, {AlexaChangeResponse, []byte{16, 2}, 2, alexaBool(false)},
	} {
		state, e := ParseAlexaWakeWordResponse(test.command, test.payload)
		if e != nil || state.Value != test.value || state.Enabled != (test.value != 1) || state.Fixed != (test.value == 2) {
			t.Fatalf("%#v: %#v %v", test, state, e)
		}
		if test.accepted == nil {
			if state.ChangeAccepted != nil {
				t.Fatal("getter invented apply result")
			}
		} else if state.ChangeAccepted == nil || *state.ChangeAccepted != *test.accepted {
			t.Fatal("change result lost")
		}
	}
}
func TestAlexaMalformedResponsesAreNotON(t *testing.T) {
	for _, data := range [][]byte{{8}, {8, 3}, {8, 1, 8, 0}, {18, 1, 0}, {0, 0}, {24, 0}, {8, 128}, {8, 255, 255, 255, 255, 255, 255, 255, 255, 255, 2}} {
		if _, e := ParseAlexaWakeWordResponse(AlexaGetResponse, data); e == nil {
			t.Fatalf("accepted %x", data)
		}
	}
	if _, e := ParseAlexaWakeWordResponse(AlexaChangeResponse, []byte{8, 2}); e == nil {
		t.Fatal("accepted invalid result bool")
	}
	if _, e := ParseAlexaWakeWordResponse(4, nil); e == nil {
		t.Fatal("request mistaken for response")
	}
}
func alexaBool(value bool) *bool { return &value }
