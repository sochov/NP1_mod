package adapters

import (
	"context"
	"encoding/binary"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCameraTailAllowsWriterHandoffBeforeDeclaringStop(t *testing.T) {
	for name, size := range map[string]uint32{"finalized": 8, "open-ended": 0} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "DRIVERECDATA", "Video")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			header := make([]byte, 16)
			binary.BigEndian.PutUint32(header, 8)
			copy(header[4:], "ftyp")
			binary.BigEndian.PutUint32(header[8:], size)
			copy(header[12:], "mdat")
			relative := "DRIVERECDATA/Video/FILEM_261112_030000_0.MP4"
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(relative)), header, 0600); err != nil {
				t.Fatal(err)
			}
			r, f, err := openContained(root, relative)
			if err != nil {
				t.Fatal(err)
			}
			tail := cameraH264Tail{env: cameraTailEnvironment{root: root, mount: func() (string, error) { return "same", nil }, now: time.Now}, source: "front", token: "same", relative: relative, root: r, file: f, offset: 16, lastSample: time.Now().Add(-10 * time.Second), lastRead: time.Now().Add(-10 * time.Second)}
			defer tail.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			if _, err := tail.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("clip declared dead before next-file handoff", err)
			}
			tail.lastSample = time.Now().Add(-31 * time.Second)
			if _, err := tail.Next(context.Background()); err == nil || err.Error() != "camera recording stopped" {
				t.Fatal("stopped writer was not bounded", err)
			}
		})
	}
}

func TestCameraTailAdvancesWhilePreviousMDATStillOpenEnded(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "DRIVERECDATA", "Video")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	header := make([]byte, 16)
	binary.BigEndian.PutUint32(header, 8)
	copy(header[4:], "ftyp")
	copy(header[12:], "mdat")
	old := "FILEM_261112_030000_0.MP4"
	next := "FILEM_261112_030300_0.MP4"
	for _, name := range []string{old, next, "FILEM_261112_025700_0.MP4"} {
		if err := os.WriteFile(filepath.Join(dir, name), header, 0600); err != nil {
			t.Fatal(err)
		}
	}
	fileRoot, file, err := openContained(root, "DRIVERECDATA/Video/"+old)
	if err != nil {
		t.Fatal(err)
	}
	defer fileRoot.Close()
	tail := cameraH264Tail{env: cameraTailEnvironment{root: root, mount: func() (string, error) { return "same-card", nil }, now: time.Now}, source: "front", token: "same-card", relative: "DRIVERECDATA/Video/" + old, root: fileRoot, file: file, offset: 16, lastSample: time.Now().Add(-6 * time.Second), lastRead: time.Now().Add(-2 * time.Second)}
	defer tail.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _ = tail.Next(ctx)
	if tail.relative != "DRIVERECDATA/Video/"+next {
		t.Fatal("stalled on open-ended old file", tail.relative)
	}
	if err := tail.rotate(); err == nil {
		t.Fatal("rotated backwards to an older open-ended file")
	}
}

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
