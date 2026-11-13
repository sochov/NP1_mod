package api

import (
	"compress/gzip"
	"net/http"
	"strings"
	"sync"
)

// Responses go to phones over 4G and the tailnet, so text is compressed: the dashboard
// summary (about 100 KB every 10 seconds) and the app bundle (about 730 KB) shrink to a
// fraction. Media, downloads, ranges, the event stream and camera sessions are left as they are.

var gzipPool = sync.Pool{New: func() any { w, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed); return w }}

func compressible(r *http.Request) bool {
	if r.Method != http.MethodGet || r.Header.Get("Range") != "" || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		return false
	}
	p := r.URL.Path
	for _, skip := range []string{"/api/v1/events", "/camera", "/recordings/", "/image", "/audio", "/download", "/uploads/"} {
		if strings.Contains(p, skip) {
			return false
		}
	}
	return true
}

type gzipResponse struct {
	http.ResponseWriter
	gz      *gzip.Writer
	decided bool
}

func (g *gzipResponse) decide() {
	if g.decided {
		return
	}
	g.decided = true
	h := g.Header()
	ct := h.Get("Content-Type")
	textual := strings.HasPrefix(ct, "application/json") || strings.HasPrefix(ct, "text/") || strings.Contains(ct, "javascript") || strings.HasPrefix(ct, "image/svg")
	if !textual || h.Get("Content-Encoding") != "" {
		return
	}
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	h.Add("Vary", "Accept-Encoding")
	gz := gzipPool.Get().(*gzip.Writer)
	gz.Reset(g.ResponseWriter)
	g.gz = gz
}

func (g *gzipResponse) WriteHeader(code int) {
	if code == http.StatusNoContent || code == http.StatusNotModified || code < 200 {
		g.decided = true
	}
	g.decide()
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipResponse) Write(b []byte) (int, error) {
	if !g.decided {
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(b))
		}
		g.decide()
	}
	if g.gz != nil {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzipResponse) Flush() {
	if g.gz != nil {
		g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (g *gzipResponse) Unwrap() http.ResponseWriter { return g.ResponseWriter }

func (g *gzipResponse) close() {
	if g.gz != nil {
		g.gz.Close()
		gzipPool.Put(g.gz)
		g.gz = nil
	}
}

func withCompression(w http.ResponseWriter, r *http.Request, next func(http.ResponseWriter)) {
	if !compressible(r) {
		next(w)
		return
	}
	g := &gzipResponse{ResponseWriter: w}
	defer g.close()
	next(g)
}
