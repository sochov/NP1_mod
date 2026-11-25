package render

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultTileSource is the OpenFreeMap planet TileJSON used by the Web UI
// (OpenStreetMap data in the OpenMapTiles schema).
const DefaultTileSource = "https://tiles.openfreemap.org/planet"

// TileAttribution is drawn on the map.
const TileAttribution = "© OpenStreetMap · OpenFreeMap"

// maxVectorZoom is the deepest zoom OpenFreeMap serves; deeper raster tiles
// are drawn from the zoom-14 tile.
const maxVectorZoom = 14

type tileKey struct {
	z, x, y int
	dark    bool
}

type vecKey struct{ z, x, y int }

type labelKey struct {
	v    vecKey
	z    int
	dark bool
}

// Tiles fetches vector tiles in the background, keeps the decoded ones in
// memory and the files on disk, and renders 256 px raster tiles from them.
type Tiles struct {
	Source    string // TileJSON URL
	Dir       string // disk cache; empty disables it
	DiskLimit int64
	MemTiles  int
	Client    *http.Client
	OnReady   func() // called when a tile becomes available

	mu         sync.Mutex
	raster     map[tileKey]*image.RGBA
	rasterUsed map[tileKey]int64
	vec        map[vecKey]mvtTile
	vecUsed    map[vecKey]int64
	labels     map[labelKey][]mapLabel
	tick       int64
	pending    map[vecKey]bool
	failed     map[vecKey]time.Time
	offline    time.Time
	sem        chan struct{}
	pruned     time.Time
	template   string
	templateAt time.Time
}

func (t *Tiles) init() {
	if t.raster == nil {
		t.raster = map[tileKey]*image.RGBA{}
		t.rasterUsed = map[tileKey]int64{}
		t.vec = map[vecKey]mvtTile{}
		t.vecUsed = map[vecKey]int64{}
		t.labels = map[labelKey][]mapLabel{}
		t.pending = map[vecKey]bool{}
		t.failed = map[vecKey]time.Time{}
		t.sem = make(chan struct{}, 2)
		if t.MemTiles == 0 {
			t.MemTiles = 64
		}
		if t.Client == nil {
			t.Client = &http.Client{Timeout: 20 * time.Second}
		}
		if t.Source == "" {
			t.Source = DefaultTileSource
		}
	}
}

func vectorKey(z, x, y int) vecKey {
	vz := min(z, maxVectorZoom)
	d := z - vz
	return vecKey{vz, x >> d, y >> d}
}

func wrapTile(z, x, y int) (int, int, bool) {
	n := 1 << z
	if y < 0 || y >= n {
		return 0, 0, false
	}
	return ((x % n) + n) % n, y, true
}

// vector returns the decoded vector tile, starting a load when missing.
func (t *Tiles) vector(ctx context.Context, k vecKey) mvtTile {
	if v, ok := t.vec[k]; ok {
		t.tick++
		t.vecUsed[k] = t.tick
		return v
	}
	if !t.pending[k] && time.Since(t.failed[k]) > time.Minute {
		t.pending[k] = true
		go t.load(ctx, k)
	}
	return nil
}

// Get returns the raster tile if its data is ready, and otherwise starts
// loading it.
func (t *Tiles) Get(ctx context.Context, z, x, y int, dark bool) *image.RGBA {
	x, y, ok := wrapTile(z, x, y)
	if !ok {
		return nil
	}
	k := tileKey{z, x, y, dark}
	t.mu.Lock()
	t.init()
	if img, ok := t.raster[k]; ok {
		t.tick++
		t.rasterUsed[k] = t.tick
		t.mu.Unlock()
		return img
	}
	vk := vectorKey(z, x, y)
	v := t.vector(ctx, vk)
	t.mu.Unlock()
	if v == nil {
		return nil
	}
	img := renderBasemap(v, z, x, y, vk.z, dark)
	t.mu.Lock()
	t.tick++
	t.raster[k], t.rasterUsed[k] = img, t.tick
	evict(t.raster, t.rasterUsed, t.MemTiles)
	t.mu.Unlock()
	return img
}

// Labels returns the label candidates of the vector tile under raster tile
// (z, x, y), in world pixels at zoom z.
func (t *Tiles) Labels(ctx context.Context, z, x, y int, dark bool) ([]mapLabel, vecKey) {
	x, y, ok := wrapTile(z, x, y)
	if !ok {
		return nil, vecKey{}
	}
	vk := vectorKey(z, x, y)
	lk := labelKey{vk, z, dark}
	t.mu.Lock()
	t.init()
	if l, ok := t.labels[lk]; ok {
		t.mu.Unlock()
		return l, vk
	}
	v := t.vector(ctx, vk)
	t.mu.Unlock()
	if v == nil {
		return nil, vk
	}
	labels := tileLabels(v, z, vk.z, vk.x, vk.y, dark)
	t.mu.Lock()
	if len(t.labels) > 64 {
		t.labels = map[labelKey][]mapLabel{}
	}
	t.labels[lk] = labels
	t.mu.Unlock()
	return labels, vk
}

func evict[K comparable, V any](m map[K]V, used map[K]int64, limit int) {
	for len(m) > limit {
		var oldest K
		var at int64 = 1<<63 - 1
		for k, u := range used {
			if u < at {
				oldest, at = k, u
			}
		}
		delete(m, oldest)
		delete(used, oldest)
	}
}

