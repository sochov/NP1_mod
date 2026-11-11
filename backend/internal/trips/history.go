package trips

import (
	"errors"
	"sort"
	"strconv"
	"time"
)

const MaximumHistoryDays = 36600
const MaximumHistoryTrips = 1000

type HistoryOptions struct {
	Period string
	From   string
	To     string
}
type HistoryRange struct {
	Period     string `json:"period"`
	From       string `json:"from"`
	To         string `json:"to"`
	SourceFrom string `json:"sourceFrom,omitempty"`
	SourceTo   string `json:"sourceTo,omitempty"`
}
type HistoryReport struct {
	Range            HistoryRange `json:"range"`
	Days             []Day        `json:"days"`
	Period           Period       `json:"period"`
	Trips            []Trip       `json:"trips"`
	TotalTrips       int          `json:"totalTrips"`
	DetailsTruncated bool         `json:"detailsTruncated"`
	SourceTruncated  bool         `json:"sourceTruncated"`
	SourceFiles      int          `json:"sourceFiles"`
	UnreadableFiles  int          `json:"unreadableFiles"`
	NoFixSentences   int          `json:"noFixSentences"`
	BadChecksums     int          `json:"badChecksums"`
	Malformed        int          `json:"malformed"`
	IndexedAt        time.Time    `json:"indexedAt"`
	Source           string       `json:"source"`
	Status           string       `json:"status"`
	Reason           string       `json:"reason,omitempty"`
}

func parseHistoryDate(value string) (time.Time, error) {
	if len(value) != 10 {
		return time.Time{}, errors.New("date must be YYYY-MM-DD")
	}
	date, e := time.ParseInLocation("2006-01-02", value, Japan)
	if e != nil || date.Format("2006-01-02") != value {
		return time.Time{}, errors.New("invalid calendar date")
	}
	return date, nil
}

