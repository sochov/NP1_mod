package adapters

import (
	"encoding/binary"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/np1-control-center/ncc/internal/model"
)

func TestNativeNotificationsStrictSchemaAndCAS(t *testing.T) {
	for _, target := range []struct {
		id    string
		index int
	}{{"front-car-start", 79}, {"signal-change", 80}} {
		t.Run(target.id, func(t *testing.T) {
			values := make([]int32, 86)
			values[target.index] = 2
			observe := func() model.Observation { return nativeObservation(values, nil) }
			calls := 0
			apply := func(id string, before, next int64) error {
				calls++
				if id != target.id || before != 2 || next != 1 {
					t.Fatal("notification command changed")
				}
				values[target.index] = int32(next)
				return nil
			}
			for _, body := range []string{
				`{"expectedValue":2,"value":3}`, `{"expectedValue":3,"value":0}`,
				`{"expectedValue":2,"value":-1}`, `{"expectedValue":2,"value":"1"}`,
				`{"expectedValue":2,"value":1.0}`, `{"expectedValue":2,"value":1e0}`,
				`{"expectedValue":2,"value":true}`, `{"expectedValue":2,"value":null}`,
				`{"expectedValue":2,"value":1,"extra":0}`, `{"expectedValue":2,"value":1} {}`,
				`{"value":1}`, `{"expectedValue":2}`,
			} {
				w := httptest.NewRecorder()
				r := httptest.NewRequest("PUT", "/native-settings/"+target.id, strings.NewReader(body))
				handleNativeSettingWithApply(w, r, observe, apply)
				if w.Code != 400 || calls != 0 {
					t.Fatalf("invalid notification accepted: %d %s", w.Code, body)
				}
			}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("PUT", "/native-settings/"+target.id, strings.NewReader(`{"expectedValue":2,"value":1}`))
			handleNativeSettingWithApply(w, r, observe, apply)
			if w.Code != 200 || calls != 1 || values[target.index] != 1 {
				t.Fatal("notification apply not confirmed")
			}
			w = httptest.NewRecorder()
			r = httptest.NewRequest("PUT", "/native-settings/"+target.id, strings.NewReader(`{"expectedValue":2,"value":1}`))
			handleNativeSettingWithApply(w, r, observe, apply)
			if w.Code != 409 || calls != 1 {
				t.Fatal("stale notification CAS was accepted")
			}
			if applyNativeVerified(target.id, 3, 0) == nil || applyNativeVerified(target.id, 0, 3) == nil {
				t.Fatal("worker enum guard bypassed")
			}
		})
	}
}

func TestNativeNotificationOptionsAndPersistentKeys(t *testing.T) {
	for _, index := range []int{79, 80} {
		options := NativeStorageOptions(index)
		if len(options) != 3 || options[0].Label != "通知しない" || options[1].Label != "効果音と音声" || options[2].Label != "効果音のみ" {
			t.Fatal("notification labels changed")
		}
		options[0].Label = "mutated"
		if NativeStorageOptions(index)[0].Label != "通知しない" {
			t.Fatal("notification options share mutable state")
		}
	}
	data := make([]byte, 704)
	binary.LittleEndian.PutUint32(data[4:], 16)
	binary.LittleEndian.PutUint32(data[12:], 86)
	for i := 0; i < 86; i++ {
		binary.LittleEndian.PutUint32(data[16+8*i:], uint32(460+i))
	}
	for _, key := range []uint32{538, 539} {
		offset := 20 + 8*int(key-460)
		for value := uint32(0); value <= 3; value++ {
			binary.LittleEndian.PutUint32(data[offset:], value)
			actual, err := parseNativeBackupValue(data, key, 2)
			if value <= 2 && (err != nil || actual != int64(value)) {
				t.Fatal("valid notification backup rejected")
			}
			if value == 3 && err == nil {
				t.Fatal("unknown notification backup accepted")
			}
		}
		binary.LittleEndian.PutUint32(data[offset:], 0)
	}
}

func TestNativeNotificationUnknownCurrentRemainsUnavailable(t *testing.T) {
	values := make([]int32, 86)
	values[79], values[80] = 3, -1
	data := nativeObservation(values, nil).Data.(NativeData)
	for _, item := range data.Items {
		if item.ID == "front-car-start" || item.ID == "signal-change" {
			if item.Status != "unknown" || item.Value != nil || item.Writable || len(item.Options) != 0 {
				t.Fatal("unknown notification became usable")
			}
		}
	}
}
