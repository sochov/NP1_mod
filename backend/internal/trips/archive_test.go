package trips

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testInterval(start time.Time, seconds, distance, driving float64) MeasuredInterval {
	return MeasuredInterval{Start: start, End: start.Add(time.Duration(seconds * float64(time.Second))), DistanceKM: distance, DrivingSeconds: driving}
}
func intervalBatch(values ...MeasuredInterval) MeasuredIntervalBatch {
	next := time.Time{}
	if len(values) > 0 {
		next = values[len(values)-1].End
	}
	return MeasuredIntervalBatch{Intervals: values, Next: next, Health: SourceHealth{Files: 2}}
}
func archiveReport(t *testing.T, a *Archive, now time.Time) HistoryReport {
	t.Helper()
	r, e := a.History(HistoryOptions{Period: "all"}, now)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func nearly(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-8 {
		t.Fatalf("got %.12f want %.12f", got, want)
	}
}

func TestArchiveRetainsAcrossRestartFIFOAndOldCardReplay(t *testing.T) {
	directory := t.TempDir()
	a := NewArchive(directory)
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, Japan)
	start := now.AddDate(0, 0, -370)
	values := []MeasuredInterval{testInterval(start, 1, .01, 1), testInterval(start.Add(time.Second), 1, .02, 1)}
	if result, e := a.Ingest(intervalBatch(values...), now); e != nil || result.Added != 2 {
		t.Fatal(result, e)
	}
	before := archiveReport(t, a, now)
	nearly(t, *before.Period.DistanceKM, .03)
	if before.TotalTrips != 1 || before.Trips[0].SampleCount != 3 || a.State().Runs != 1 {
		t.Fatal("continuous points were not compacted", before.Trips, a.State())
	}
	a = NewArchive(directory)
	if result, e := a.Ingest(intervalBatch(values...), now); e != nil || result.Duplicates != 2 || result.Added != 0 {
		t.Fatal("replayed SD doubled totals", result, e)
	}
	// FIFO deletion/card removal provides no interval. It cannot erase retained trips.
	if _, e := a.Ingest(intervalBatch(), now); e != nil {
		t.Fatal(e)
	}
	after := archiveReport(t, a, now)
	nearly(t, *after.Period.DistanceKM, .03)
	if after.Range.SourceFrom != start.Format("2006-01-02") || after.TotalTrips != 1 {
		t.Fatal("retained range lost", after.Range)
	}
	lastYear, e := a.History(HistoryOptions{Period: "365"}, now)
	if e != nil || lastYear.Period.HasData || lastYear.Period.DistanceKM != nil {
		t.Fatal("missing days invented zero", lastYear, e)
	}
	// A different card containing older disjoint data is accepted, while its
	// replay of the first card's coverage remains idempotent.
	late := testInterval(start.Add(-10*time.Second), 1, .015, 1)
	if r, e := a.Ingest(intervalBatch(late), now); e != nil || r.Added != 1 {
		t.Fatal("late disjoint interval lost", r, e)
	}
	after = archiveReport(t, NewArchive(directory), now)
	nearly(t, *after.Period.DistanceKM, .045)
	if after.TotalTrips != 2 {
		t.Fatal("gap was joined", after.Trips)
	}
}

func TestArchiveLateGapFillMergesCoverageWithoutDoubleCounting(t *testing.T) {
	a := NewArchive(t.TempDir())
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, Japan)
	start := now.Add(-time.Hour)
	one := testInterval(start, 1, .01, 1)
	three := testInterval(start.Add(2*time.Second), 1, .03, 1)
	if _, e := a.Ingest(intervalBatch(one, three), now); e != nil {
		t.Fatal(e)
	}
	if a.State().Runs != 2 {
		t.Fatal("unmeasured gap merged")
	}
	two := testInterval(start.Add(time.Second), 1, .02, 1)
	if _, e := a.Ingest(intervalBatch(two), now); e != nil {
		t.Fatal(e)
	}
	report := archiveReport(t, a, now)
	nearly(t, *report.Period.DistanceKM, .06)
	if report.TotalTrips != 1 || report.Trips[0].SampleCount != 4 || a.State().Runs != 1 {
		t.Fatal("late bridge did not merge journeys", report.Trips, a.State())
	}
	// A different sampling rate partly crossing already merged coverage is
	// rejected as partial rather than prorating an invented uncovered distance.
	overlap := testInterval(start.Add(2500*time.Millisecond), 1, .05, 1)
	result, e := a.Ingest(intervalBatch(overlap), now)
	if e != nil || result.Rejected != 1 || result.Added != 0 {
		t.Fatal(result, e)
	}
	report = archiveReport(t, a, now)
	nearly(t, *report.Period.DistanceKM, .06)
	if !report.SourceTruncated || !a.State().Partial {
		t.Fatal("partial overlap hidden")
	}
}

