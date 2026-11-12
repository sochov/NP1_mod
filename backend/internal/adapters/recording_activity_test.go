package adapters

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordingActivityMeasuresProgressRotationAndStops(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, recordingDirectory)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	write := func(name string, size int, at time.Time) {
		t.Helper()
		file := filepath.Join(dir, name)
		if err := os.WriteFile(file, make([]byte, size), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file, at, at); err != nil {
			t.Fatal(err)
		}
	}
	front, cabin := "FILEM_261112_120000_0000.MP4", "FILEE_261112_120000_0000.MP4"
	write(front, 100, now)
	write(cabin, 100, now)
	index := NewRecordingIndex(root, func() (string, error) { return "card-one", nil })
	read := func(at time.Time) RecordingActivity {
		t.Helper()
		v, e := index.Activity(context.Background(), at)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	if v := read(now); v.Writing != nil {
		t.Fatal("first recent file is not proof of ongoing recording", v)
	}
	write(front, 200, now.Add(3*time.Second))
	// Preallocated files may update their mtime without growing.
	write(cabin, 100, now.Add(3*time.Second))
	if v := read(now.Add(3 * time.Second)); v.Writing == nil || !*v.Writing || !*v.Cameras["front"] || !*v.Cameras["in"] {
		t.Fatal(v)
	}
	if v := read(now.Add(20 * time.Second)); v.Writing == nil || *v.Writing {
		t.Fatal("stopped files must expire", v)
	}
	write("FILEM_261112_120300_0000.MP4", 50, now.Add(21*time.Second))
	if v := read(now.Add(23 * time.Second)); v.Writing == nil || !*v.Writing || *v.Cameras["in"] {
		t.Fatal("rotation or individual camera state", v)
	}
	if v := read(now.Add(60 * time.Second)); v.Writing == nil || *v.Writing {
		t.Fatal("old progress after a long polling gap", v)
	}
}

func TestRecordingActivityCardChangeAndUnavailableNeverReuseEvidence(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, recordingDirectory)
	os.MkdirAll(dir, 0700)
	now := time.Now().UTC().Truncate(time.Second)
	file := filepath.Join(dir, "FILEM_261112_120000_0000.MP4")
	os.WriteFile(file, []byte("first"), 0600)
	os.Chtimes(file, now, now)
	token := "card-one"
	var mountError error
	index := NewRecordingIndex(root, func() (string, error) { return token, mountError })
	index.Activity(context.Background(), now)
	os.WriteFile(file, []byte("more bytes"), 0600)
	os.Chtimes(file, now.Add(3*time.Second), now.Add(3*time.Second))
	v, err := index.Activity(context.Background(), now.Add(3*time.Second))
	if err != nil || v.Writing == nil || !*v.Writing {
		t.Fatal(v, err)
	}
	token = "card-two"
	v, err = index.Activity(context.Background(), now.Add(4*time.Second))
	if err != nil || v.Writing != nil {
		t.Fatal("new card must not inherit progress", v, err)
	}
	mountError = errors.New("card removed")
	v, err = index.Activity(context.Background(), now.Add(5*time.Second))
	if err == nil || v.Writing != nil {
		t.Fatal(v, err)
	}
	mountError = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = index.Activity(ctx, now.Add(8*time.Second)); err == nil {
		t.Fatal("cancelled scan accepted")
	}
}
