package adapters

import (
	"context"
	"encoding/json"
	"github.com/np1-control-center/ncc/internal/model"
	"github.com/np1-control-center/ncc/internal/radio"
	"github.com/np1-control-center/ncc/internal/trips"
	"time"
)

func RadioSample(ctx context.Context, modem model.Observation) model.Observation {
	if modem.Status != "available" || modem.ObservedAt.IsZero() {
		return model.Missing("radio observer", "有効な電波測定を待っています")
	}
	raw, e := json.Marshal(modem.Data)
	if e != nil {
		return model.Missing("radio observer", "電波測定を取得できません")
	}
	var sample radio.Observation
	if json.Unmarshal(raw, &sample) != nil {
		return model.Missing("radio observer", "電波測定を取得できません")
	}
	sample.At = modem.ObservedAt
	sample.Source = modem.Source
	if gps := recentQuality(sample.At); gps != nil && gps.At.Sub(sample.At).Abs() <= 5*time.Second {
		sample.GPS = &radio.Position{At: gps.At, Lat: gps.Lat, Lon: gps.Lon, AccuracyM: gps.AccuracyM, HDOP: gps.HDOP, Source: gps.Source}
	} else {
		// NMEA sidecars arrive when a recording closes, about three minutes late.
		// Use the current per-second stock position while waiting for that file.
		fix := recentQuality(sample.At)
		meta, moving := liveMeta.near(sample.At, livePositionTolerance)
		sample.GPS = radioLivePosition(combineGNSS(sample.At, meta, moving, fix, nil), sample.At)
	}
	return model.Value("measured radio and GNSS sample", sample)
}

func radioLivePosition(value model.Observation, at time.Time) *radio.Position {
	if value.Status != "available" {
		return nil
	}
	var data struct {
		Fix  bool      `json:"fix"`
		Live bool      `json:"live"`
		At   time.Time `json:"at"`
		Lat  float64   `json:"lat"`
		Lon  float64   `json:"lon"`
	}
	raw, err := json.Marshal(value.Data)
	if err != nil || json.Unmarshal(raw, &data) != nil || !data.Fix || !data.Live || data.At.IsZero() || data.At.Sub(at).Abs() > 5*time.Second {
		return nil
	}
	// Accuracy belongs to the older NMEA fix, not this live coordinate. The
	// deferred NMEA match will add the quality measured at the actual time.
	return &radio.Position{At: data.At, Lat: data.Lat, Lon: data.Lon, Source: "stock-image-recognition"}
}

// RadioHistoryPositions is read-only and uses the same valid fixes as the map.
func RadioHistoryPositions(ctx context.Context) ([]radio.Position, bool, error) {
	snapshot, err := tripIndex.Read(ctx)
	if err != nil {
		return nil, false, err
	}
	positions := []radio.Position{}
	now := time.Now()
	for _, sample := range snapshot.Samples {
		if trips.Usable(sample) && !sample.At.After(now) {
			positions = append(positions, radio.Position{At: sample.At, Lat: sample.Lat, Lon: sample.Lon, AccuracyM: sample.AccuracyM, HDOP: sample.HDOP, Source: sample.Source})
		}
	}
	return positions, snapshot.Truncated, nil
}
