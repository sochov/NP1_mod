package trips

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const MaximumArchiveRuns = 50000
const MaximumArchiveBytes = 16 * 1024 * 1024

var ErrArchiveUnavailable = errors.New("journey archive unavailable")

// archiveRun is continuous measured coverage within a single JST day. Merging
// adjacent intervals makes retention depend on journeys and gaps, not GNSS Hz.
// There are deliberately no coordinates or source file/device identifiers.
type archiveRun struct {
	ID             string    `json:"id"`
	Start          time.Time `json:"start"`
	End            time.Time `json:"end"`
	DistanceKM     float64   `json:"distanceKm"`
	DrivingSeconds float64   `json:"drivingSeconds"`
	Samples        int       `json:"samples"`
	FirstReal      bool      `json:"firstReal"`
	LastReal       bool      `json:"lastReal"`
}
type archiveDay struct {
	Date string       `json:"date"`
	Runs []archiveRun `json:"runs"`
	// An endpoint exactly at midnight belongs to the next day, even if its
	// preceding positive-duration interval is entirely in the previous day.
	MidnightPointID string `json:"midnightPointId,omitempty"`
}
type archiveData struct {
	Version         int          `json:"version"`
	Days            []archiveDay `json:"days"`
	Watermark       time.Time    `json:"watermark"`
	CollectedAt     time.Time    `json:"collectedAt"`
	Health          SourceHealth `json:"health"`
	Backfilling     bool         `json:"backfilling"`
	SourcePartial   bool         `json:"sourcePartial"`
	OverlapRejected uint64       `json:"overlapRejected"`
	LimitReached    bool         `json:"limitReached"`
}
type archiveEnvelope struct {
	Version int             `json:"version"`
	SHA256  string          `json:"sha256"`
	Payload json.RawMessage `json:"payload"`
}

type ArchiveState struct {
	Available       bool      `json:"available"`
	Days            int       `json:"days"`
	Runs            int       `json:"runs"`
	Watermark       time.Time `json:"watermark"`
	CollectedAt     time.Time `json:"collectedAt"`
	Backfilling     bool      `json:"backfilling"`
	Partial         bool      `json:"partial"`
	OverlapRejected uint64    `json:"overlapRejected"`
	LimitReached    bool      `json:"limitReached"`
	Error           string    `json:"error,omitempty"`
}
type IngestResult struct {
	Added      int  `json:"added"`
	Duplicates int  `json:"duplicates"`
	Rejected   int  `json:"rejected"`
	Limited    bool `json:"limited"`
}

type Archive struct {
	mu          sync.Mutex
	directory   string
	loaded      bool
	loadError   error
	data        archiveData
	maximumRuns int
	maximumDays int
}

// NewArchive and all read methods are side-effect free. Only Ingest creates or
// replaces the non-root worker's private aggregate file.
func NewArchive(stateDirectory string) *Archive {
	directory := ""
	if stateDirectory != "" {
		directory = filepath.Join(stateDirectory, "journeys")
	}
	return &Archive{directory: directory, maximumRuns: MaximumArchiveRuns, maximumDays: MaximumHistoryDays}
}

