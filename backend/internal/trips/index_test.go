package trips

import (
	"context"
	"errors"
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