func TestArchiveMidnightUsesMeasuredFractionsAndRealEndpointCounts(t *testing.T) {
	dir := t.TempDir()
	a := NewArchive(dir)
	midnight := time.Date(2026, 11, 11, 0, 0, 0, 0, Japan)
	now := midnight.Add(time.Hour)
	v := testInterval(midnight.Add(-time.Second), 2, .02, 2)
	if _, e := a.Ingest(intervalBatch(v), now); e != nil {
		t.Fatal(e)
	}
	report := archiveReport(t, a, now)
	if len(report.Days) != 2 || report.TotalTrips != 1 {
		t.Fatal(report)
	}
	for _, day := range report.Days {
		nearly(t, *day.DistanceKM, .01)
		nearly(t, *day.DrivingSeconds, 1)
		if day.SampleCount != 1 {
			t.Fatal("synthetic midnight counted as a GNSS sample", day)
		}
	}
	if report.Trips[0].SampleCount != 2 {
		t.Fatal(report.Trips)
	}
	day, e := a.History(HistoryOptions{Period: "custom", From: "2026-11-11", To: "2026-11-11"}, now)
	if e != nil {
		t.Fatal(e)
	}
	nearly(t, *day.Period.DistanceKM, .01)
	if day.Trips[0].Start != midnight.UTC() || day.Trips[0].ObservedSeconds != 1 {
		t.Fatal("date clipping used entire-trip averages", day.Trips)
	}
	// An observed endpoint exactly at midnight is retained on the next day
	// without marking that next day as a continuously observed zero.
	b := NewArchive(t.TempDir())
	if _, e := b.Ingest(intervalBatch(testInterval(midnight.Add(-time.Second), 1, .01, 1)), now); e != nil {
		t.Fatal(e)
	}
	r := archiveReport(t, b, now)
	if len(r.Days) != 2 || r.Days[1].Observed || r.Days[1].DistanceKM != nil || r.Days[1].SampleCount != 1 || r.Trips[0].SampleCount != 2 {
		t.Fatal("midnight endpoint semantics", r)
	}
	if _, e := b.Ingest(intervalBatch(testInterval(midnight, 1, 0, 0)), now); e != nil {
		t.Fatal(e)
	}
	r = archiveReport(t, b, now)
	if r.Trips[0].SampleCount != 3 || r.Days[1].SampleCount != 2 || r.TotalTrips != 1 {
		t.Fatal("midnight endpoint double counted", r)
	}
}

func TestArchiveInvalidBatchCannotAdvanceOrCreateFile(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, Japan)
	for name, value := range map[string]MeasuredInterval{
		"future": testInterval(now, 1, 0, 0), "gap": testInterval(now.Add(-10*time.Second), 6, 0, 0),
		"distance": testInterval(now.Add(-time.Second), 1, .1, 1), "driving": testInterval(now.Add(-time.Second), 1, .01, 2),
		"nan": testInterval(now.Add(-time.Second), 1, math.NaN(), 1), "negative": testInterval(now.Add(-time.Second), 1, -1, 1),
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			a := NewArchive(dir)
			if _, e := a.Ingest(intervalBatch(value), now); e == nil {
				t.Fatal("invalid interval accepted")
			}
			if !a.State().Watermark.IsZero() {
				t.Fatal("invalid interval advanced cursor")
			}
			if _, e := os.Stat(filepath.Join(dir, "journeys", "archive.json")); !os.IsNotExist(e) {
				t.Fatal("invalid batch wrote state")
			}
		})
	}
}

