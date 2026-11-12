package nasbackup

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func box(kind string, payload ...[]byte) []byte {
	size := 8
	for _, v := range payload {
		size += len(v)
	}
	out := make([]byte, 8, size)
	binary.BigEndian.PutUint32(out, uint32(size))
	copy(out[4:], kind)
	for _, v := range payload {
		out = append(out, v...)
	}
	return out
}
func originalTestBytes() []byte {
	movie := make([]byte, 20)
	binary.BigEndian.PutUint32(movie[12:], 1000)
	binary.BigEndian.PutUint32(movie[16:], 60000)
	track := func(audio bool) []byte {
		handler := make([]byte, 12)
		entry := make([]byte, 86)
		copy(handler[8:], "vide")
		copy(entry[4:], "avc1")
		binary.BigEndian.PutUint16(entry[32:], 1920)
		binary.BigEndian.PutUint16(entry[34:], 1080)
		entry = append(entry, box("avcC", []byte{1, 100, 0, 40, 255, 225, 0, 2, 0x67, 0, 1, 0, 2, 0x68, 0})...)
		if audio {
			copy(handler[8:], "soun")
			entry = make([]byte, 36)
			copy(entry[4:], "mp4a")
			binary.BigEndian.PutUint16(entry[24:], 2)
			binary.BigEndian.PutUint32(entry[32:], 48000<<16)
			config := make([]byte, 13)
			config[0], config[1] = 0x40, 0x15
			config = append(config, 5, 2, 0x11, 0x90)
			es := append([]byte{0, 1, 0, 4, byte(len(config))}, config...)
			entry = append(entry, box("esds", append([]byte{0, 0, 0, 0, 3, byte(len(es))}, es...))...)
		}
		binary.BigEndian.PutUint32(entry, uint32(len(entry)))
		return box("trak", box("mdia", box("hdlr", handler), box("minf", box("stbl", box("stsd", []byte{0, 0, 0, 0, 0, 0, 0, 1}, entry)))))
	}
	return bytes.Join([][]byte{box("ftyp", []byte("isom0000")), box("mdat", bytes.Repeat([]byte("synthetic-original"), 16000)), box("moov", box("mvhd", movie), track(false), track(true))}, nil)
}

type fakeEntry struct {
	data     []byte
	identity string
}
type fakeRemote struct {
	files                  map[string]*fakeEntry
	writes, renames, opens int
	onWrite                func()
	onRename               func(string)
	badRead                bool
}