func decodeStrict(data []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if e := d.Decode(target); e != nil {
		return e
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("multiple JSON values")
	}
	return nil
}
func privateArchiveDirectory(directory string) error {
	if directory == "" {
		return ErrArchiveUnavailable
	}
	absolute, e := filepath.Abs(directory)
	if e != nil {
		return e
	}
	for p := absolute; ; p = filepath.Dir(p) {
		info, e := os.Lstat(p)
		if e == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return errors.New("archive directory is not a normal directory")
		}
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}
func (a *Archive) load() error {
	if a.loaded {
		return a.loadError
	}
	a.loaded = true
	a.data = archiveData{Version: 1, Days: []archiveDay{}}
	if e := privateArchiveDirectory(a.directory); e != nil {
		a.loadError = ErrArchiveUnavailable
		return a.loadError
	}
	filename := filepath.Join(a.directory, "archive.json")
	info, e := os.Lstat(filename)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > MaximumArchiveBytes || !archiveFilePrivate(info) {
		a.loadError = ErrArchiveUnavailable
		return a.loadError
	}
	f, e := os.Open(filename)
	if e != nil {
		a.loadError = ErrArchiveUnavailable
		return a.loadError
	}
	raw, e := io.ReadAll(io.LimitReader(f, MaximumArchiveBytes+1))
	f.Close()
	var envelope archiveEnvelope
	if e != nil || len(raw) > MaximumArchiveBytes || decodeStrict(raw, &envelope) != nil || envelope.Version != 1 || len(envelope.SHA256) != 64 {
		a.loadError = ErrArchiveUnavailable
		return a.loadError
	}
	hash := sha256.Sum256(envelope.Payload)
	var data archiveData
	if hex.EncodeToString(hash[:]) != envelope.SHA256 || decodeStrict(envelope.Payload, &data) != nil || a.validateData(data) != nil {
		a.loadError = ErrArchiveUnavailable
		return a.loadError
	}
	a.data = data
	return nil
}
func validArchiveNumber(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }
func validMeasured(v MeasuredInterval, now time.Time) bool {
	seconds := v.End.Sub(v.Start).Seconds()
	return !v.Start.IsZero() && v.End.After(v.Start) && v.End.Sub(v.Start) <= MaximumGap && !v.End.After(now) && !v.Start.Before(now.AddDate(-100, 0, 0)) && validArchiveNumber(v.DistanceKM) && validArchiveNumber(v.DrivingSeconds) && v.DistanceKM <= seconds*.075+1e-8 && v.DrivingSeconds <= seconds+1e-8
}
func (a *Archive) validateData(data archiveData) error {
	if data.Version != 1 || len(data.Days) > a.maximumDays {
		return errors.New("unsupported archive")
	}
	runs := 0
	lastDate := ""
	var maxEnd time.Time
	for _, day := range data.Days {
		date, e := parseHistoryDate(day.Date)
		if e != nil || day.Date <= lastDate {
			return errors.New("invalid archive day ordering")
		}
		lastDate = day.Date
		var previous time.Time
		for _, run := range day.Runs {
			seconds := run.End.Sub(run.Start).Seconds()
			if run.ID == "" || len(run.ID) > 64 || !run.Start.Before(run.End) || run.Start.Before(date) || run.End.After(date.AddDate(0, 0, 1)) || (!previous.IsZero() && run.Start.Before(previous)) || !validArchiveNumber(run.DistanceKM) || !validArchiveNumber(run.DrivingSeconds) || run.DistanceKM > seconds*.075+1e-7 || run.DrivingSeconds > seconds+1e-7 || run.Samples < 0 || run.Samples > 1000000000 {
				return errors.New("invalid archive run")
			}
			previous = run.End
			if run.End.After(maxEnd) {
				maxEnd = run.End
			}
			runs++
		}
		if len(day.MidnightPointID) > 64 {
			return errors.New("invalid midnight point")
		}
	}
	if runs > a.maximumRuns || maxEnd.After(data.Watermark) {
		return errors.New("archive bounds exceeded")
	}
	return nil
}
func cloneArchive(data archiveData) archiveData {
	out := data
	out.Days = make([]archiveDay, len(data.Days))
	for n, day := range data.Days {
		out.Days[n] = day
		out.Days[n].Runs = append([]archiveRun{}, day.Runs...)
	}
	return out
}
func (a *Archive) save(data archiveData) error {
	if e := privateArchiveDirectory(a.directory); e != nil {
		return e
	}
	if e := os.MkdirAll(a.directory, 0700); e != nil {
		return e
	}
	payload, e := json.Marshal(data)
	if e != nil {
		return e
	}
	hash := sha256.Sum256(payload)
	raw, e := json.Marshal(archiveEnvelope{Version: 1, SHA256: hex.EncodeToString(hash[:]), Payload: payload})
	if e != nil {
		return e
	}
	if len(raw) > MaximumArchiveBytes {
		return errors.New("archive file limit exceeded")
	}
	f, e := os.CreateTemp(a.directory, ".archive-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(raw)
	}
	if e == nil {
		e = f.Sync()
	}
	closeError := f.Close()
	if e == nil {
		e = closeError
	}
	if e != nil {
		return e
	}
	if e = os.Rename(name, filepath.Join(a.directory, "archive.json")); e != nil {
		return e
	}
	return syncArchiveDirectory(a.directory)
}
func runCount(data archiveData) int {
	count := 0
	for _, day := range data.Days {
		count += len(day.Runs)
	}
	return count
}
func dayIndex(data *archiveData, date string) int {
	return sort.Search(len(data.Days), func(n int) bool { return data.Days[n].Date >= date })
}
func findDay(data *archiveData, date string) *archiveDay {
	n := dayIndex(data, date)
	if n < len(data.Days) && data.Days[n].Date == date {
		return &data.Days[n]
	}
	return nil
}
func ensureDay(data *archiveData, date string) *archiveDay {
	n := dayIndex(data, date)
	if n == len(data.Days) || data.Days[n].Date != date {
		data.Days = append(data.Days, archiveDay{})
		copy(data.Days[n+1:], data.Days[n:])
		data.Days[n] = archiveDay{Date: date, Runs: []archiveRun{}}
	}
	return &data.Days[n]
}
func runPosition(runs []archiveRun, start time.Time) int {
	return sort.Search(len(runs), func(n int) bool { return !runs[n].Start.Before(start) })
}

