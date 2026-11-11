package modem

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func syntheticProfile(index int, overrides map[byte][]byte) []byte {
	fields := map[byte][]byte{0x11: {0}, 0x14: []byte("private.example"), 0x1b: []byte("secret-user"), 0x1c: []byte("secret-password"), 0x1d: {3}}
	for kind, value := range overrides {
		if value == nil {
			delete(fields, kind)
		} else {
			fields[kind] = value
		}
	}
	return statusFrame(uint16(index+4), 0x2b, fields)
}
func syntheticRecovery(t *testing.T) ProfileRecovery {
	t.Helper()
	r, e := collectProfiles(context.Background(), func(_ context.Context, transaction, message uint16) ([]byte, error) {
		if message != 0x2b || transaction < 5 || transaction > 6 {
			t.Fatal("unexpected query")
		}
		return syntheticProfile(int(transaction)-4, nil), nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestFixedProfileRequestsCannotSetOrSelectOtherProfiles(t *testing.T) {
	for index := 1; index <= 2; index++ {
		request, e := profileRequest(index)
		want := []byte{0, byte(index + 4), 0, 0x2b, 0, 5, 0, 1, 2, 0, 0, byte(index)}
		if e != nil || !bytes.Equal(request, want) {
			t.Fatal(request, e)
		}
		common, e := fixedQMIRequest(uint16(index+4), 0x2b)
		if e != nil || !bytes.Equal(common, want) {
			t.Fatal(common, e)
		}
	}
	for _, index := range []int{-1, 0, 3, 255} {
		if _, e := profileRequest(index); e == nil {
			t.Fatal("unbounded profile ID accepted")
		}
	}
	for _, pair := range [][2]uint16{{5, 0x28}, {6, 0x20}, {4, 0x2b}, {7, 0x2b}, {1, 0x2b}, {0xffff, 0xffff}} {
		if _, e := fixedQMIRequest(pair[0], pair[1]); e == nil {
			t.Fatal("unapproved QMI operation accepted", pair)
		}
	}
	for _, job := range []struct {
		transaction, message uint16
		want                 []byte
	}{{1, 0x4f, []byte{0, 1, 0, 0x4f, 0, 0, 0}}, {2, 0x24, []byte{0, 2, 0, 0x24, 0, 0, 0}}, {3, 0x31, []byte{0, 3, 0, 0x31, 0, 0, 0}}, {4, 0x2d, apnCurrentRequest()}} {
		request, e := fixedQMIRequest(job.transaction, job.message)
		if e != nil || !bytes.Equal(request, job.want) {
			t.Fatal("existing request changed", request, e)
		}
	}
}
func TestProfilePublicFormsNeverExposeAPNOrCredential(t *testing.T) {
	r := syntheticRecovery(t)
	summary := r.Summary()
	if !summary.Complete || !summary.NativeFieldsComplete || summary.Current || len(summary.Profiles) != 2 || summary.Profiles[0].FieldCount != 6 {
		t.Fatal(summary)
	}
	public, _ := json.Marshal(r)
	for _, output := range []string{string(public), fmt.Sprint(r), fmt.Sprintf("%#v", r)} {
		for _, secret := range []string{"private.example", "secret-user", "secret-password"} {
			if strings.Contains(output, secret) {
				t.Fatal("profile field escaped public summary")
			}
		}
	}
}
func TestProfileMissingEmptyUnknownAndUnsupportedFields(t *testing.T) {
	for _, variant := range []struct {
		fields                    map[byte][]byte
		complete, native, unknown bool
	}{{map[byte][]byte{0x1b: {}, 0x1c: {}}, true, true, false}, {map[byte][]byte{0x1b: nil}, false, false, false}, {map[byte][]byte{0x11: {2}}, true, false, false}, {map[byte][]byte{0x11: {255}}, false, false, true}, {map[byte][]byte{0x1d: {255}}, true, false, false}, {map[byte][]byte{0x1c: []byte("unsafe\"value")}, true, false, false}, {map[byte][]byte{0x14: []byte(strings.Repeat("a", 50))}, true, false, false}} {
		p, e := parseSavedAPNProfile(syntheticProfile(1, variant.fields), 1)
		if e != nil {
			t.Fatal(e)
		}
		r := syntheticRecovery(t)
		r.profiles[0] = p
		s := r.Summary()
		if s.Complete != variant.complete || s.NativeFieldsComplete != variant.native || s.Profiles[0].UnknownPDP != variant.unknown {
			t.Fatal(s, variant)
		}
		var wire bytes.Buffer
		if e = r.writeNativeProfiles(&wire); (e == nil) != variant.native {
			t.Fatal("unsupported native conversion accepted", e)
		}
	}
	for _, fields := range []map[byte][]byte{{0x11: {0, 1}}, {0x1d: {}}, {0x1b: []byte{'a', 0, 'b'}}, {0x14: bytes.Repeat([]byte{'x'}, 1025)}} {
		if _, e := parseSavedAPNProfile(syntheticProfile(1, fields), 1); e == nil {
			t.Fatal("malformed profile accepted")
		}
	}
	frame := syntheticProfile(1, nil)
	frame[5]++
	if _, e := parseSavedAPNProfile(frame, 1); e == nil {
		t.Fatal("invalid outer length accepted")
	}
	if _, e := parseSavedAPNProfile(syntheticProfile(1, nil), 2); !errors.Is(e, errUnrelatedQMI) {
		t.Fatal(e)
	}
}
func TestProfileQueriesKeepPartialFailureAndCancellation(t *testing.T) {
	r, e := collectProfiles(context.Background(), func(_ context.Context, transaction, message uint16) ([]byte, error) {
		if transaction == 5 {
			return nil, errors.New("unavailable")
		}
		return syntheticProfile(2, nil), nil
	})
	if e != nil || r.Summary().Complete || r.profiles[0].status != "failed" || r.profiles[1].status != "available" {
		t.Fatal(r, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, e = collectProfiles(ctx, func(context.Context, uint16, uint16) ([]byte, error) {
		t.Fatal("query after cancellation")
		return nil, nil
	})
	if e == nil || r.profiles[0].status != "timeout" || r.profiles[1].status != "timeout" {
		t.Fatal(r, e)
	}
}
func TestProfileErrorNumbersSurviveBackupAndOnlyExplicitAbsenceIsConfirmed(t *testing.T) {
	for _, item := range []struct {
		code, extended uint16
		absence        bool
	}{{81, 1003, true}, {81, 1004, true}, {81, 10, true}, {81, 5, false}, {81, 1002, false}, {10, 0, false}} {
		frame := statusFrame(6, 0x2b, map[byte][]byte{0xe0: {byte(item.extended), byte(item.extended >> 8)}})
		for pos := 7; pos < len(frame); {
			size := int(binary.LittleEndian.Uint16(frame[pos+1 : pos+3]))
			if frame[pos] == 2 {
				binary.LittleEndian.PutUint16(frame[pos+3:pos+5], 1)
				binary.LittleEndian.PutUint16(frame[pos+5:pos+7], item.code)
			}
			pos += 3 + size
		}
		_, protocol := parseQMIResponse(frame, 6, 0x2b)
		r, e := collectProfiles(context.Background(), func(_ context.Context, transaction, message uint16) ([]byte, error) {
			if transaction == 5 {
				return syntheticProfile(1, nil), nil
			}
			return frame, protocol
		})
		if e != nil {
			t.Fatal(e)
		}
		state := r.Summary().Profiles[1]
		if state.ProtocolError == nil || *state.ProtocolError != item.code || state.AbsenceConfirmed != item.absence || state.NativeFieldsComplete {
			t.Fatal(state)
		}
		bundle, e := r.bundle()
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(bundle)
		loaded, e := decodeProfileRecovery(raw, r.observedAt)
		if e != nil || loaded.Summary().Profiles[1].AbsenceConfirmed != item.absence || !bytes.Equal(loaded.profiles[1].raw, frame) {
			t.Fatal(loaded, e)
		}
		var wire bytes.Buffer
		if e = r.writeNativeProfiles(&wire); e == nil || wire.Len() != 0 {
			t.Fatal("absence converted into invented native profile")
		}
	}
}
func TestNativeProfileWireIsFixedSizedPaddedAndRedactedOnError(t *testing.T) {
	r := syntheticRecovery(t)
	var output bytes.Buffer
	if e := r.writeNativeProfiles(&output); e != nil {
		t.Fatal(e)
	}
	wire := output.Bytes()
	if len(wire) != 316 || string(wire[:8]) != "NCCAPN1\n" {
		t.Fatal("native framing mismatch")
	}
	for i := 0; i < 2; i++ {
		record := wire[8+i*154 : 8+(i+1)*154]
		if record[0] != byte(i+1) || record[1] != 3 || record[2] != 0 || record[3] != 0 {
			t.Fatal("native enum mismatch")
		}
		for j, want := range []string{"private.example", "secret-user", "secret-password"} {
			field := record[4+j*50 : 4+(j+1)*50]
			if string(field[:len(want)]) != want || !bytes.Equal(field[len(want):], make([]byte, 50-len(want))) {
				t.Fatal("native field padding mismatch")
			}
		}
	}
	if e := r.writeNativeProfiles(shortProfileWriter{}); !errors.Is(e, io.ErrShortWrite) {
		t.Fatal(e)
	}
}

type shortProfileWriter struct{}

func (shortProfileWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func portableRecoveryOwner(info os.FileInfo, mode os.FileMode) error {
	if mode == 0700 && !info.IsDir() || mode == 0600 && !info.Mode().IsRegular() {
		return errors.New("wrong file type")
	}
	return nil
}
func TestProfilePrivateAtomicBackupLoadAndCollisionPreservesOriginal(t *testing.T) {
	r := syntheticRecovery(t)
	now := r.observedAt
	directory := filepath.Join(t.TempDir(), "recovery")
	nonce := bytes.Repeat([]byte{1}, 8)
	summary, e := saveProfileRecoveryAt(directory, r, now, bytes.NewReader(nonce), portableRecoveryOwner)
	if e != nil || !profileRecoveryName.MatchString(summary.BackupFile) {
		t.Fatal(summary, e)
	}
	before, e := os.ReadFile(filepath.Join(directory, summary.BackupFile))
	if e != nil {
		t.Fatal(e)
	}
	loaded, e := loadProfileRecoveryAt(directory, summary.BackupFile, now, portableRecoveryOwner)
	if e != nil || !loaded.Summary().NativeFieldsComplete || !bytes.Equal(loaded.profiles[0].raw, r.profiles[0].raw) {
		t.Fatal(loaded, e)
	}
	if _, e = saveProfileRecoveryAt(directory, r, now, bytes.NewReader(nonce), portableRecoveryOwner); e == nil {
		t.Fatal("existing recovery overwritten")
	}
	after, _ := os.ReadFile(filepath.Join(directory, summary.BackupFile))
	if !bytes.Equal(before, after) {
		t.Fatal("original recovery changed")
	}
	files, _ := os.ReadDir(directory)
	if len(files) != 1 {
		t.Fatal("temporary recovery remained", len(files))
	}
	for _, name := range []string{"../outside.json", "/root/file", "profiles-anything.json", summary.BackupFile + ".tmp"} {
		if _, e = loadProfileRecoveryAt(directory, name, now, portableRecoveryOwner); e == nil {
			t.Fatal("unbounded recovery path accepted")
		}
	}
	if _, e = saveProfileRecoveryAt(directory, r, now.Add(3*time.Minute), bytes.NewReader(nonce), portableRecoveryOwner); e == nil {
		t.Fatal("stale capture saved as fresh backup")
	}
	if _, e = saveProfileRecoveryAt(directory, r, now, bytes.NewReader(bytes.Repeat([]byte{2}, 8)), func(os.FileInfo, os.FileMode) error { return errors.New("wrong owner") }); e == nil {
		t.Fatal("ownership failure ignored")
	}
}
func TestProfilePrivateBundleRejectsProvenanceBoundsAndTrailingJSON(t *testing.T) {
	r := syntheticRecovery(t)
	bundle, e := r.bundle()
	if e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*privateProfileBundle){func(b *privateProfileBundle) { b.SchemaVersion = 2 }, func(b *privateProfileBundle) { b.ModuleSHA256 = "unknown" }, func(b *privateProfileBundle) { b.Profiles[0].Index = 2 }, func(b *privateProfileBundle) { b.Profiles[0].Frame = nil }, func(b *privateProfileBundle) { b.Profiles = b.Profiles[:1] }} {
		copy := bundle
		copy.Profiles = append([]privateProfileFrame(nil), bundle.Profiles...)
		change(&copy)
		raw, _ := json.Marshal(copy)
		if _, e = decodeProfileRecovery(raw, r.observedAt); e == nil {
			t.Fatal("invalid recovery accepted")
		}
	}
	raw, _ := json.Marshal(bundle)
	if _, e = decodeProfileRecovery(append(raw, []byte(" {}")...), r.observedAt); e == nil {
		t.Fatal("trailing recovery JSON accepted")
	}
	if _, e = decodeProfileRecovery(bytes.Repeat([]byte{'x'}, maximumProfileRecoveryBytes+1), r.observedAt); e == nil {
		t.Fatal("oversized recovery accepted")
	}
}
func TestProfileRecoveryRejectsSymlinkBeforeCreatingOutside(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	link := filepath.Join(base, "linked")
	if e := os.Symlink(outside, link); e != nil {
		t.Skip("symlink unavailable on this host")
	}
	if e := createProfileRecoveryDirectories(filepath.Join(link, "recovery")); e == nil {
		t.Fatal("symlinked parent accepted")
	}
	if _, e := os.Stat(filepath.Join(outside, "recovery")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("directory created through symlink")
	}
}
func FuzzProfileResponseBounds(f *testing.F) {
	f.Add(syntheticProfile(1, nil))
	f.Fuzz(func(t *testing.T, frame []byte) {
		_, _ = parseSavedAPNProfile(frame, 1)
		_, _ = parseSavedAPNProfile(frame, 2)
	})
}
