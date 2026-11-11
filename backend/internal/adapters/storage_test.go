package adapters

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContainedOpenRejectsTraversal(t *testing.T) {
	base := t.TempDir()
	if e := os.WriteFile(filepath.Join(base, "safe.txt"), []byte("fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{"../safe.txt", "/etc/passwd", "folder/../../etc/passwd", "bad\\path", "nul\x00"} {
		root, f, e := openContained(base, p)
		if e == nil {
			f.Close()
			root.Close()
			t.Errorf("accepted %q", p)
		}
	}
	root, f, e := openContained(base, "safe.txt")
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
	root.Close()
}
func TestContainedOpenRejectsSymlink(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "private"), []byte("do not expose"), 0600)
	if e := os.Symlink(outside, filepath.Join(base, "escape")); e != nil {
		t.Skip("symlink creation unavailable on this host")
	}
	if root, f, e := openContained(base, "escape/private"); e == nil {
		f.Close()
		root.Close()
		t.Fatal("symlink escape accepted")
	}
}
func TestFileRangeAndUntrustedContent(t *testing.T) {
	base := t.TempDir()
	data := append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, bytes.Repeat([]byte{1}, 200)...)
	for _, tc := range []struct {
		name     string
		data     []byte
		wantType string
	}{{"video.mp4", data, "video/mp4"}, {"malicious.html", []byte("<script>fetch('/api/v1/settings')</script>"), "application/octet-stream"}} {
		p := filepath.Join(base, tc.name)
		os.WriteFile(p, tc.data, 0600)
		f, e := os.Open(p)
		if e != nil {
			t.Fatal(e)
		}
		req := httptest.NewRequest("GET", "http://127.0.0.1/file", nil)
		req.Header.Set("Range", "bytes=0-9")
		w := httptest.NewRecorder()
		serveOpenedFile(w, req, f)
		f.Close()
		if w.Code != 206 || w.Body.Len() != 10 || w.Header().Get("Content-Type") != tc.wantType {
			t.Fatalf("%s range/type failed: %d %v", tc.name, w.Code, w.Header())
		}
		if tc.name == "malicious.html" && !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment") {
			t.Fatal("untrusted HTML rendered inline")
		}
	}
}
