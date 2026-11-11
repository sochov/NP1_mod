package adapters

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordingOpenRequiresIDFinalizationAndCurrentCard(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, recordingDirectory)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	name := "FILEM_261112_120000_0000.MP4"
	filename := filepath.Join(dir, name)
	if err := os.WriteFile(filename, recordingFixture(60), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	_ = os.Chtimes(filename, old, old)
	token := "fixture-card"
	index := NewRecordingIndex(base, func() (string, error) { return token, nil })
	id := recordingID(token, recordingDirectory+"/"+name)
	root, file, clip, err := index.Open(context.Background(), id)
	if err != nil || clip.Name != name || clip.ID != id {
		t.Fatal("safe open failed", err)
	}
	file.Close()
	root.Close()
	for _, bad := range []string{filename, "../" + name, recordingID("other-card", recordingDirectory+"/"+name)} {
		if _, _, _, err = index.Open(context.Background(), bad); err == nil {
			t.Fatal("arbitrary/stale ID accepted")
		}
	}
	token = "replacement-card"
	if _, _, _, err = index.Open(context.Background(), id); err == nil || err.Error() != "SD_CHANGED" {
		t.Fatal("changed card accepted")
	}
	token = "fixture-card"
	now := time.Now()
	_ = os.Chtimes(filename, now, now)
	if _, _, _, err = index.Open(context.Background(), id); err == nil || err.Error() != "RECORDING_NOT_FINALIZED" {
		t.Fatal("recent writer accepted")
	}
}
