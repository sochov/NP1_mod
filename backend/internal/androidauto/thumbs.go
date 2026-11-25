package androidauto

import (
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"sync"

	xdraw "golang.org/x/image/draw"
)

// thumbHeight is the stored height of a plate crop; the list draws it smaller.
const thumbHeight = 96

// thumbs keeps the crops of plate images (the detection frame cut to the
// plate's box), fetched one at a time from the NP1CC API.
type thumbs struct {
	src     *Source
	mu      sync.Mutex
	crops   map[string]*image.RGBA
	failed  map[string]bool
	order   []string
	queue   chan thumbJob
	pending map[string]bool
}

type thumbJob struct {
	id  string
	box [4]int
}

func newThumbs(src *Source) *thumbs {
	return &thumbs{src: src, crops: map[string]*image.RGBA{}, failed: map[string]bool{}, pending: map[string]bool{}, queue: make(chan thumbJob, 64)}
}

// get returns the crop when ready and otherwise queues it.
func (t *thumbs) get(id string, box [4]int) *image.RGBA {
	t.mu.Lock()
	defer t.mu.Unlock()
	if img, ok := t.crops[id]; ok {
		return img
	}
	if t.failed[id] || t.pending[id] || box[2] <= 0 || box[3] <= 0 {
		return nil
	}
	select {
	case t.queue <- thumbJob{id, box}:
		t.pending[id] = true
	default:
	}
	return nil
}

func (t *thumbs) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-t.queue:
			img, err := t.fetch(ctx, job)
			t.mu.Lock()
			delete(t.pending, job.id)
			if err != nil {
				t.failed[job.id] = true
			} else {
				t.crops[job.id] = img
				t.order = append(t.order, job.id)
				for len(t.order) > 60 {
					delete(t.crops, t.order[0])
					t.order = t.order[1:]
				}
			}
			t.mu.Unlock()
		}
	}
}

func (t *thumbs) fetch(ctx context.Context, job thumbJob) (*image.RGBA, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.src.Base+"/plate/records/"+url.PathEscape(job.id)+"/image", nil)
	if err != nil {
		return nil, err
	}
	resp, err := t.src.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("plate image: %s", resp.Status)
	}
	frame, err := jpeg.Decode(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	// The box is in 1920x1080 frame pixels, as on the Web UI.
	b := frame.Bounds()
	sx, sy := float64(b.Dx())/1920, float64(b.Dy())/1080
	r := image.Rect(int(float64(job.box[0])*sx), int(float64(job.box[1])*sy), int(float64(job.box[0]+job.box[2])*sx), int(float64(job.box[1]+job.box[3])*sy)).Add(b.Min).Intersect(b)
	if r.Dx() < 4 || r.Dy() < 4 {
		return nil, fmt.Errorf("plate box outside the image")
	}
	w := max(1, r.Dx()*thumbHeight/r.Dy())
	w = min(w, thumbHeight*4)
	out := image.NewRGBA(image.Rect(0, 0, w, thumbHeight))
	xdraw.CatmullRom.Scale(out, out.Bounds(), frame, r, draw.Src, nil)
	return out, nil
}