// coverage returns 1 for a complete duplicate, 0 for a disjoint span and -1 for
// a partial overlap. Partial quantities cannot be recovered from merged totals.
func coverage(day *archiveDay, part archiveRun) int {
	if day == nil {
		return 0
	}
	n := runPosition(day.Runs, part.Start)
	if n > 0 {
		previous := day.Runs[n-1]
		if !part.Start.Before(previous.Start) && !part.End.After(previous.End) {
			return 1
		}
		if part.Start.Before(previous.End) {
			return -1
		}
	}
	if n < len(day.Runs) {
		next := day.Runs[n]
		if !part.Start.Before(next.Start) && !part.End.After(next.End) {
			return 1
		}
		if part.End.After(next.Start) {
			return -1
		}
	}
	return 0
}
func splitMeasured(v MeasuredInterval) []archiveRun {
	parts := []archiveRun{}
	for start := v.Start; start.Before(v.End); {
		local := start.In(Japan)
		boundary := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, Japan)
		end := v.End
		if boundary.Before(end) {
			end = boundary
		}
		fraction := end.Sub(start).Seconds() / v.End.Sub(v.Start).Seconds()
		part := archiveRun{Start: start.UTC(), End: end.UTC(), DistanceKM: v.DistanceKM * fraction, DrivingSeconds: v.DrivingSeconds * fraction}
		part.FirstReal = start.Equal(v.Start)
		part.LastReal = end.Equal(v.End) && v.End.In(Japan).Format("2006-01-02") == local.Format("2006-01-02")
		if part.FirstReal {
			part.Samples++
		}
		if part.LastReal {
			part.Samples++
		}
		parts = append(parts, part)
		start = end
	}
	return parts
}
func adjacentID(data *archiveData, at time.Time, previous bool) string {
	date := at.In(Japan).Format("2006-01-02")
	if previous {
		date = at.Add(-time.Nanosecond).In(Japan).Format("2006-01-02")
	}
	day := findDay(data, date)
	if day == nil {
		return ""
	}
	position := runPosition(day.Runs, at)
	if !previous && position < len(day.Runs) && day.Runs[position].Start.Equal(at) {
		return day.Runs[position].ID
	}
	if previous && position > 0 && day.Runs[position-1].End.Equal(at) {
		return day.Runs[position-1].ID
	}
	return ""
}
func coveredID(day *archiveDay, part archiveRun) string {
	if day == nil {
		return ""
	}
	n := runPosition(day.Runs, part.Start)
	if n > 0 && !part.End.After(day.Runs[n-1].End) {
		return day.Runs[n-1].ID
	}
	if n < len(day.Runs) && !part.Start.Before(day.Runs[n].Start) && !part.End.After(day.Runs[n].End) {
		return day.Runs[n].ID
	}
	return ""
}
func addedRunCount(day *archiveDay, part archiveRun) int {
	if day == nil {
		return 1
	}
	n := runPosition(day.Runs, part.Start)
	delta := 1
	if n > 0 && day.Runs[n-1].End.Equal(part.Start) {
		delta--
	}
	if n < len(day.Runs) && part.End.Equal(day.Runs[n].Start) {
		delta--
	}
	return delta
}
func endsAtMidnight(at time.Time) bool {
	local := at.In(Japan)
	return local.Hour() == 0 && local.Minute() == 0 && local.Second() == 0 && local.Nanosecond() == 0
}
func healthChanged(a, b SourceHealth) bool {
	return a.Files != b.Files || a.UnreadableFiles != b.UnreadableFiles || a.NoFix != b.NoFix || a.BadChecksum != b.BadChecksum || a.Malformed != b.Malformed || a.Truncated != b.Truncated
}
func (a *Archive) persistNeeded(previous, next archiveData, added int) bool {
	if added > 0 || previous.CollectedAt.IsZero() || healthChanged(previous.Health, next.Health) || previous.Backfilling != next.Backfilling || previous.SourcePartial != next.SourcePartial || previous.LimitReached != next.LimitReached || previous.OverlapRejected != next.OverlapRejected || !previous.Watermark.Equal(next.Watermark) {
		return true
	}
	return false
}
func unifyID(data *archiveData, from, to string) {
	if from == "" || from == to {
		return
	}
	for n := range data.Days {
		day := &data.Days[n]
		if day.MidnightPointID == from {
			day.MidnightPointID = to
		}
		for j := range day.Runs {
			if day.Runs[j].ID == from {
				day.Runs[j].ID = to
			}
		}
	}
}
func addPart(data *archiveData, part archiveRun) {
	date := part.Start.In(Japan).Format("2006-01-02")
	day := ensureDay(data, date)
	n := runPosition(day.Runs, part.Start)
	if n > 0 && day.Runs[n-1].End.Equal(part.Start) {
		left := day.Runs[n-1]
		part.Start = left.Start
		part.DistanceKM += left.DistanceKM
		part.DrivingSeconds += left.DrivingSeconds
		part.Samples += left.Samples
		if left.LastReal && part.FirstReal {
			part.Samples--
		}
		part.FirstReal = left.FirstReal
		day.Runs = append(day.Runs[:n-1], day.Runs[n:]...)
		n--
	}
	if n < len(day.Runs) && part.End.Equal(day.Runs[n].Start) {
		right := day.Runs[n]
		part.End = right.End
		part.DistanceKM += right.DistanceKM
		part.DrivingSeconds += right.DrivingSeconds
		part.Samples += right.Samples
		if part.LastReal && right.FirstReal {
			part.Samples--
		}
		part.LastReal = right.LastReal
		day.Runs = append(day.Runs[:n], day.Runs[n+1:]...)
	}
	day.Runs = append(day.Runs, archiveRun{})
	copy(day.Runs[n+1:], day.Runs[n:])
	day.Runs[n] = part
}