func (t *Tiles) load(ctx context.Context, k vecKey) {
	select {
	case t.sem <- struct{}{}:
	case <-ctx.Done():
		t.done(k, nil)
		return
	}
	defer func() { <-t.sem }()
	raw, err := t.read(ctx, k)
	if err != nil {
		t.done(k, nil)
		return
	}
	v, err := decodeMVT(raw, basemapLayers)
	if err != nil {
		t.done(k, nil)
		return
	}
	t.done(k, v)
	if t.OnReady != nil {
		t.OnReady()
	}
}

func (t *Tiles) done(k vecKey, v mvtTile) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.pending, k)
	if v == nil {
		t.failed[k] = time.Now()
		return
	}
	delete(t.failed, k)
	t.tick++
	t.vec[k], t.vecUsed[k] = v, t.tick
	evict(t.vec, t.vecUsed, 24)
}

func (t *Tiles) path(k vecKey) string {
	return filepath.Join(t.Dir, "v", fmt.Sprint(k.z), fmt.Sprint(k.x), fmt.Sprintf("%d.pbf", k.y))
}

func (t *Tiles) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "NP1ControlCenter-AndroidAuto/1")
	resp, err := t.Client.Do(req)
	if err != nil {
		t.mu.Lock()
		t.offline = time.Now()
		t.mu.Unlock()
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// tileTemplate resolves the versioned tile URL from the TileJSON.
func (t *Tiles) tileTemplate(ctx context.Context) (string, error) {
	t.mu.Lock()
	tpl, at := t.template, t.templateAt
	t.mu.Unlock()
	if tpl != "" && time.Since(at) < 12*time.Hour {
		return tpl, nil
	}
	saved := filepath.Join(t.Dir, "tiles-url.txt")
	b, err := t.get(ctx, t.Source, 1<<20)
	if err == nil {
		var tj struct {
			Tiles []string `json:"tiles"`
		}
		if json.Unmarshal(b, &tj) == nil && len(tj.Tiles) > 0 && strings.HasPrefix(tj.Tiles[0], "https://") {
			tpl = tj.Tiles[0]
			if t.Dir != "" && os.MkdirAll(t.Dir, 0750) == nil {
				_ = os.WriteFile(saved, []byte(tpl), 0640)
			}
		}
	}
	if tpl == "" && t.Dir != "" {
		if b, e := os.ReadFile(saved); e == nil {
			tpl = strings.TrimSpace(string(b))
		}
	}
	if tpl == "" {
		return "", errors.New("no tile URL")
	}
	t.mu.Lock()
	t.template, t.templateAt = tpl, time.Now()
	t.mu.Unlock()
	return tpl, nil
}

func (t *Tiles) read(ctx context.Context, k vecKey) ([]byte, error) {
	var raw []byte
	if t.Dir != "" {
		if st, e := os.Stat(t.path(k)); e == nil && time.Since(st.ModTime()) < 30*24*time.Hour {
			raw, _ = os.ReadFile(t.path(k))
		}
	}
	if raw == nil {
		t.mu.Lock()
		offline := time.Since(t.offline) < 20*time.Second
		t.mu.Unlock()
		if offline {
			// Serve a stale copy rather than nothing while offline.
			if b, e := os.ReadFile(t.path(k)); e == nil {
				return gunzipped(b)
			}
			return nil, errors.New("offline")
		}
		tpl, err := t.tileTemplate(ctx)
		if err != nil {
			if b, e := os.ReadFile(t.path(k)); e == nil {
				return gunzipped(b)
			}
			return nil, err
		}
		url := strings.NewReplacer("{z}", fmt.Sprint(k.z), "{x}", fmt.Sprint(k.x), "{y}", fmt.Sprint(k.y)).Replace(tpl)
		if raw, err = t.get(ctx, url, 4<<20); err != nil {
			if b, e := os.ReadFile(t.path(k)); e == nil {
				return gunzipped(b)
			}
			return nil, err
		}
		if t.Dir != "" {
			p := t.path(k)
			if os.MkdirAll(filepath.Dir(p), 0750) == nil && os.WriteFile(p+".tmp", raw, 0640) == nil {
				_ = os.Rename(p+".tmp", p)
			}
			t.prune()
		}
	}
	return gunzipped(raw)
}

func gunzipped(b []byte) ([]byte, error) {
	if len(b) < 2 || b[0] != 0x1f || b[1] != 0x8b {
		return b, nil
	}
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, 16<<20))
}

// prune keeps the disk cache under its limit, oldest files first.
func (t *Tiles) prune() {
	t.mu.Lock()
	if time.Since(t.pruned) < 10*time.Minute || t.DiskLimit <= 0 {
		t.mu.Unlock()
		return
	}
	t.pruned = time.Now()
	t.mu.Unlock()
	type file struct {
		path string
		size int64
		at   time.Time
	}
	var files []file
	var total int64
	_ = filepath.WalkDir(filepath.Join(t.Dir, "v"), func(p string, d fs.DirEntry, e error) error {
		if e != nil || d.IsDir() {
			return nil
		}
		if info, e := d.Info(); e == nil {
			files = append(files, file{p, info.Size(), info.ModTime()})
			total += info.Size()
		}
		return nil
	})
	if total <= t.DiskLimit {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at.Before(files[j].at) })
	for _, f := range files {
		if total <= t.DiskLimit*8/10 {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
}
