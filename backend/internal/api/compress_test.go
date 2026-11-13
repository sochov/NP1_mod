package api

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"testing"

	"github.com/np1-control-center/ncc/internal/webui"
)

func TestTextResponsesAreCompressedAndAssetsCached(t *testing.T) {
	s, c, _ := fixture(t)
	login(t, c, s)
	c.Transport.(*http.Transport).DisableCompression = true
	get := func(path string, headers map[string]string) *http.Response {
		req, _ := http.NewRequest("GET", s.URL+path, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		r, e := c.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	r := get("/api/v1/settings", nil)
	defer r.Body.Close()
	if r.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("JSON not compressed: %v", r.Header)
	}
	zr, e := gzip.NewReader(r.Body)
	if e != nil {
		t.Fatal(e)
	}
	var v map[string]any
	if e = json.NewDecoder(zr).Decode(&v); e != nil || v["revision"] == nil {
		t.Fatalf("decoded %v %v", v, e)
	}
	ranged := get("/api/v1/settings", map[string]string{"Range": "bytes=0-10"})
	io.Copy(io.Discard, ranged.Body)
	ranged.Body.Close()
	if ranged.Header.Get("Content-Encoding") != "" {
		t.Fatal("a range request was compressed")
	}
	entries, _ := fs.ReadDir(webui.Files, "dist/assets")
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".js") {
			continue
		}
		asset := get("/assets/"+entry.Name(), nil)
		io.Copy(io.Discard, asset.Body)
		asset.Body.Close()
		if asset.StatusCode != 200 || asset.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" || asset.Header.Get("Content-Encoding") != "gzip" {
			t.Fatalf("asset %d cache %q encoding %q", asset.StatusCode, asset.Header.Get("Cache-Control"), asset.Header.Get("Content-Encoding"))
		}
		break
	}
}