// Ingest accepts only measured, non-future intervals. Its file replacement is
// atomic and fsynced; a failed write leaves the active in-memory state unchanged.
// Replayed coverage is immutable, while disjoint late arrivals can fill gaps.
func (a *Archive) Ingest(batch MeasuredIntervalBatch, now time.Time) (IngestResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := IngestResult{}
	if e := a.load(); e != nil {
		return result, e
	}
	if len(batch.Intervals) > MaximumMeasuredBatch || batch.After.After(now) || batch.Health.Files < 0 || batch.Health.UnreadableFiles < 0 || batch.Health.NoFix < 0 || batch.Health.BadChecksum < 0 || batch.Health.Malformed < 0 {
		return result, errors.New("invalid measured batch")
	}
	previous := batch.After
	for _, v := range batch.Intervals {
		if !validMeasured(v, now) || !v.End.After(batch.After) || v.Start.Before(previous) && !previous.Equal(batch.After) {
			return result, errors.New("invalid measured interval")
		}
		previous = v.End
	}
	if !batch.Next.Equal(previous) || batch.More && len(batch.Intervals) == 0 {
		return result, errors.New("invalid measured cursor")
	}
	next := cloneArchive(a.data)
	next.Health = batch.Health
	next.CollectedAt = now.UTC()
	next.Backfilling = batch.More
	next.SourcePartial = next.SourcePartial || batch.Health.Truncated
	totalRuns := runCount(next)
	for _, v := range batch.Intervals {
		parts := splitMeasured(v)
		overlap := false
		newParts := 0
		newDays := map[string]bool{}
		runDelta := 0
		anchors := []string{}
		for _, part := range parts {
			date := part.Start.In(Japan).Format("2006-01-02")
			day := findDay(&next, date)
			state := coverage(day, part)
			if state < 0 {
				overlap = true
			}
			if state == 0 {
				newParts++
				runDelta += addedRunCount(day, part)
				if day == nil {
					newDays[date] = true
				}
			}
			if state == 1 {
				anchors = append(anchors, coveredID(day, part))
			}
		}
		if overlap {
			result.Rejected++
			if next.OverlapRejected < ^uint64(0) {
				next.OverlapRejected++
			}
			if v.End.After(next.Watermark) {
				next.Watermark = v.End
			}
			continue
		}
		if newParts == 0 {
			result.Duplicates++
			if v.End.After(next.Watermark) {
				next.Watermark = v.End
			}
			continue
		}
		if endsAtMidnight(v.End) && findDay(&next, v.End.In(Japan).Format("2006-01-02")) == nil {
			newDays[v.End.In(Japan).Format("2006-01-02")] = true
		}
		if len(next.Days)+len(newDays) > a.maximumDays || totalRuns+runDelta > a.maximumRuns {
			next.LimitReached = true
			result.Limited = true
			result.Rejected++
			if v.End.After(next.Watermark) {
				next.Watermark = v.End
			}
			continue
		}
		id := adjacentID(&next, v.Start, true)
		rightID := adjacentID(&next, v.End, false)
		if id == "" && len(anchors) > 0 {
			id = anchors[0]
		}
		if id == "" {
			id = rightID
		}
		if id == "" {
			id = fmt.Sprintf("journey-%x", v.Start.UnixNano())
		}
		unifyID(&next, rightID, id)
		for _, anchor := range anchors {
			unifyID(&next, anchor, id)
		}
		for _, part := range parts {
			if coverage(findDay(&next, part.Start.In(Japan).Format("2006-01-02")), part) != 0 {
				continue
			}
			part.ID = id
			addPart(&next, part)
		}
		local := v.End.In(Japan)
		if endsAtMidnight(v.End) {
			ensureDay(&next, local.Format("2006-01-02")).MidnightPointID = id
		}
		if v.End.After(next.Watermark) {
			next.Watermark = v.End
		}
		totalRuns += runDelta
		result.Added++
	}
	if a.persistNeeded(a.data, next, result.Added) {
		if e := a.save(next); e != nil {
			return IngestResult{}, e
		}
	}
	a.data = next
	return result, nil
}
func (a *Archive) State() ArchiveState {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.load() != nil {
		return ArchiveState{Available: false, Error: "storage-unavailable"}
	}
	d := a.data
	return ArchiveState{Available: true, Days: len(d.Days), Runs: runCount(d), Watermark: d.Watermark, CollectedAt: d.CollectedAt, Backfilling: d.Backfilling, Partial: d.SourcePartial || d.LimitReached || d.OverlapRejected > 0, OverlapRejected: d.OverlapRejected, LimitReached: d.LimitReached}
}
