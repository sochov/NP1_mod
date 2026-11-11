package trips

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

type fixtureSource struct {
	token   string
	opens   int
	missing bool
}

type budgetSource struct {
	files []SourceFile
	opens int
}

func (s *budgetSource) List(context.Context) ([]SourceFile, string, error) {
	return s.files, "synthetic-budget-card", nil
}
func (s *budgetSource) Open(string) (io.ReadCloser, error) {
	s.opens++
	return io.NopCloser(strings.NewReader(Sentence("GPRMC,,V,,,,,,,,,,N"))), nil
}
func TestIndexBoundsSamplesAndCachedBackingArrays(t *testing.T) {
	source := &budgetSource{}
	index := NewIndex(source)
	index.token = "synthetic-budget-card"
	for file := 0; file < 25; file++ {
		metadata := SourceFile{Path: fmt.Sprintf("DRIVERECDATA/Video/FILEM_261111_120000_%04d.NMEA", file), Size: 100, Modified: time.Unix(0, 0)}
		source.files = append(source.files, metadata)
		samples := make([]Sample, 5010)
		for i := range samples {
			samples[i] = sample(time.Unix(int64(file*5010+i+1), 0), 35, 139, 0)
		}
		index.cache[metadata.Path] = cacheEntry{file: metadata, report: ParseReport{Samples: samples}}
	}
	snapshot, e := index.Read(context.Background())
	if e != nil || !snapshot.Truncated || len(snapshot.Samples) != MaximumIndexedSamples || source.opens != 0 {
		t.Fatal(len(snapshot.Samples), snapshot.Truncated, source.opens, e)
	}
	total := 0
	var clipped SourceFile
	for _, entry := range index.cache {
		total += len(entry.report.Samples)
		if entry.truncated {
			clipped = entry.file
		}
	}
	if total > MaximumIndexedSamples || clipped.Path == "" {
		t.Fatal("cached source exceeded sample budget", total, clipped)
	}
	// A clipped cache entry must be reparsed if it is the only remaining file.
	source.files = []SourceFile{clipped}
	index.at = time.Time{}
	snapshot, e = index.Read(context.Background())
	if e != nil || snapshot.Truncated || source.opens != 1 || len(snapshot.Samples) != 0 || snapshot.NoFix != 1 {
		t.Fatal(snapshot, source.opens, e)
	}
}

func (s *fixtureSource) Identity() (string, error) {
	if s.missing {
		return "", errors.New("card absent")
	}
	return s.token, nil
}
func (s *fixtureSource) List(context.Context) ([]SourceFile, string, error) {
	token, e := s.Identity()
	return []SourceFile{{Path: "DRIVERECDATA/Video/FILEM_261111_120000_0000.NMEA", Size: 100, Modified: time.Unix(0, 0)}}, token, e
}
func (s *fixtureSource) Open(string) (io.ReadCloser, error) {
	s.opens++
	return io.NopCloser(strings.NewReader(Sentence("GPRMC,,V,,,,,,,,,,N"))), nil
}
func TestIndexCacheMountRemovalAndReplacement(t *testing.T) {
	source := &fixtureSource{token: "synthetic-card-a"}
	index := NewIndex(source)
	ctx := context.Background()
	snapshot, e := index.Read(ctx)
	if e != nil || snapshot.Files != 1 || snapshot.NoFix != 1 {
		t.Fatal(snapshot, e)
	}
	index.Read(ctx)
	if source.opens != 1 {
		t.Fatal("cache missed", source.opens)
	}
	source.missing = true
	if _, e = index.Read(ctx); e == nil {
		t.Fatal("removed card reused cache")
	}
	source.missing = false
	source.token = "synthetic-card-b"
	if _, e = index.Read(ctx); e != nil || source.opens != 2 {
		t.Fatal("replacement did not invalidate", source.opens, e)
	}
}