func TestArchiveBoundDoesNotDiscardOldTotalsAndStationaryIsMeasuredZero(t *testing.T) {
	a := NewArchive(t.TempDir())
	a.maximumRuns = 1
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, Japan)
	start := now.Add(-time.Hour)
	first := testInterval(start, 1, 0, 0)
	if _, e := a.Ingest(intervalBatch(first), now); e != nil {
		t.Fatal(e)
	}
	result, e := a.Ingest(intervalBatch(testInterval(start.Add(10*time.Second), 1, .01, 1)), now)
	if e != nil || !result.Limited || result.Rejected != 1 {
		t.Fatal(result, e)
	}
	report := archiveReport(t, a, now)
	if !report.Period.HasData || report.Period.DistanceKM == nil || *report.Period.DistanceKM != 0 || !report.SourceTruncated {
		t.Fatal("measured zero or cap hidden", report)
	}
	if a.State().Runs != 1 {
		t.Fatal("archive cap exceeded")
	}
	// Extending an existing run still fits the bound.
	if r, e := a.Ingest(intervalBatch(testInterval(first.End, 1, .01, 1)), now); e != nil || r.Added != 1 {
		t.Fatal(r, e)
	}
	report = archiveReport(t, a, now)
	nearly(t, *report.Period.DistanceKM, .01)
}

func TestArchiveChecksumFailureIsNotResetAndFailedWriteKeepsActiveState(t *testing.T) {
	dir := t.TempDir()
	a := NewArchive(dir)
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, Japan)
	first := testInterval(now.Add(-time.Hour), 1, .01, 1)
	if _, e := a.Ingest(intervalBatch(first), now); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(dir, "journeys", "archive.json")
	raw, e := os.ReadFile(file)
	if e != nil {
		t.Fatal(e)
	}
	corrupt := bytes.Replace(raw, []byte(`"distanceKm":0.01`), []byte(`"distanceKm":0.02`), 1)
	if bytes.Equal(corrupt, raw) {
		t.Fatal("test fixture not corrupted")
	}
	if e = os.WriteFile(file, corrupt, 0600); e != nil {
		t.Fatal(e)
	}
	b := NewArchive(dir)
	if b.State().Available {
		t.Fatal("checksum failure silently reset archive")
	}
	if _, e = b.Ingest(intervalBatch(), now); e == nil {
		t.Fatal("corrupt state overwritten")
	}
	if read, _ := os.ReadFile(file); !bytes.Equal(read, corrupt) {
		t.Fatal("corrupt evidence replaced")
	}
	if e = os.WriteFile(file, raw, 0600); e != nil {
		t.Fatal(e)
	}
	// Keep the loaded active state but make the atomic target unavailable.
	if e = os.Rename(file, file+".saved"); e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(file, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(file, "occupied"), []byte("test"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = a.Ingest(intervalBatch(testInterval(first.End, 1, .02, 1)), now); e == nil {
		t.Fatal("failed replacement reported success")
	}
	report := archiveReport(t, a, now)
	nearly(t, *report.Period.DistanceKM, .01)
}

func TestArchiveDuplicatePollDoesNotRewriteMetricsFileAndRawCoordinatesAreAbsent(t *testing.T) {
	dir := t.TempDir()
	a := NewArchive(dir)
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, Japan)
	batch := intervalBatch(testInterval(now.Add(-time.Hour), 1, .01, 1))
	if _, e := a.Ingest(batch, now); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(dir, "journeys", "archive.json")
	before, _ := os.ReadFile(file)
	batch.Health.IndexedAt = now.Add(time.Minute)
	if _, e := a.Ingest(batch, now.Add(time.Minute)); e != nil {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(file)
	if !bytes.Equal(before, after) {
		t.Fatal("duplicate-only poll rewrote retained metrics")
	}
	for _, key := range []string{`"lat"`, `"lon"`, `"path"`, `"ssid"`, `"sourceId"`} {
		if bytes.Contains(after, []byte(key)) {
			t.Fatal("raw identity/position persisted", key)
		}
	}
	var decoded any
	if json.Unmarshal(after, &decoded) != nil {
		t.Fatal("invalid persisted JSON")
	}
}

func TestArchiveNoFixDiagnosticsAndClockRollbackDoNotInventValues(t *testing.T) {
	a := NewArchive(t.TempDir())
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, Japan)
	batch := intervalBatch()
	batch.Health = SourceHealth{Files: 12, NoFix: 456, Truncated: true, IndexedAt: now}
	if _, e := a.Ingest(batch, now); e != nil {
		t.Fatal(e)
	}
	r := archiveReport(t, a, now)
	if r.SourceFiles != 12 || r.NoFixSentences != 456 || r.Period.HasData || r.Period.DistanceKM != nil || !r.SourceTruncated {
		t.Fatal("no-fix health lost", r)
	}
	if _, e := a.Ingest(intervalBatch(testInterval(now.Add(-time.Second), 1, .01, 1)), now); e != nil {
		t.Fatal(e)
	}
	r = archiveReport(t, a, now.Add(-2*time.Second))
	if r.Period.HasData || r.Period.DistanceKM != nil || !r.SourceTruncated {
		t.Fatal("future-ending run appeared after clock rollback", r)
	}
	r = archiveReport(t, a, now)
	nearly(t, *r.Period.DistanceKM, .01)
}

