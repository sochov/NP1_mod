package trips

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// LiveIndex reads only the newest sidecars. The large history index has a
// 30-second cache and must not determine whether a five-second fix is current.
type LiveIndex struct {
	mu      sync.Mutex
	source  Source
	clock   func() time.Time
	at      time.Time
	token   string
	samples []Sample
}

func NewLiveIndex(source Source) *LiveIndex { return &LiveIndex{source: source, clock: time.Now} }

func (i *LiveIndex) Latest(ctx context.Context, at time.Time) (*Sample, error) {
	return i.LatestWithin(ctx, at, 5*time.Second)
}

// LatestWithin returns the newest usable sample no older than maxAge. The stock recorder
// writes a clip's NMEA sidecar only when the clip closes (about every three minutes), so
// while driving the newest sample is usually one to three minutes old.
func (i *LiveIndex) LatestWithin(ctx context.Context, at time.Time, maxAge time.Duration) (*Sample, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if identity, ok := i.source.(interface{ Identity() (string, error) }); ok {
		token, e := identity.Identity()
		if e != nil || token == "" {
			i.at = time.Time{}
			i.samples = nil
			return nil, errors.New("GNSS source unavailable")
		}
		if token != i.token {
			i.at = time.Time{}
			i.samples = nil
		}
	}
	if i.at.IsZero() || i.clock().Sub(i.at) >= time.Second {
		files, token, e := i.source.List(ctx)
		if e != nil || token == "" {
			i.at = time.Time{}
			i.samples = nil
			return nil, errors.New("GNSS source unavailable")
		}
		sort.Slice(files, func(a, b int) bool {
			if !files[a].Modified.Equal(files[b].Modified) {
				return files[a].Modified.After(files[b].Modified)
			}
			return files[a].Path > files[b].Path
		})
		samples := []Sample{}
		opened := 0
		for _, file := range files {
			if opened == 4 {
				break
			}
			if e = ctx.Err(); e != nil {
				return nil, e
			}
			if file.Size < 0 || file.Size > MaxSidecarBytes {
				continue
			}
			stream, e := i.source.Open(file.Path)
			if e != nil {
				continue
			}
			opened++
			report, e := ParseNMEA(stream, "stock-recording-nmea-live")
			stream.Close()
			if e == nil {
				samples = append(samples, report.Samples...)
			}
		}
		i.samples = samples
		i.at = i.clock()
		i.token = token
	}
	var latest *Sample
	for _, sample := range i.samples {
		if !Usable(sample) || sample.At.After(at.Add(time.Second)) || at.Sub(sample.At) > maxAge {
			continue
		}
		if latest == nil || sample.At.After(latest.At) {
			copy := sample
			latest = &copy
		}
	}
	return latest, nil
}
