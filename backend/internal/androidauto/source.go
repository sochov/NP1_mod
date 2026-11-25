package androidauto

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/androidauto/render"
)

// Source reads what the dashboard shows from the local NP1CC Web API, as the
// Android app does. The projection service has no access of its own to the
// plate store or the GNSS history.
type Source struct {
	Base   string // e.g. http://127.0.0.1/api/v1
	client *http.Client
	thumbs *thumbs

	mu       sync.Mutex
	state    render.State
	trailAt  time.Time
	platesOK bool
	pics     picsSnapshot
	err      string
}

// NewSource returns a source for the NP1CC API at base.
func NewSource(base string) *Source {
	jar, _ := cookiejar.New(nil)
	s := &Source{Base: base, client: &http.Client{Timeout: 8 * time.Second, Jar: jar}}
	s.thumbs = newThumbs(s)
	return s
}

func (s *Source) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Base+path, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		// Obtain a session cookie (the API has no password; see docs/security.md).
		if r2, e := s.client.Get(s.Base + "/auth/session"); e == nil {
			r2.Body.Close()
		}
		return fmt.Errorf("%s: %s", path, resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", path, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(v)
}

// State returns the newest snapshot.
func (s *Source) State() render.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state
	st.Signal = s.pics.signalAt(time.Now())
	return st
}

// Run polls until ctx ends.
func (s *Source) Run(ctx context.Context) {
	if r, e := s.client.Get(s.Base + "/auth/session"); e == nil {
		r.Body.Close()
	}
	go s.thumbs.run(ctx)
	go s.runPICS(ctx)
	gps := time.NewTicker(2 * time.Second)
	plates := time.NewTicker(1500 * time.Millisecond)
	slow := time.NewTicker(15 * time.Second)
	defer gps.Stop()
	defer plates.Stop()
	defer slow.Stop()
	s.pollGPS(ctx)
	s.pollPlates(ctx)
	s.pollStatus(ctx)
	s.pollTrail(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-gps.C:
			s.pollGPS(ctx)
		case <-plates.C:
			s.pollPlates(ctx)
		case <-slow.C:
			s.pollStatus(ctx)
			s.mu.Lock()
			due := time.Since(s.trailAt) > time.Minute
			s.mu.Unlock()
			if due {
				s.pollTrail(ctx)
			}
		}
	}
}

func (s *Source) pollGPS(ctx context.Context) {
	var v struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
		Data   struct {
			Fix       bool    `json:"fix"`
			Live      bool    `json:"live"`
			Lat       float64 `json:"lat"`
			Lon       float64 `json:"lon"`
			LastKnown *struct {
				Lat float64 `json:"lat"`
				Lon float64 `json:"lon"`
			} `json:"lastKnown"`
		} `json:"data"`
	}
	err := s.get(ctx, "/device/gps/live", &v)
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case err != nil:
		s.state.Live, s.state.PosNote = false, "NP1CCから位置を取得できません"
	case v.Data.Fix:
		s.state.Position = &render.LatLon{Lat: v.Data.Lat, Lon: v.Data.Lon}
		s.state.Live = v.Data.Live
		s.state.PosNote = ""
		if !v.Data.Live {
			s.state.PosNote = "最後に測位した位置を表示中"
		}
	default:
		s.state.Live = false
		if v.Data.LastKnown != nil {
			s.state.Position = &render.LatLon{Lat: v.Data.LastKnown.Lat, Lon: v.Data.LastKnown.Lon}
			s.state.PosNote = "測位待ち · 最後の位置を表示中"
		} else {
			s.state.PosNote = "測位待ち"
			if v.Reason != "" {
				s.state.PosNote = v.Reason
			}
		}
	}
}

func (s *Source) pollTrail(ctx context.Context) {
	now := time.Now().UTC()
	q := url.Values{"from": {now.Add(-24*time.Hour + time.Minute).Format(time.RFC3339)}, "to": {now.Format(time.RFC3339)}}
	var v struct {
		Data struct {
			Points []struct {
				Lat     float64 `json:"lat"`
				Lon     float64 `json:"lon"`
				Segment int     `json:"segment"`
			} `json:"points"`
		} `json:"data"`
	}
	if err := s.get(ctx, "/recordings/track?"+q.Encode(), &v); err != nil {
		return
	}
	var trail [][]render.LatLon
	seg := -1
	var last render.LatLon
	for _, p := range v.Data.Points {
		pt := render.LatLon{Lat: p.Lat, Lon: p.Lon}
		if p.Segment != seg || len(trail) == 0 {
			trail = append(trail, []render.LatLon{pt})
			seg, last = p.Segment, pt
			continue
		}
		// About 5 m apart is enough for a line on a car display.
		if math.Abs(pt.Lat-last.Lat) < 0.00004 && math.Abs(pt.Lon-last.Lon) < 0.00005 {
			continue
		}
		trail[len(trail)-1] = append(trail[len(trail)-1], pt)
		last = pt
	}
	s.mu.Lock()
	s.state.Trail, s.trailAt = trail, time.Now()
	s.mu.Unlock()
}

