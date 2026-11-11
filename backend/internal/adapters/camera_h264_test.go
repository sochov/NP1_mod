package adapters

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCameraTailSelectionKeepsCameraAndFilenameBoundary(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "DRIVERECDATA", "Video")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"FILEM_261112_030000_0.MP4", "FILEM_261112_030300_0.MP4", "FILEE_261112_030600_0.MP4", "notes.mp4"} {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, 32), 0600); err != nil {
			t.Fatal(err)
		}
	}
	env := cameraTailEnvironment{root: root, now: time.Now}
	front, err := cameraTailCandidates(env, "front")
	if err != nil || len(front) != 2 || filepath.Base(front[0].relative) != "FILEM_261112_030300_0.MP4" {
		t.Fatal("camera/order mismatch", err, front)
	}
	cabin, err := cameraTailCandidates(env, "in")
	if err != nil || len(cabin) != 1 {
		t.Fatal("cabin mismatch", err)
	}
	if _, err := cameraTailCandidates(env, "../etc"); err == nil {
		t.Fatal("arbitrary source accepted")
	}
	// A replacement at the same name must not inherit the inventory's identity.
	item := front[0]
	full := filepath.Join(root, filepath.FromSlash(item.relative))
	if err := os.Rename(full, full+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, make([]byte, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if r, f, _, err := openTailCandidate(env, item); err == nil {
		f.Close()
		r.Close()
		t.Fatal("replacement inherited current file proof")
	}
}

func TestCameraH264RejectsArbitraryOrDuplicateSourcesBeforeOpeningSD(t *testing.T) {
	for _, query := range []string{"", "source=unknown", "source=front&source=in", "source=front&path=elsewhere"} {
		w := httptest.NewRecorder()
		HandleCameraH264(w, httptest.NewRequest("GET", "/camera/h264?"+query, nil))
		if w.Code != 400 {
			t.Fatal("invalid source accepted", query, w.Code)
		}
	}
}
