package trips

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const Directory = "DRIVERECDATA/Video"
const MaximumIndexedSamples = 100000

var sidecarName = regexp.MustCompile(`(?i)^FILE([ME])_([0-9]{6})_([0-9]{6})_[0-9]+\.NMEA$`)

type SourceFile struct {
	Path     string
	Size     int64
	Modified time.Time
}
type Source interface {
	List(context.Context) ([]SourceFile, string, error)
	Open(string) (io.ReadCloser, error)
}
type cacheEntry struct {
	file      SourceFile
	report    ParseReport
	truncated bool
}
type Index struct {
	mu       sync.Mutex
	source   Source
	token    string
	at       time.Time
	cache    map[string]cacheEntry
	snapshot Snapshot
}
type Snapshot struct {
	Samples         []Sample  `json:"-"`
	Files           int       `json:"files"`
	UnreadableFiles int       `json:"unreadableFiles"`
	NoFix           int       `json:"noFix"`
	BadChecksum     int       `json:"badChecksum"`
	Malformed       int       `json:"malformed"`
	Truncated       bool      `json:"truncated"`
	IndexedAt       time.Time `json:"indexedAt"`
}

func NewIndex(source Source) *Index { return &Index{source: source, cache: map[string]cacheEntry{}} }
func (i *Index) Read(ctx context.Context) (Snapshot, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if identity, ok := i.source.(interface{ Identity() (string, error) }); ok {
		token, e := identity.Identity()
		if e != nil || token == "" {
			i.at = time.Time{}
			i.cache = map[string]cacheEntry{}
			if e == nil {
				e = errors.New("SD identity unavailable")
			}
			return Snapshot{}, e
		}
		if token != i.token {
			i.at = time.Time{}
			i.cache = map[string]cacheEntry{}
		}
	}
	if time.Since(i.at) < 30*time.Second {
		return i.snapshot, nil
	}
	files, token, e := i.source.List(ctx)
	if e != nil {
		i.at = time.Time{}
		i.cache = map[string]cacheEntry{}
		return Snapshot{}, e
	}
	if token == "" {
		return Snapshot{}, errors.New("SD identity unavailable")
	}
	if token != i.token {
		i.cache = map[string]cacheEntry{}
	}
	sort.Slice(files, func(a, b int) bool { return files[a].Path > files[b].Path })
	out := Snapshot{Samples: []Sample{}, IndexedAt: time.Now().UTC()}
	if len(files) > 5000 {
		files = files[:5000]
		out.Truncated = true
	}
	cache := map[string]cacheEntry{}
	for _, file := range files {
		if len(out.Samples) >= MaximumIndexedSamples {
			out.Truncated = true
			break
		}
		if e = ctx.Err(); e != nil {
			return Snapshot{}, e
		}
		if !sidecarName.MatchString(path.Base(file.Path)) {
			continue
		}
		if file.Size > MaxSidecarBytes {
			out.UnreadableFiles++
			continue
		}
		old, ok := i.cache[file.Path]
		if !ok || old.truncated || old.file.Size != file.Size || !old.file.Modified.Equal(file.Modified) {
			stream, err := i.source.Open(file.Path)
			if err != nil {
				out.UnreadableFiles++
				continue
			}
			report, err := ParseNMEA(stream, "stock-recording-nmea")
			stream.Close()
			if err != nil {
				out.UnreadableFiles++
				continue
			}
			old = cacheEntry{file: file, report: report}
		}
		remaining := MaximumIndexedSamples - len(out.Samples)
		if len(old.report.Samples) > remaining {
			// Retain the latest part of this sidecar and release its larger
			// backing array. Mark it incomplete so a later scan with room will
			// reparse, rather than treating the clipped cache as a complete file.
			old.report.Samples = append([]Sample(nil), old.report.Samples[len(old.report.Samples)-remaining:]...)
			old.truncated = true
			out.Truncated = true
		}
		cache[file.Path] = old
		out.Files++
		out.NoFix += old.report.NoFix
		out.BadChecksum += old.report.BadChecksum
		out.Malformed += old.report.Malformed
		out.Samples = append(out.Samples, old.report.Samples...)
	}
	_, after, e := i.source.List(ctx)
	if e != nil || after != token {
		return Snapshot{}, errors.New("SD changed during GNSS scan")
	}
	out.Samples = ordered(out.Samples)
	i.cache = cache
	i.token = token
	i.snapshot = out
	i.at = time.Now()
	return out, nil
}
func (i *Index) Statistics(ctx context.Context, now time.Time) (Statistics, error) {
	snapshot, e := i.Read(ctx)
	if e != nil {
		return Stats(nil, now), e
	}
	stats := Stats(snapshot.Samples, now)
	stats.SourceTruncated, stats.SourceFiles = snapshot.Truncated, snapshot.Files
	return stats, nil
}

// SDSource opens only recognized sidecars beneath a root boundary. MountToken must
// reject an absent/unmounted card and change when the card or mount changes.
type SDSource struct {
	Root       string
	MountToken func() (string, error)
}

func (s SDSource) Identity() (string, error) { return s.MountToken() }

func (s SDSource) List(ctx context.Context) ([]SourceFile, string, error) {
	token, e := s.MountToken()
	if e != nil {
		return nil, "", e
	}
	root, e := os.OpenRoot(s.Root)
	if e != nil {
		return nil, "", e
	}
	defer root.Close()
	for _, name := range []string{"DRIVERECDATA", Directory} {
		st, e := root.Lstat(name)
		if e != nil || st.Mode()&os.ModeSymlink != 0 {
			return nil, "", errors.New("invalid GNSS directory")
		}
	}
	dir, e := root.Open(Directory)
	if e != nil {
		return nil, "", e
	}
	defer dir.Close()
	entries, e := dir.ReadDir(20001)
	if e != nil && e != io.EOF {
		return nil, "", e
	}
	out := []SourceFile{}
	for _, entry := range entries {
		if e = ctx.Err(); e != nil {
			return nil, "", e
		}
		if entry.Type()&os.ModeSymlink != 0 || !sidecarName.MatchString(entry.Name()) {
			continue
		}
		st, e := entry.Info()
		if e != nil || !st.Mode().IsRegular() {
			continue
		}
		out = append(out, SourceFile{path.Join(Directory, entry.Name()), st.Size(), st.ModTime()})
	}
	return out, token, nil
}

type rootedReader struct {
	*os.File
	root *os.Root
}

func (r rootedReader) Close() error { e := r.File.Close(); r.root.Close(); return e }
func (s SDSource) Open(relative string) (io.ReadCloser, error) {
	if strings.Contains(relative, "\\") || path.Dir(relative) != Directory || !sidecarName.MatchString(path.Base(relative)) {
		return nil, errors.New("invalid sidecar path")
	}
	root, e := os.OpenRoot(s.Root)
	if e != nil {
		return nil, e
	}
	for _, name := range []string{"DRIVERECDATA", Directory, relative} {
		st, err := root.Lstat(name)
		if err != nil || st.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, errors.New("symlink or absent sidecar")
		}
	}
	stream, e := root.Open(relative)
	if e != nil {
		root.Close()
		return nil, e
	}
	return rootedReader{stream, root}, nil
}
