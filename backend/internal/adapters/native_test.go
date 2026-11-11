package adapters

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"github.com/np1-control-center/ncc/internal/model"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func nativeFixture() []byte {
	b := make([]byte, 7296)
	binary.LittleEndian.PutUint64(b, 7168)
	p := b[128:]
	for offset, value := range map[int]uint32{0: 1, 4: 1, 20: 44, 24: 86} {
		binary.LittleEndian.PutUint32(p[offset:], value)
	}
	binary.LittleEndian.PutUint32(p[76:], 1)
	return b
}
func TestNativeMemoryValidation(t *testing.T) {
	valid := nativeFixture()
	v, e := parseNativeValues(valid)
	if e != nil || len(v) != 86 || v[8] != 1 {
		t.Fatalf("valid: %v", e)
	}
	if _, e := parseNativeValues(valid[:100]); e == nil {
		t.Fatal("truncated accepted")
	}
	for _, offset := range []int{0, 128, 132, 148, 152} {
		b := append([]byte{}, valid...)
		b[offset]++
		if _, e := parseNativeValues(b); e == nil {
			t.Fatalf("corrupt header %d accepted", offset)
		}
	}
}
func TestNativeReaderPinsFirmware(t *testing.T) {
	dir := t.TempDir()
	a, b := []byte("test-setting"), []byte("test-library")
	files := nativeFiles{filepath.Join(dir, "memory"), filepath.Join(dir, "setting"), filepath.Join(dir, "library"), fmt.Sprintf("%x", sha256.Sum256(a)), fmt.Sprintf("%x", sha256.Sum256(b))}
	for p, v := range map[string][]byte{files.memory: nativeFixture(), files.setting: a, files.library: b} {
		if e := os.WriteFile(p, v, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := readNativeValues(files); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(files.library, []byte("other-firmware"), 0600)
	if _, e := readNativeValues(files); e == nil {
		t.Fatal("unmatched firmware accepted")
	}
}
func TestNativeMissingIsNeverOff(t *testing.T) {
	o := nativeObservation(nil, fmt.Errorf("missing"))
	if o.Status != "unknown" || o.Verified {
		t.Fatal("missing marked verified")
	}
	for _, i := range o.Data.(NativeData).Items {
		if i.Value != nil || i.ValueLabel != "" || i.Writable {
			t.Fatal("missing fabricated")
		}
	}
	v := make([]int32, 86)
	v[8] = 2
	if nativeObservation(v, nil).Data.(NativeData).Items[0].Value != nil {
		t.Fatal("invalid enum coerced")
	}
}
func TestNativePutRejectsUnverifiedWrites(t *testing.T) {
	values := make([]int32, 86)
	values[8] = 1
	observe := func() model.Observation { return nativeObservation(values, nil) }
	tests := []struct {
		name, id, body string
		code           int
	}{
		{"readonly", "privacy-mode", `{"expectedValue":1,"value":0}`, 403}, {"conflict", "privacy-mode", `{"expectedValue":0,"value":1}`, 409},
		{"bool", "privacy-mode", `{"expectedValue":1,"value":true}`, 400}, {"string", "privacy-mode", `{"expectedValue":1,"value":"0"}`, 400}, {"float", "privacy-mode", `{"expectedValue":1,"value":0.0}`, 400}, {"exponent", "privacy-mode", `{"expectedValue":1,"value":1e0}`, 400}, {"range", "privacy-mode", `{"expectedValue":1,"value":2}`, 400}, {"negative", "privacy-mode", `{"expectedValue":1,"value":-1}`, 400}, {"missing", "privacy-mode", `{"value":0}`, 400}, {"null", "privacy-mode", `{"expectedValue":null,"value":0}`, 400}, {"extra", "privacy-mode", `{"expectedValue":1,"value":0,"path":"/etc/config"}`, 400}, {"second-object", "privacy-mode", `{"expectedValue":1,"value":0}{}`, 400}, {"unknown", "format-sd", `{"expectedValue":1,"value":0}`, 404},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("PUT", "/native-settings/"+tc.id, strings.NewReader(tc.body))
			w := httptest.NewRecorder()
			handleNativeSetting(w, r, observe)
			if w.Code != tc.code {
				t.Fatalf("got %d %s", w.Code, w.Body.String())
			}
			if values[8] != 1 {
				t.Fatal("mutated")
			}
		})
	}
	r := httptest.NewRequest("PUT", "/native-settings/privacy-mode", strings.NewReader(`{"expectedValue":1,"value":0}`))
	w := httptest.NewRecorder()
	handleNativeSetting(w, r, func() model.Observation { return nativeObservation(nil, fmt.Errorf("missing")) })
	if w.Code != 503 {
		t.Fatal("missing current accepted")
	}
}
func TestNativeTailgatedEnumsAreNotWireValues(t *testing.T) {
	for _, test := range []struct {
		detector, sensitivity int32
		label, sense          string
	}{{0, 0, "ON", "低"}, {1, 1, "OFF", "標準"}, {0, 2, "ON", "高"}} {
		values := make([]int32, 86)
		values[50] = test.detector
		values[51] = test.sensitivity
		for _, item := range nativeObservation(values, nil).Data.(NativeData).Items {
			if item.ID == "tailgating-detection" && item.ValueLabel != test.label {
				t.Fatal("native detector enum inverted")
			}
			if item.ID == "tailgating-sensitivity" && item.ValueLabel != test.sense {
				t.Fatal("native sensitivity enum mismatch")
			}
		}
	}
}