var japan = time.FixedZone("JST", 9*3600)

func (s *Source) pollPlates(ctx context.Context) {
	now := time.Now().In(japan)
	q := url.Values{"from": {now.AddDate(0, 0, -1).Format("2006-01-02")}, "to": {now.Format("2006-01-02")}, "limit": {"40"}}
	var v struct {
		Records []struct {
			Record struct {
				ID        string    `json:"id"`
				FirstSeen time.Time `json:"firstSeen"`
				Sightings int       `json:"sightings"`
				Reading   struct {
					Region     string  `json:"region"`
					Class      string  `json:"class"`
					Kana       string  `json:"kana"`
					Number     string  `json:"number"`
					Confidence float64 `json:"confidence"`
				} `json:"reading"`
				Matches []struct {
					RuleName string `json:"ruleName"`
				} `json:"matches"`
				Box      [4]int `json:"box"`
				Location *struct {
					Lat       float64 `json:"lat"`
					Lon       float64 `json:"lon"`
					Estimated bool    `json:"estimated"`
				} `json:"location"`
			} `json:"record"`
			ImageAvailable bool `json:"imageAvailable"`
			NumberShare    *struct {
				Status   string `json:"status"`
				Category string `json:"category"`
			} `json:"numberShare"`
		} `json:"records"`
	}
	err := s.get(ctx, "/plate/records?"+q.Encode(), &v)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		if !s.platesOK {
			s.state.PlatesNote = "ナンバーの記録を取得できません"
		}
		return
	}
	s.platesOK = true
	plates := make([]render.Plate, 0, len(v.Records))
	for _, r := range v.Records {
		p := render.Plate{ID: r.Record.ID, FirstSeen: r.Record.FirstSeen, Region: r.Record.Reading.Region, Class: r.Record.Reading.Class,
			Kana: r.Record.Reading.Kana, Number: r.Record.Reading.Number, Confidence: r.Record.Reading.Confidence, Sightings: r.Record.Sightings}
		for _, m := range r.Record.Matches {
			p.Matches = append(p.Matches, m.RuleName)
		}
		// Crops only for the rows a head unit can show.
		if p.Image = r.ImageAvailable; p.Image && len(plates) < 10 {
			p.Thumb = s.thumbs.get(p.ID, r.Record.Box)
		}
		if loc := r.Record.Location; loc != nil && (loc.Lat != 0 || loc.Lon != 0) {
			p.Located, p.Lat, p.Lon, p.Estimated = true, loc.Lat, loc.Lon, loc.Estimated
		}
		if ns := r.NumberShare; ns != nil && ns.Status == "exact" {
			p.Share = ns.Category
			if p.Share == "" {
				p.Share = "登録済み"
			}
			p.Danger = ns.Category == "危険"
		}
		plates = append(plates, p)
	}
	s.state.Plates = plates
	s.state.PlatesNote = ""
}

func (s *Source) pollStatus(ctx context.Context) {
	var ps struct {
		Enabled bool `json:"enabled"`
		Sensor  struct {
			State string `json:"state"`
		} `json:"sensor"`
	}
	perr := s.get(ctx, "/plate/status", &ps)
	var sum struct {
		Recording struct {
			Data struct {
				Writing bool `json:"writing"`
			} `json:"data"`
		} `json:"recording"`
	}
	serr := s.get(ctx, "/system/summary", &sum)
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case perr != nil:
		s.state.Sensor, s.state.SensorTone = "", 0
	case !ps.Enabled:
		s.state.Sensor, s.state.SensorTone = "ナンバー記録停止中", 0
	case ps.Sensor.State == "running":
		s.state.Sensor, s.state.SensorTone = "認識中", 1
	default:
		s.state.Sensor, s.state.SensorTone = "ナンバー認識停止", 2
	}
	s.state.Recording = ""
	if serr == nil && sum.Recording.Data.Writing {
		s.state.Recording = "録画中"
	}
}
