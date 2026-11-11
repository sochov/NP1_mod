package trips

import (
	"sort"
	"time"
)

// History reports retained measured coverage. Date filters select complete JST
// day pieces, so midnight clipping never estimates a day's share from a whole
// trip average. A clock rollback hides future-ending pieces until time catches
// up and marks that response partial; it never rewrites retained measurements.
func (a *Archive) History(options HistoryOptions, now time.Time) (HistoryReport, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e := a.load(); e != nil {
		return HistoryReport{}, e
	}
	data := a.data
	var earliest, latest time.Time
	for _, day := range data.Days {
		for _, run := range day.Runs {
			if run.End.After(now) {
				continue
			}
			if earliest.IsZero() || run.Start.Before(earliest) {
				earliest = run.Start
			}
			if latest.IsZero() || run.End.After(latest) {
				latest = run.End
			}
		}
	}
	query := options
	if query.Period == "all" && query.From == "" && query.To == "" && !earliest.IsZero() {
		query = HistoryOptions{Period: "custom", From: earliest.In(Japan).Format("2006-01-02"), To: latest.In(Japan).Format("2006-01-02")}
	}
	out, e := History(Snapshot{}, query, now)
	if e != nil {
		return out, e
	}
	if options.Period == "all" {
		out.Range.Period = "all"
	}
	out.Source = "ncc-retained-measured-intervals"
	out.SourceFiles = data.Health.Files
	out.UnreadableFiles = data.Health.UnreadableFiles
	out.NoFixSentences = data.Health.NoFix
	out.BadChecksums = data.Health.BadChecksum
	out.Malformed = data.Health.Malformed
	out.IndexedAt = data.Health.IndexedAt
	out.SourceTruncated = data.SourcePartial || data.LimitReached || data.OverlapRejected > 0 || data.Backfilling
	if data.CollectedAt.IsZero() {
		out.Reason = "走行記録の取得を開始しています"
	}
	if !earliest.IsZero() {
		out.Range.SourceFrom = earliest.In(Japan).Format("2006-01-02")
		out.Range.SourceTo = latest.In(Japan).Format("2006-01-02")
	}
	trips := map[string]*Trip{}
	for n := range out.Days {
		day := &out.Days[n]
		archived := findDay(&data, day.Date)
		if archived == nil {
			continue
		}
		midnight, _ := parseHistoryDate(day.Date)
		pointIncluded := false
		for _, run := range archived.Runs {
			if run.End.After(now) {
				out.SourceTruncated = true
				continue
			}
			if !day.Observed {
				day.Observed = true
				day.DistanceKM = ptr(0)
				day.DrivingSeconds = ptr(0)
				day.Reason = ""
			}
			*day.DistanceKM += run.DistanceKM
			*day.DrivingSeconds += run.DrivingSeconds
			day.ObservedSeconds += run.End.Sub(run.Start).Seconds()
			day.SampleCount += run.Samples
			if run.Start.Equal(midnight) && run.FirstReal {
				pointIncluded = true
			}
			trip := trips[run.ID]
			if trip == nil {
				trip = &Trip{ID: run.ID, Start: run.Start}
				trips[run.ID] = trip
			}
			trip.End = run.End
			trip.DistanceKM += run.DistanceKM
			trip.DrivingSeconds += run.DrivingSeconds
			trip.ObservedSeconds += run.End.Sub(run.Start).Seconds()
			trip.SampleCount += run.Samples
		}
		if archived.MidnightPointID != "" && !pointIncluded && !midnight.After(now) {
			day.SampleCount++
			if !day.Observed {
				day.Reason = "no-continuous-position"
			}
			if trip := trips[archived.MidnightPointID]; trip != nil {
				trip.SampleCount++
			}
		}
	}
	distance, driving := 0.0, 0.0
	for _, day := range out.Days {
		out.Period.SampleCount += day.SampleCount
		if day.Observed {
			out.Period.HasData = true
			out.Period.ValidObservedDays++
			distance += *day.DistanceKM
			driving += *day.DrivingSeconds
		}
	}
	if out.Period.HasData {
		out.Period.DistanceKM = &distance
		out.Period.DrivingSeconds = &driving
		out.Status = "available"
		out.Reason = ""
	}
	out.Period.Days = out.Days
	out.Trips = []Trip{}
	for _, trip := range trips {
		out.Trips = append(out.Trips, *trip)
	}
	sort.Slice(out.Trips, func(i, j int) bool { return out.Trips[i].Start.After(out.Trips[j].Start) })
	out.TotalTrips = len(out.Trips)
	if len(out.Trips) > MaximumHistoryTrips {
		out.Trips = out.Trips[:MaximumHistoryTrips]
		out.DetailsTruncated = true
	}
	return out, nil
}
