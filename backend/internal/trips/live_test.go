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

type liveFixture struct {
	token   string
	raw     string
	opens   int
	files   int
	missing bool
}

func (s *liveFixture) Identity() (string, error) {
	if s.missing {
		return "", errors.New("missing")
	}
	return s.token, nil
}
func (s *liveFixture) List(context.Context) ([]SourceFile, string, error) {
	files := []SourceFile{}
	for n := 0; n < s.files; n++ {
		files = append(files, SourceFile{Path: fmt.Sprint(n), Size: int64(len(s.raw)), Modified: time.Unix(int64(n), 0)})
	}
	return files, s.token, nil
}
func (s *liveFixture) Open(string) (io.ReadCloser, error) {
	s.opens++
	return io.NopCloser(strings.NewReader(s.raw)), nil
}
func liveSentence(clock string) string {
	return Sentence("GPRMC," + clock + ",A,3500.0000,N,13900.0000,E,10.0,90.0,111126,,,A")
}

func TestLiveGNSSRefreshesWithoutHistoryCacheAndRejectsStaleFuture(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	source := &liveFixture{token: "card-a", raw: liveSentence("120000.00"), files: 1}
	live := NewLiveIndex(source)
	live.clock = func() time.Time { return now }
	fix, e := live.Latest(ctx, now)
	if e != nil || fix == nil || !fix.At.Equal(now) || source.opens != 1 {
		t.Fatal("initial fix", fix, e)
	}
	now = now.Add(2 * time.Second)
	source.raw = liveSentence("120002.00")
	fix, e = live.Latest(ctx, now)
	if e != nil || fix == nil || !fix.At.Equal(now) || source.opens != 2 {
		t.Fatal("live cache did not refresh", fix, e)
	}
	if fix, _ = live.Latest(ctx, now.Add(6*time.Second)); fix != nil {
		t.Fatal("stale position presented as current")
	}
	if fix, _ = live.Latest(ctx, now.Add(-2*time.Second)); fix != nil {
		t.Fatal("future position accepted")
	}
	source.missing = true
	if fix, e = live.Latest(ctx, now); fix != nil || e == nil {
		t.Fatal("removed SD kept cached current position")
	}
}

func TestLiveGNSSBoundsReadAndInvalidatesCardReplacement(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	source := &liveFixture{token: "card-a", raw: liveSentence("120000.00"), files: 20}
	live := NewLiveIndex(source)
	live.clock = func() time.Time { return now }
	if _, e := live.Latest(context.Background(), now); e != nil || source.opens != 4 {
		t.Fatal("live reader must open at most four sidecars", e, source.opens)
	}
	source.token = "card-b"
	source.raw = Sentence("GPRMC,,V,,,,,,,,,,N")
	if fix, e := live.Latest(context.Background(), now); e != nil || fix != nil || source.opens != 8 {
		t.Fatal("replacement inherited previous fix", fix, e, source.opens)
	}
}
