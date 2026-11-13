package trips

import (
	"encoding/binary"
	"errors"
	"math"
	"time"
)

// The stock image recognition service rewrites a 288-byte metadata record every second
// (/tmp/application/imagerecog/metaData.bin). It is the only real-time position the stock
// software exposes outside its GNSS port: the recorder's NMEA sidecars appear only when a
// clip closes, about every three minutes. Layout established on the device (2026-11-14):
//
//	0..11   uint16 year, month, day, hour, minute, second (Japan time)
//	72      float64 latitude in degrees
//	80      float64 longitude in degrees
//
// The record has no field that was confirmed to mean "fix valid", so the position is the
// stock software's latest known position. Callers decide whether it is live by pairing it
// with a recent NMEA fix or with movement, and never label it live otherwise.
const ImageRecogMetaSize = 288

var japan = time.FixedZone("JST", 9*60*60)

type MetaPosition struct {
	At       time.Time
	Lat, Lon float64
}

func ParseImageRecogMeta(b []byte) (MetaPosition, error) {
	if len(b) != ImageRecogMetaSize {
		return MetaPosition{}, errors.New("unexpected metadata size")
	}
	u := func(i int) int { return int(binary.LittleEndian.Uint16(b[i*2:])) }
	year, month, day, hour, minute, second := u(0), u(1), u(2), u(3), u(4), u(5)
	if year < 2020 || year > 2100 || month < 1 || month > 12 || day < 1 || day > 31 || hour > 23 || minute > 59 || second > 60 {
		return MetaPosition{}, errors.New("metadata has no valid time")
	}
	at := time.Date(year, time.Month(month), day, hour, minute, second, 0, japan)
	lat := math.Float64frombits(binary.LittleEndian.Uint64(b[72:]))
	lon := math.Float64frombits(binary.LittleEndian.Uint64(b[80:]))
	if math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) || lat < -90 || lat > 90 || lon < -180 || lon > 180 || (lat == 0 && lon == 0) {
		return MetaPosition{}, errors.New("metadata has no position")
	}
	return MetaPosition{At: at.UTC(), Lat: lat, Lon: lon}, nil
}