func newFakeRemote() *fakeRemote        { return &fakeRemote{files: map[string]*fakeEntry{}} }
func (r *fakeRemote) TargetKey() string { return strings.Repeat("f", 64) }
func (r *fakeRemote) Stat(_ context.Context, name string) (RemoteInfo, error) {
	f, ok := r.files[name]
	if !ok {
		return RemoteInfo{}, os.ErrNotExist
	}
	return RemoteInfo{int64(len(f.data)), true, f.identity}, nil
}
func (r *fakeRemote) Open(_ context.Context, name string) (io.ReadCloser, error) {
	f, ok := r.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	data := append([]byte(nil), f.data...)
	if r.badRead && len(data) > 0 {
		data[0] ^= 1
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
func (r *fakeRemote) OpenPart(_ context.Context, name string, exclusive bool) (Part, error) {
	r.opens++
	f, ok := r.files[name]
	if exclusive {
		if ok {
			return nil, os.ErrExist
		}
		f = &fakeEntry{identity: "owned-birth"}
		r.files[name] = f
	} else if !ok {
		return nil, os.ErrNotExist
	}
	return &fakePart{r: r, f: f}, nil
}
func (r *fakeRemote) RenameNoReplace(_ context.Context, a, b string) error {
	r.renames++
	if r.onRename != nil {
		r.onRename(b)
	}
	if _, ok := r.files[b]; ok {
		return os.ErrExist
	}
	r.files[b] = r.files[a]
	delete(r.files, a)
	return nil
}

type fakePart struct {
	r      *fakeRemote
	f      *fakeEntry
	offset int64
}

func (f *fakePart) Write(data []byte) (int, error) {
	f.r.writes++
	end := int(f.offset) + len(data)
	if end > len(f.f.data) {
		f.f.data = append(f.f.data, make([]byte, end-len(f.f.data))...)
	}
	copy(f.f.data[int(f.offset):], data)
	f.offset = int64(end)
	if f.r.onWrite != nil {
		f.r.onWrite()
	}
	return len(data), nil
}
func (f *fakePart) Seek(offset int64, whence int) (int64, error) {
	if whence != io.SeekStart || offset < 0 {
		return 0, errors.New("fixture seek")
	}
	f.offset = offset
	return offset, nil
}
func (f *fakePart) Sync() error  { return nil }
func (f *fakePart) Close() error { return nil }
func (f *fakePart) Stat() (RemoteInfo, error) {
	return RemoteInfo{int64(len(f.f.data)), true, f.f.identity}, nil
}
func sourceFixture(t *testing.T, raw []byte) *os.File {
	t.Helper()
	name := filepath.Join(privateTempDir(t), "FILEM_261112_120000_0000.MP4")
	if err := os.WriteFile(name, raw, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return file
}
func engineFixture(t *testing.T) (*Engine, *fakeRemote, *os.File, []byte) {
	t.Helper()
	raw := originalTestBytes()
	file := sourceFixture(t, raw)
	remote := newFakeRemote()
	engine, err := NewEngine(privateTempDir(t), remote)
	if err != nil {
		t.Fatal(err)
	}
	return engine, remote, file, raw
}
func partEntry(t *testing.T, r *fakeRemote) (string, *fakeEntry) {
	t.Helper()
	for name, f := range r.files {
		if strings.HasSuffix(name, ".part") {
			return name, f
		}
	}
	t.Fatal("no retained part")
	return "", nil
}

func TestBackupVerifiedOriginalAtomicFinalizeAndDeduplicate(t *testing.T) {
	e, r, file, raw := engineFixture(t)
	result, err := e.Backup(context.Background(), file)
	if err != nil || result.Status != "complete" || result.ResumedBytes != "0" || result.Camera != "front" || result.StartAt.Format(time.RFC3339) != "2026-11-12T03:00:00Z" || r.renames != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(r.files) != 1 {
		t.Fatal("part not atomically renamed")
	}
	for name, f := range r.files {
		if !strings.HasPrefix(name, "FILEM_261112_120000_0000-") || !bytes.Equal(f.data, raw) {
			t.Fatal("original name/bytes not preserved")
		}
	}
	writes, opens := r.writes, r.opens
	result, err = e.Backup(context.Background(), file)
	if err != nil || result.Status != "already-complete" || r.writes != writes || r.opens != opens || r.renames != 1 {
		t.Fatal("duplicate was re-uploaded", err)
	}
}
func TestBackupCancellationResumesOnlyVerifiedOwnedPrefix(t *testing.T) {
	e, r, file, raw := engineFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	r.onWrite = cancel
	result, err := e.Backup(ctx, file)
	if !errors.Is(err, context.Canceled) || result.Status != "cancelled" {
		t.Fatal("cancel not respected", err)
	}
	_, part := partEntry(t, r)
	prefix := len(part.data)
	if prefix <= 0 || prefix >= len(raw) || r.renames != 0 {
		t.Fatal("cancelled part not retained correctly")
	}
	r.onWrite = nil
	result, err = e.Backup(context.Background(), file)
	if err != nil || result.Status != "complete" || result.ResumedBytes == "0" {
		t.Fatalf("resume result=%+v err=%v", result, err)
	}
	for _, final := range r.files {
		if !bytes.Equal(final.data, raw) {
			t.Fatal("resume changed original bytes")
		}
	}
}
func TestBackupUnknownOrChangedPartNeverOverwrites(t *testing.T) {
	for _, mode := range []string{"journal-missing", "prefix-changed", "identity-changed"} {
		t.Run(mode, func(t *testing.T) {
			e, r, file, _ := engineFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			r.onWrite = cancel
			_, _ = e.Backup(ctx, file)
			r.onWrite = nil
			name, part := partEntry(t, r)
			switch mode {
			case "journal-missing":
				names, _ := filepath.Glob(filepath.Join(e.directory, "*.json"))
				for _, name := range names {
					if err := os.Remove(name); err != nil {
						t.Fatal(err)
					}
				}
			case "prefix-changed":
				part.data[0] ^= 1
			case "identity-changed":
				part.identity = "foreign-birth"
			}
			before := append([]byte(nil), part.data...)
			writes := r.writes
			result, err := e.Backup(context.Background(), file)
			if err == nil || result.Status == "complete" || r.writes != writes || !bytes.Equal(r.files[name].data, before) || r.renames != 0 {
				t.Fatal("unknown/changed part modified", err)
			}
		})
	}
}
func TestBackupReadbackMismatchSourceChangeAndRenameRaceFailClosed(t *testing.T) {
	for _, mode := range []string{"readback", "source", "rename-race"} {
		t.Run(mode, func(t *testing.T) {
			e, r, file, _ := engineFixture(t)
			switch mode {
			case "readback":
				r.badRead = true
			case "source":
				r.onWrite = func() { _ = os.Chtimes(file.Name(), time.Now().Add(time.Hour), time.Now().Add(time.Hour)) }
			case "rename-race":
				r.onRename = func(name string) { r.files[name] = &fakeEntry{data: []byte("foreign file"), identity: "foreign"} }
			}
			result, err := e.Backup(context.Background(), file)
			if err == nil || result.Status == "complete" {
				t.Fatal("failure incorrectly succeeded")
			}
			if mode != "rename-race" && r.renames != 0 {
				t.Fatal("unverified data finalized")
			}
			if mode == "rename-race" {
				for name, f := range r.files {
					if strings.HasSuffix(name, ".MP4") && string(f.data) != "foreign file" {
						t.Fatal("existing final overwritten")
					}
				}
			}
		})
	}
}
func TestBackupRejectsUnfinalizedOrUnsupportedMediaBeforeNetwork(t *testing.T) {
	for _, raw := range [][]byte{[]byte("not a finalized MP4"), bytes.Replace(originalTestBytes(), []byte("avcC"), []byte("xxxx"), 1), bytes.Replace(originalTestBytes(), []byte("esds"), []byte("xxxx"), 1)} {
		r := newFakeRemote()
		e, err := NewEngine(privateTempDir(t), r)
		if err != nil {
			t.Fatal(err)
		}
		file := sourceFixture(t, raw)
		if _, err = e.Backup(context.Background(), file); err == nil || len(r.files) != 0 || r.opens != 0 {
			t.Fatal("unconfirmed media reached remote")
		}
	}
}
func TestBackupBusyAndPreCancelledNeverWrite(t *testing.T) {
	e, r, file, _ := engineFixture(t)
	e.mu.Lock()
	_, err := e.Backup(context.Background(), file)
	e.mu.Unlock()
	if err == nil || err.Error() != "NAS_BUSY" {
		t.Fatal("parallel backup accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = e.Backup(ctx, file)
	if !errors.Is(err, context.Canceled) || len(r.files) != 0 {
		t.Fatal("pre-cancel wrote remote data")
	}
}
