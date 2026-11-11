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
