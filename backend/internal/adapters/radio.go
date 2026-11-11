package adapters

import (
	"context"
	"encoding/json"
	"github.com/np1-control-center/ncc/internal/model"
	"github.com/np1-control-center/ncc/internal/radio"
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
	gps, _ := LatestGNSS(ctx, sample.At)
	if gps != nil {
		sample.GPS = &radio.Position{At: gps.At, Lat: gps.Lat, Lon: gps.Lon, AccuracyM: gps.AccuracyM, HDOP: gps.HDOP, Source: gps.Source}
	}
	return model.Value("measured radio and GNSS sample", sample)
}
