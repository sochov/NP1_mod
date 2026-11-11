package adapters

import (
	"encoding/binary"
	"github.com/np1-control-center/ncc/internal/model"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeBackupPersistenceVerification(t *testing.T) {
	data := make([]byte, 704)
	binary.LittleEndian.PutUint32(data[4:], 16)
	binary.LittleEndian.PutUint32(data[12:], 86)
	for i := 0; i < 86; i++ {
		key := uint32(450 + i)
		binary.LittleEndian.PutUint32(data[16+i*8:], key)
	}
	offset := 16 + (507-450)*8 + 4
	for _, value := range []uint32{0, 1, 2} {
		binary.LittleEndian.PutUint32(data[offset:], value)
		actual, e := parseNativeBackup(data)
		if e != nil || actual != int64(value) {
			t.Fatalf("persisted enum %d: %d %v", value, actual, e)
		}
	}
	for _, size := range []int{0, 703} {
		if _, e := parseNativeBackup(data[:size]); e == nil {
			t.Fatal("truncated persistence accepted")
		}
	}
	binary.LittleEndian.PutUint32(data[offset:], 3)
	if _, e := parseNativeBackup(data); e == nil {
		t.Fatal("invalid persisted enum accepted")
	}
	binary.LittleEndian.PutUint32(data[offset:], 1)
	binary.LittleEndian.PutUint32(data[24:], 450)
	if _, e := parseNativeBackup(data); e == nil {
		t.Fatal("duplicate key accepted")
	}
}

func TestNativeControlsPinIndividualStorageKeysAndHelpers(t *testing.T) {
	seen := map[uint32]bool{}
	for _, test := range []struct {
		id    string
		index int
		key   uint32
		max   int64
	}{
		{"privacy-mode", 8, 529, 1}, {"driving-support", 39, 495, 1}, {"service-cabin-camera", 49, 505, 1},
		{"tailgating-detection", 50, 506, 1}, {"tailgating-sensitivity", 51, 507, 2},
		{"traffic-check", 61, 519, 1}, {"traffic-congestion", 63, 521, 1}, {"traffic-regulation", 64, 522, 1},
		{"ferry", 65, 523, 1}, {"toll-road", 66, 524, 1}, {"smart-ic", 67, 525, 1},
		{"route-search", 68, 526, 5}, {"daily-voice", 77, 536, 5}, {"support-voice", 78, 537, 5},
	} {
		spec, ok := nativeSpec(test.id)
		if !ok || spec.index != test.index || spec.persistent != test.key || spec.maximum != test.max || seen[spec.persistent] {
			t.Fatal("invalid native mapping", test.id)
		}
		seen[spec.persistent] = true
		if test.index == 50 || test.index == 51 {
			if spec.helper != nativeHelper || spec.hash != nativeHelperHash {
				t.Fatal("proven helper changed")
			}
		} else if test.index == 68 || test.index == 77 || test.index == 78 {
			if spec.helper != nativeChoiceHelper || spec.hash != nativeChoiceHash {
				t.Fatal("choice helper is not pinned")
			}
		} else if spec.helper != nativeExtendedHelper || spec.hash != nativeExtendedHash {
			t.Fatal("extended helper is not pinned")
		}
	}
	for _, id := range []string{"guidance-frequency", "beginner-driver", "daily-voice;id", "format"} {
		if _, ok := nativeSpec(id); ok {
			t.Fatal("unproven setting writable", id)
		}
	}
}

func TestNativeChoiceStrictValuesAndCAS(t *testing.T) {
	for _, test := range []struct {
		id    string
		index int
	}{{"route-search", 68}, {"daily-voice", 77}, {"support-voice", 78}} {
		t.Run(test.id, func(t *testing.T) {
			values := make([]int32, 86)
			values[test.index] = 4
			observe := func() model.Observation { return nativeObservation(values, nil) }
			calls := 0
			apply := func(id string, before, next int64) error {
				calls++
				if id != test.id || before != 4 || next != 5 {
					t.Fatal("incorrect choice command")
				}
				values[test.index] = int32(next)
				return nil
			}
			for _, body := range []string{
				`{"expectedValue":4,"value":6}`, `{"expectedValue":6,"value":0}`,
				`{"expectedValue":4,"value":12}`, `{"expectedValue":4,"value":-1}`,
				`{"expectedValue":4,"value":"5"}`, `{"expectedValue":4,"value":5.0}`,
				`{"expectedValue":true,"value":5}`, `{"expectedValue":4,"value":5,"extra":1}`,
				`{"expectedValue":4,"value":5} {}`, `{"value":5}`,
			} {
				w := httptest.NewRecorder()
				r := httptest.NewRequest("PUT", "/native-settings/"+test.id, strings.NewReader(body))
				handleNativeSettingWithApply(w, r, observe, apply)
				if w.Code != 400 || calls != 0 {
					t.Fatalf("unproven choice applied: %s status%d", body, w.Code)
				}
			}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("PUT", "/native-settings/"+test.id, strings.NewReader(`{"expectedValue":4,"value":5}`))
			handleNativeSettingWithApply(w, r, observe, apply)
			if w.Code != 200 || calls != 1 || values[test.index] != 5 {
				t.Fatal("valid maximum choice rejected")
			}
			w = httptest.NewRecorder()
			r = httptest.NewRequest("PUT", "/native-settings/"+test.id, strings.NewReader(`{"expectedValue":4,"value":5}`))
			handleNativeSettingWithApply(w, r, observe, apply)
			if w.Code != 409 || calls != 1 {
				t.Fatal("stale expected value applied")
			}
			if applyNativeVerified(test.id, 6, 0) == nil || applyNativeVerified(test.id, 0, 6) == nil {
				t.Fatal("local worker must reject values outside 0..5 before native execution")
			}
		})
	}
}

func TestNativeChoiceUnknownCurrentValueStaysUnknown(t *testing.T) {
	values := make([]int32, 86)
	for _, index := range []int{68, 77, 78} {
		values[index] = 6
	}
	data := nativeObservation(values, nil).Data.(NativeData)
	for _, item := range data.Items {
		if item.ID == "route-search" || item.ID == "daily-voice" || item.ID == "support-voice" {
			if item.Status != "unknown" || item.Value != nil || item.Writable || len(item.Options) != 0 {
				t.Fatal("unknown current choice exposed as usable", item.ID)
			}
		}
	}
}

func TestNativeVerifiedWriteRequiresExpectedEnum(t *testing.T) {
	values := make([]int32, 86)
	values[51] = 1
	observe := func() model.Observation { return nativeObservation(values, nil) }
	calls := 0
	apply := func(id string, before, next int64) error {
		calls++
		if id != "tailgating-sensitivity" || before != 1 || next != 2 {
			t.Fatal("unexpected native command")
		}
		values[51] = int32(next)
		return nil
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/native-settings/tailgating-sensitivity", strings.NewReader(`{"expectedValue":1,"value":2}`))
	handleNativeSettingWithApply(w, r, observe, apply)
	if w.Code != 200 || calls != 1 || values[51] != 2 {
		t.Fatalf("apply not verified: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest("PUT", "/native-settings/tailgating-sensitivity", strings.NewReader(`{"expectedValue":1,"value":0}`))
	handleNativeSettingWithApply(w, r, observe, apply)
	if w.Code != 409 || calls != 1 {
		t.Fatal("stale native value applied")
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest("PUT", "/native-settings/tailgating-sensitivity", strings.NewReader(`{"expectedValue":1,"value":0}`))
	missing := func() model.Observation { return model.Value("test", NativeData{Items: []NativeItem{}}) }
	handleNativeSettingWithApply(w, r, missing, apply)
	if w.Code != 503 || calls != 1 {
		t.Fatal("missing native value applied")
	}
}