func TestArchiveDetailLimitKeepsCompleteDayTotals(t *testing.T) {
	a := NewArchive(t.TempDir())
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, Japan)
	values := make([]MeasuredInterval, MaximumHistoryTrips+5)
	for n := range values {
		values[n] = testInterval(now.Add(time.Duration(n*10-20000)*time.Second), 1, .01, 1)
	}
	if _, e := a.Ingest(intervalBatch(values...), now); e != nil {
		t.Fatal(e)
	}
	r := archiveReport(t, a, now)
	if !r.DetailsTruncated || len(r.Trips) != MaximumHistoryTrips || r.TotalTrips != len(values) {
		t.Fatal("detail cap changed source totals", len(r.Trips), r.TotalTrips)
	}
	nearly(t, *r.Period.DistanceKM, float64(len(values))*.01)
}

func TestArchiveCoveredMidnightPartAnchorsLateContinuation(t *testing.T) {
	a := NewArchive(t.TempDir())
	midnight := time.Date(2026, 11, 11, 0, 0, 0, 0, Japan)
	now := midnight.Add(time.Hour)
	if _, e := a.Ingest(intervalBatch(testInterval(midnight.Add(-2*time.Second), 2, .02, 2)), now); e != nil {
		t.Fatal(e)
	}
	// The first day is covered, the second part is a genuinely new measured span.
	if _, e := a.Ingest(intervalBatch(testInterval(midnight.Add(-time.Second), 2, .02, 2)), now); e != nil {
		t.Fatal(e)
	}
	r := archiveReport(t, a, now)
	nearly(t, *r.Period.DistanceKM, .03)
	if r.TotalTrips != 1 {
		t.Fatal("covered day lost continuity ID", r.Trips)
	}
}

func TestArchiveRejectsSymlinkDirectoryWithoutWritingThroughIt(t *testing.T) {
	directory := t.TempDir()
	target := t.TempDir()
	if e := os.Symlink(target, filepath.Join(directory, "journeys")); e != nil {
		t.Skip("symlink creation unavailable on this host")
	}
	a := NewArchive(directory)
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, Japan)
	if a.State().Available {
		t.Fatal("symlink source accepted")
	}
	if _, e := a.Ingest(intervalBatch(testInterval(now.Add(-time.Second), 1, 0, 0)), now); e == nil {
		t.Fatal("symlink target written")
	}
	if entries, e := os.ReadDir(target); e != nil || len(entries) != 0 {
		t.Fatal("external target changed", e)
	}
}

func BenchmarkArchiveDenseIntervalBatch(b *testing.B) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, Japan)
	values := make([]MeasuredInterval, MaximumMeasuredBatch)
	for n := range values {
		values[n] = testInterval(now.Add(time.Duration(n-10000)*time.Second), 1, .001, 1)
	}
	batch := intervalBatch(values...)
	for n := 0; n < b.N; n++ {
		a := NewArchive(b.TempDir())
		if _, e := a.Ingest(batch, now); e != nil {
			b.Fatal(e)
		}
	}
}
