package modem

import (
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
)

func statusFrame(transaction, message uint16, fields map[byte][]byte) []byte {
	payload := []byte{2, 4, 0, 0, 0, 0, 0}
	for kind, value := range fields {
		payload = append(payload, kind, byte(len(value)), byte(len(value)>>8))
		payload = append(payload, value...)
	}
	frame := response(payload)
	binary.LittleEndian.PutUint16(frame[1:3], transaction)
	binary.LittleEndian.PutUint16(frame[3:5], message)
	return frame
}
func servingFixture(flag *byte) []byte {
	fields := map[byte][]byte{1: {1, 1, 1, 2, 1, 8}, 0x12: {1, 0, 1, 0, 4, 'T', 'E', 'S', 'T'}}
	if flag != nil {
		fields[0x27] = []byte{1, 0, 1, 0, *flag}
	}
	return statusFrame(2, 0x24, fields)
}
func TestNASServingRegistrationPSAndPLMNDigits(t *testing.T) {
	two, three := byte(0), byte(1)
	for _, test := range []struct {
		flag *byte
		want string
	}{{&two, "00101"}, {&three, "001001"}, {nil, ""}} {
		value, e := parseServingResponse(servingFixture(test.flag), 2)
		if e != nil || value.Registered == nil || !*value.Registered || value.PSAttached == nil || !*value.PSAttached || value.Carrier != "TEST" || value.PLMN != test.want || value.RAT != "LTE" {
			t.Fatal(value, e)
		}
	}
}
func TestNASServingUnknownAndUnregisteredNeverImplyConnected(t *testing.T) {
	for _, test := range []struct {
		registration, ps     byte
		registered, attached *bool
	}{{4, 0, nil, nil}, {2, 2, boolValue(false), boolValue(false)}, {99, 99, nil, nil}} {
		value, e := parseServingResponse(statusFrame(2, 0x24, map[byte][]byte{1: {test.registration, 1, test.ps, 2, 0}}), 2)
		if e != nil || !reflect.DeepEqual(value.Registered, test.registered) || !reflect.DeepEqual(value.PSAttached, test.attached) {
			t.Fatal(value, e)
		}
	}
}
func TestNASServingLengthsCodeMismatchAndUnsafeName(t *testing.T) {
	for _, fields := range []map[byte][]byte{{1: {1, 1, 1, 2, 2, 8}}, {0x12: {1, 0, 1, 0, 3, 'T'}}, {0x27: {1, 0, 1, 0, 2}}, {0x12: {1, 0, 1, 0, 0}, 0x27: {2, 0, 1, 0, 0}}, {0x27: {1, 0, 100, 0, 0}}} {
		if _, e := parseServingResponse(statusFrame(2, 0x24, fields), 2); e == nil {
			t.Fatal("malformed serving accepted", fields)
		}
	}
	value, e := parseServingResponse(statusFrame(2, 0x24, map[byte][]byte{0x12: {1, 0, 1, 0, 1, 0x1b}}), 2)
	if e != nil || value.Carrier != "" {
		t.Fatal(value, e)
	}
}
func TestNASServingThreeDigitMNCUnambiguousWithoutFlag(t *testing.T) {
	value, e := parseServingResponse(statusFrame(2, 0x24, map[byte][]byte{0x12: {1, 0, 100, 0, 0}}), 2)
	if e != nil || value.PLMN != "001100" {
		t.Fatal(value, e)
	}
}
func TestNASExplicitLTEBandsAndRATIsolation(t *testing.T) {
	for _, test := range []struct {
		rat  byte
		enum uint16
		want string
	}{{8, 120, "B1"}, {8, 122, "B3"}, {8, 143, "B18"}, {8, 144, "B19"}, {8, 158, "B28"}, {8, 999, ""}, {5, 122, ""}, {99, 120, ""}} {
		band, bands, e := parseBandResponse(statusFrame(3, 0x31, map[byte][]byte{1: {1, test.rat, byte(test.enum), byte(test.enum >> 8), 1, 0}}), 3)
		if e != nil || band != test.want || test.want == "" && len(bands) != 0 {
			t.Fatal(band, bands, e)
		}
	}
}
func TestNASBandExtendedAndAmbiguous(t *testing.T) {
	band, bands, e := parseBandResponse(statusFrame(3, 0x31, map[byte][]byte{1: {1, 8, 120, 0, 1, 0}, 0x11: {1, 8, 143, 0, 1, 0, 0, 0}}), 3)
	if e != nil || band != "B18" || len(bands) != 1 {
		t.Fatal(band, bands, e)
	}
	band, bands, e = parseBandResponse(statusFrame(3, 0x31, map[byte][]byte{1: {2, 8, 120, 0, 1, 0, 8, 122, 0, 2, 0}}), 3)
	if e != nil || band != "" || len(bands) != 2 {
		t.Fatal(band, bands, e)
	}
	for _, value := range [][]byte{{}, {2, 8, 120, 0, 1, 0}, {1, 8, 120, 0, 1, 0, 2}} {
		if _, _, e = parseBandResponse(statusFrame(3, 0x31, map[byte][]byte{1: value}), 3); e == nil {
			t.Fatal("bad band bounds accepted", value)
		}
	}
}
func TestNASQueryFailureIsolationAndFixedIDs(t *testing.T) {
	ids := [][2]uint16{}
	out, e := collectNAS(context.Background(), func(ctx context.Context, transaction, message uint16) ([]byte, error) {
		ids = append(ids, [2]uint16{transaction, message})
		switch message {
		case 0x4f:
			return response([]byte{2, 4, 0, 0, 0, 0, 0, 0x14, 6, 0, 0xcd, 0xf5, 0xae, 0xff, 0x9e, 0}), nil
		case 0x24:
			return nil, context.DeadlineExceeded
		case 0x31:
			return nil, errors.New("synthetic failure")
		}
		panic("unexpected query")
	})
	if e != nil || out.RSRP == nil || *out.RSRP != -82 || out.Registered != nil || out.PSAttached != nil || out.PLMN != "" || out.Band != "" || out.Queries["servingSystem"] != "timeout" || out.Queries["rfBand"] != "failed" {
		t.Fatal(out, e)
	}
	if !reflect.DeepEqual(ids, [][2]uint16{{1, 0x4f}, {2, 0x24}, {3, 0x31}}) {
		t.Fatal(ids)
	}
}
func TestNASStatusCanSucceedWithoutSignalAndCancellationStopsQueries(t *testing.T) {
	flag := byte(0)
	out, e := collectNAS(context.Background(), func(ctx context.Context, transaction, message uint16) ([]byte, error) {
		if message == 0x24 {
			return servingFixture(&flag), nil
		}
		return nil, errors.New("synthetic failure")
	})
	if e != nil || out.Registered == nil || !*out.Registered || out.RSRP != nil || out.Queries["signal"] != "failed" {
		t.Fatal(out, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	out, e = collectNAS(ctx, func(ctx context.Context, transaction, message uint16) ([]byte, error) {
		calls++
		cancel()
		return response([]byte{2, 4, 0, 0, 0, 0, 0}), nil
	})
	if e != nil || calls != 1 || out.Queries["servingSystem"] != "timeout" || out.Registered != nil {
		t.Fatal(out, e, calls)
	}
}
func FuzzNASStatusBounds(f *testing.F) {
	f.Add(servingFixture(nil))
	f.Add(statusFrame(3, 0x31, map[byte][]byte{1: {1, 8, 120, 0, 1, 0}}))
	f.Fuzz(func(t *testing.T, frame []byte) {
		_, _ = parseServingResponse(frame, 2)
		_, _, _ = parseBandResponse(frame, 3)
	})
}

func TestVerifiedSignalPathNeverSendsStatusQueries(t *testing.T) {
	calls := 0
	out, e := collectSignalOnly(context.Background(), func(ctx context.Context, transaction, message uint16) ([]byte, error) {
		calls++
		if transaction != 1 || message != 0x004f {
			t.Fatal("unverified status query reached signal path", transaction, message)
		}
		return response([]byte{2, 4, 0, 0, 0, 0, 0}), nil
	})
	if e != nil || calls != 1 || out.Queries["servingSystem"] != "not-requested" || out.Registered != nil {
		t.Fatal(out, e, calls)
	}
}