// History uses only measured intervals from the sidecar source. Calendar days
// without an interval remain null, including isolated fixes and no-fix records.
// Custom ranges are JST days; crossing intervals are proportionally clipped,
// never converted from recording duration or interpolated across missing fixes.
func History(snapshot Snapshot, options HistoryOptions, now time.Time) (HistoryReport, error) {
	ordered := ordered(snapshot.Samples)
	samples := make([]Sample, 0, len(ordered))
	var sourceFrom, sourceTo time.Time
	for _, sample := range ordered {
		if sample.At.After(now) {
			continue
		}
		samples = append(samples, sample)
		if Usable(sample) {
			if sourceFrom.IsZero() {
				sourceFrom = sample.At
			}
			sourceTo = sample.At
		}
	}
	today, _ := parseHistoryDate(now.In(Japan).Format("2006-01-02"))
	period := options.Period
	if period == "" {
		period = "7"
	}
	from, to := today, today
	switch period {
	case "7", "30", "90", "365":
		if options.From != "" || options.To != "" {
			return HistoryReport{}, errors.New("from/to require custom period")
		}
		count, _ := strconv.Atoi(period)
		from = today.AddDate(0, 0, 1-count)
	case "all":
		if options.From != "" || options.To != "" {
			return HistoryReport{}, errors.New("all period does not accept from/to")
		}
		if !sourceFrom.IsZero() {
			from, _ = parseHistoryDate(sourceFrom.In(Japan).Format("2006-01-02"))
			to, _ = parseHistoryDate(sourceTo.In(Japan).Format("2006-01-02"))
		}
	case "custom":
		var e error
		from, e = parseHistoryDate(options.From)
		if e != nil {
			return HistoryReport{}, e
		}
		to, e = parseHistoryDate(options.To)
		if e != nil {
			return HistoryReport{}, e
		}
		if to.After(today) {
			return HistoryReport{}, errors.New("range must not include future dates")
		}
	default:
		return HistoryReport{}, errors.New("unsupported history period")
	}
	if from.After(to) || to.Sub(from)/(24*time.Hour) >= MaximumHistoryDays {
		return HistoryReport{}, errors.New("invalid or excessive history date range")
	}
	end := to.AddDate(0, 0, 1)
	if now.Before(end) {
		end = now
	}
	out := HistoryReport{
		Range: HistoryRange{Period: period, From: from.Format("2006-01-02"), To: to.Format("2006-01-02")},
		Days:  []Day{}, Trips: []Trip{}, Source: "stock-recording-nmea", Status: "unknown",
		SourceFiles: snapshot.Files, UnreadableFiles: snapshot.UnreadableFiles,
		NoFixSentences: snapshot.NoFix, BadChecksums: snapshot.BadChecksum,
		Malformed: snapshot.Malformed, IndexedAt: snapshot.IndexedAt,
		SourceTruncated: snapshot.Truncated, Reason: "この期間に有効な連続測位データがありません",
	}
	if !sourceFrom.IsZero() {
		out.Range.SourceFrom = sourceFrom.In(Japan).Format("2006-01-02")
		out.Range.SourceTo = sourceTo.In(Japan).Format("2006-01-02")
	}
	dayIndexes := map[string]int{}
	for date := from; !date.After(to); date = date.AddDate(0, 0, 1) {
		key := date.Format("2006-01-02")
		dayIndexes[key] = len(out.Days)
		out.Days = append(out.Days, Day{Date: key, Reason: "no-position"})
	}
	for _, sample := range samples {
		if Usable(sample) && !sample.At.Before(from) && !sample.At.After(end) {
			if i, ok := dayIndexes[sample.At.In(Japan).Format("2006-01-02")]; ok {
				out.Days[i].SampleCount++
				out.Days[i].Reason = "no-continuous-position"
			}
		}
	}
	for _, trip := range BuildTrips(samples) {
		clipped := Trip{ID: trip.ID}
		lastSample := -1
		for i := 1; i < len(trip.Samples); i++ {
			a, b := trip.Samples[i-1], trip.Samples[i]
			start, finish := a.At, b.At
			if start.Before(from) {
				start = from
			}
			if finish.After(end) {
				finish = end
			}
			if !start.Before(finish) {
				continue
			}
			distance, driving, seconds, ok := interval(a, b)
			if !ok {
				continue
			}
			if clipped.Start.IsZero() {
				clipped.Start = start
			}
			clipped.End = finish
			fraction := finish.Sub(start).Seconds() / seconds
			clipped.DistanceKM += distance * fraction
			clipped.DrivingSeconds += driving * fraction
			clipped.ObservedSeconds += finish.Sub(start).Seconds()
			if lastSample != i-1 {
				clipped.SampleCount++
			}
			clipped.SampleCount++
			lastSample = i
			for start.Before(finish) {
				date := start.In(Japan)
				boundary := time.Date(date.Year(), date.Month(), date.Day()+1, 0, 0, 0, 0, Japan)
				next := finish
				if boundary.Before(next) {
					next = boundary
				}
				part := next.Sub(start).Seconds() / seconds
				day := &out.Days[dayIndexes[date.Format("2006-01-02")]]
				if !day.Observed {
					day.Observed, day.DistanceKM, day.DrivingSeconds = true, ptr(0), ptr(0)
					day.Reason = ""
				}
				*day.DistanceKM += distance * part
				*day.DrivingSeconds += driving * part
				day.ObservedSeconds += next.Sub(start).Seconds()
				start = next
			}
		}
		if clipped.ObservedSeconds > 0 {
			out.Trips = append(out.Trips, clipped)
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
		out.Period.DistanceKM, out.Period.DrivingSeconds = &distance, &driving
		out.Status, out.Reason = "available", ""
	}
	out.Period.Days = out.Days
	out.TotalTrips = len(out.Trips)
	sort.Slice(out.Trips, func(i, j int) bool { return out.Trips[i].Start.After(out.Trips[j].Start) })
	if len(out.Trips) > MaximumHistoryTrips {
		out.Trips = out.Trips[:MaximumHistoryTrips]
		out.DetailsTruncated = true
	}
	return out, nil
}
