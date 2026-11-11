package radio

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryReadHasNoSideEffects(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "uncreated")
	h := NewHistory(dir)
	r, e := h.Query(Query{})
	if e != nil || r.Total != 0 {
		t.Fatal(r, e)
	}
	if _, e = os.Stat(dir); !os.IsNotExist(e) {
		t.Fatal("query created storage")
	}
}
func TestHistoryPositionAgeAndUnknownAccuracy(t *testing.T) {
	h := NewHistory(t.TempDir())
	at := time.Now().UTC()
	v := -85.0
	for _, offset := range []time.Duration{0, 10 * time.Second} {
		e := h.Append(Observation{At: at, RSRP: &v, Source: "synthetic", GPS: &Position{At: at.Add(-offset), Lat: 35, Lon: 139, Source: "synthetic"}})
		if e != nil {
			t.Fatal(e)
		}
	}
	r, e := h.Query(Query{})
	if e != nil || r.Total != 2 || r.Unplaced != 1 || r.AccuracyExcluded != 1 || len(r.Points) != 0 {
		t.Fatal(r, e)
	}
	r, e = h.Query(Query{IncludeUnknownAccuracy: true})
	if e != nil || len(r.Points) != 1 || !r.Points[0].AccuracyUnknown || len(r.Cells) != 0 {
		t.Fatal(r, e)
	}
}
func TestHistoryGridAndState(t *testing.T) {
	h := NewHistory(t.TempDir())
	at := time.Now().UTC()
	v, a := -85.0, 5.0
	no := false
	for n := 0; n < 2; n++ {
		o := Observation{At: at.Add(time.Duration(n) * time.Second), RSRP: &v, Source: "synthetic", GPS: &Position{At: at.Add(time.Duration(n) * time.Second), Lat: 35, Lon: 139, AccuracyM: &a}}
		if n == 1 {
			o.Registered = &no
		}
		if e := h.Append(o); e != nil {
			t.Fatal(e)
		}
	}
	r, e := h.Query(Query{})
	if e != nil || len(r.Points) != 2 || len(r.Cells) != 1 || r.Cells[0].Count != 1 || r.OutOfService != 1 {
		t.Fatal(r, e)
	}
}
