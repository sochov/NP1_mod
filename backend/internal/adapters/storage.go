package adapters

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/np1-control-center/ncc/internal/model"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

const SDRoot = "/mnt/sdcard"

func SDStatus() model.Observation {
	m := read("/proc/mounts")
	for _, l := range strings.Split(m, "\n") {
		f := strings.Fields(l)
		if len(f) > 3 && f[1] == SDRoot {
			data := map[string]any{"mount": SDRoot, "filesystem": f[2], "mode": "read-only API", "identity": f[0] + ":" + f[2]}
			for key, value := range diskUsage(SDRoot) {
				data[key] = value
			}
			return model.Value("SD mount/capacity", data)
		}
	}
	return model.Missing("/proc/mounts", "NO_SD: SDはmountされていません")
}
func OpenSD(relative string) (*os.Root, *os.File, error) {
	if SDStatus().Status != "available" {
		return nil, nil, errors.New("NO_SD")
	}
	return openContained(SDRoot, relative)
}
func openContained(base, relative string) (*os.Root, *os.File, error) {
	if strings.ContainsAny(relative, "\\\x00") || strings.HasPrefix(relative, "/") {
		return nil, nil, errors.New("invalid path")
	}
	for _, s := range strings.Split(relative, "/") {
		if s == ".." {
			return nil, nil, errors.New("path traversal")
		}
	}
	relative = path.Clean(relative)
	root, e := os.OpenRoot(base)
	if e != nil {
		return nil, nil, e
	}
	parts := strings.Split(relative, "/")
	for i := range parts {
		st, e := root.Lstat(strings.Join(parts[:i+1], "/"))
		if e != nil {
			root.Close()
			return nil, nil, e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, nil, errors.New("symlinks are not permitted")
		}
	}
	f, e := root.Open(relative)
	if e != nil {
		root.Close()
		return nil, nil, e
	}
	return root, f, nil
}

type FileEntry struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Size      string    `json:"size"`
	Modified  time.Time `json:"modified"`
	Directory bool      `json:"directory"`
	Media     string    `json:"media"`
}

func ListFiles(relative string) ([]FileEntry, error) {
	root, f, e := OpenSD(relative)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	defer f.Close()
	entries, e := f.ReadDir(301)
	if e != nil && len(entries) == 0 {
		return nil, e
	}
	if len(entries) > 300 {
		entries = entries[:300]
	}
	out := []FileEntry{}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		st, e := entry.Info()
		if e != nil {
			continue
		}
		media := "file"
		ext := strings.ToLower(path.Ext(st.Name()))
		if ext == ".mp4" || ext == ".ts" || ext == ".avi" {
			media = "video-candidate"
		}
		out = append(out, FileEntry{st.Name(), path.Join(relative, st.Name()), fmt.Sprint(st.Size()), st.ModTime().UTC(), st.IsDir(), media})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Directory != out[j].Directory {
			return out[i].Directory
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
func StreamFile(w http.ResponseWriter, r *http.Request) {
	root, f, e := OpenSD(r.URL.Query().Get("path"))
	if e != nil {
		http.Error(w, "NO_SD or invalid file", 400)
		return
	}
	defer root.Close()
	defer f.Close()
	serveOpenedFile(w, r, f)
}
func serveOpenedFile(w http.ResponseWriter, r *http.Request, f *os.File) {
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		http.Error(w, "not a regular file", 400)
		return
	}
	// SD files are untrusted content. Only verified MP4 containers can be inline.
	header := make([]byte, 12)
	_, _ = io.ReadFull(f, header)
	_, _ = f.Seek(0, 0)
	if bytes.Equal(header[4:8], []byte("ftyp")) && r.URL.Query().Get("download") != "1" {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Disposition", "inline")
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": st.Name()}))
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}
