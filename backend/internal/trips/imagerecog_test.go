package trips

import (
	"encoding/binary"
	"math"
	"testing"
	"time"
)

func metaRecord(lat, lon float64) []byte {
	b := make([]byte, ImageRecogMetaSize)
	for i, v := range []uint16{2026, 11, 14, 2, 31, 9} {
		binary.LittleEndian.PutUint16(b[i*2:], v)
	}
	binary.LittleEndian.PutUint64(b[72:], math.Float64bits(lat))
	binary.LittleEndian.PutUint64(b[80:], math.Float64bits(lon))
	return b
}

func TestParseImageRecogMeta(t *testing.T) {
	p, e := ParseImageRecogMeta(metaRecord(35.681236, 139.767125))
	if e != nil {
		t.Fatal(e)
	}
	if want := time.Date(2026, 11, 13, 17, 31, 9, 0, time.UTC); !p.At.Equal(want) {
		t.Fatalf("time %v, want %v (record time is Japan time)", p.At, want)
	}
	if p.Lat != 35.681236 || p.Lon != 139.767125 {
		t.Fatalf("position %v,%v", p.Lat, p.Lon)
	}
}

func TestParseImageRecogMetaRejectsInvalidRecords(t *testing.T) {
	for name, b := range map[string][]byte{
		"short":    make([]byte, 100),
		"no time":  make([]byte, ImageRecogMetaSize),
		"zero pos": metaRecord(0, 0),
		"nan":      metaRecord(math.NaN(), 139),
		"range":    metaRecord(95, 139),
	} {
		if _, e := ParseImageRecogMeta(b); e == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
