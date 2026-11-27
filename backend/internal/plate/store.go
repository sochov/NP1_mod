package plate

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Match is a rule snapshot stored with the record so later rule edits do not
// rewrite history.
type Match struct {
	RuleID    string `json:"ruleId"`
	RuleName  string `json:"ruleName"`
	SaveImage bool   `json:"saveImage"`
	Speak     bool   `json:"speak"`
}

// Speech states. "unsupported" means no verified device speech path exists;
// it is never reported as spoken.
const (
	SpeechNone        = ""
	SpeechRequested   = "requested"
	SpeechSpoken      = "spoken"
	SpeechFailed      = "failed"
	SpeechUnsupported = "unsupported"
)

type Record struct {
	ID          string    `json:"id"`
	EncounterID string    `json:"encounterId,omitempty"` // root track assigned when recording a returned vehicle
	FirstSeen   time.Time `json:"firstSeen"`
	LastSeen    time.Time `json:"lastSeen"`
	Sightings   int       `json:"sightings"`
	Reading     Reading   `json:"reading"`
	Display     string    `json:"display"`
	Location    *Location `json:"location,omitempty"`
	Box         [4]int    `json:"box"`
	Matches     []Match   `json:"matches"`
	ImageID     string    `json:"imageId,omitempty"`
	ImageNote   string    `json:"imageNote,omitempty"`
	Speech      string    `json:"speech,omitempty"`
	// votes records distinct frames per field value while the record is open.
	votes map[string]map[string]string
}

type Query struct {
	From, To    string // YYYY-MM-DD in JST, inclusive
	MatchedOnly bool
	Text        string
	Limit       int
}

type Outcome struct {
	Record     Record
	New        bool
	Skipped    string // reason when the detection was not recorded
	NewMatches []Match
}

var (
	jst         = time.FixedZone("JST", 9*3600)
	dayPattern  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	recordIDPat = regexp.MustCompile(`^[a-z0-9-]{8,48}$`)
)

const maxRecordLine = 64 * 1024
const MaxListLimit = 5000

// Store persists records as append-only JSON lines per JST day. The newest
// line for an ID wins. Open (still being merged) records live in memory and
// are included in List results.
type Store struct {
	dir              string
	externalImages   string
	clock            func() time.Time
	started          time.Time
	encountersLoaded bool
	mu               sync.Mutex
	open             map[string]*Record
	cand             map[string]*Record // provisional: not yet seen MinSightings times
}

func NewStore(dir string) *Store {
	return NewStoreWithExternalImages(dir, "")
}

// NewStoreWithExternalImages saves one representative frame for each recorded
// plate when a separate image directory has been configured.
func NewStoreWithExternalImages(dir, imageDir string) *Store {
	return &Store{dir: dir, externalImages: imageDir, clock: time.Now, started: time.Now(), open: map[string]*Record{}, cand: map[string]*Record{}}
}

func (s *Store) recordsDir() string { return filepath.Join(s.dir, "records") }
func (s *Store) imagesDir() string  { return filepath.Join(s.dir, "images") }

func Day(t time.Time) string { return t.In(jst).Format("2006-01-02") }

func newID(at time.Time) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "p" + strconv.FormatInt(at.UnixMilli(), 36) + "-" + hex.EncodeToString(b)
}

func (s *Store) appendRecord(r Record) error {
	if e := os.MkdirAll(s.recordsDir(), 0700); e != nil {
		return e
	}
	line, e := json.Marshal(r)
	if e != nil {
		return e
	}
	f, e := os.OpenFile(filepath.Join(s.recordsDir(), Day(r.FirstSeen)+".jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(append(line, '\n'))
	if ce := f.Close(); e == nil {
		e = ce
	}
	return e
}

// closeExpired persists and forgets open records not seen within the window.
func (s *Store) closeExpired(now time.Time, window time.Duration) error {
	var first error
	// Keep source snapshots of an active encounter together. Their frames
	// are cumulative per track and must not be counted again on retries.
	through := map[string]time.Time{}
	for _, r := range s.open {
		if strings.HasPrefix(r.ID, "pt-") && r.LastSeen.After(through[encounterKey(*r)]) {
			through[encounterKey(*r)] = r.LastSeen
		}
	}
	for key, r := range s.open {
		expired := now.Sub(r.LastSeen) > window
		if until, tracked := through[encounterKey(*r)]; tracked {
			expired = now.Sub(until) > max(window, encounterRetention)
		}
		if expired {
			if e := s.appendRecord(*r); e != nil && first == nil {
				first = e
			}
			delete(s.open, key)
		}
	}
	for key, r := range s.cand {
		if now.Sub(r.LastSeen) > window {
			delete(s.cand, key) // never confirmed by a second sighting
		}
	}
	return first
}

// Flush persists every open record (used on shutdown and settings changes).
func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.encountersLoaded = false
	return s.closeExpired(time.Unix(1<<40, 0), 0)
}

// Ingest records one detection. frameImage returns the JPEG for the
// detection's frame when an image is needed for a recorded plate.
func (s *Store) Ingest(d Detection, settings Settings, frameImage func(string) ([]byte, error)) (Outcome, error) {
	if d.Track != nil {
		return s.ingestTrack(d, settings, frameImage)
	}
	reading := Normalize(d.Reading)
	if !reading.Readable() {
		return Outcome{Skipped: "unreadable"}, nil
	}
	if reading.Confidence < settings.MinConfidence {
		return Outcome{Skipped: "low-confidence"}, nil
	}
	if d.At.IsZero() {
		d.At = s.clock()
	}
	window := time.Duration(settings.MergeSeconds) * time.Second
	s.mu.Lock()
	defer s.mu.Unlock()
	closeErr := s.closeExpired(d.At, window)
	if r := findCompatible(s.open, reading, d.At, window); r != nil {
		r.Sightings++
		if !absorb(r, reading, d, settings) {
			return Outcome{Record: *r}, closeErr
		}
		// The majority reading changed (e.g. the region became readable):
		// re-check the rules on the richer reading.
		added := s.applyRules(r, settings, d.Frame, frameImage)
		e := s.appendRecord(*r)
		if e == nil {
			e = closeErr
		}
		return Outcome{Record: *r, NewMatches: added}, e
	}
	r := findCompatible(s.cand, reading, d.At, window)
	if r == nil {
		r = &Record{ID: newID(d.At), FirstSeen: d.At, LastSeen: d.At, Sightings: 0, Reading: Reading{Number: reading.Number, Confidence: reading.Confidence}, Location: d.Location, Box: d.Box, Matches: []Match{}}
		s.cand[r.ID] = r
	}
	r.Sightings++
	absorb(r, reading, d, settings)
	instant := settings.InstantConfidence > 0 && settings.InstantConfidence < 1 && reading.Confidence >= settings.InstantConfidence && d.Box[2] >= InstantMinWidth
	if r.Sightings < max(1, settings.MinSightings) && !instant {
		return Outcome{Skipped: "provisional"}, closeErr
	}
	// Confirmed: becomes a record; rules act on this frame.
	delete(s.cand, r.ID)
	r.Display = r.Reading.Display()
	added := s.applyRules(r, settings, d.Frame, frameImage)
	s.open[r.ID] = r
	e := s.appendRecord(*r)
	if e == nil {
		e = closeErr
	}
	return Outcome{Record: *r, New: true, NewMatches: added}, e
}

// compatible reports whether two readings can be the same vehicle: the
// serial number is equal. Region, class and kana are voted on instead, since
// a single frame often misreads them (a conflicting field must not split one
// car into several records).
func compatible(a, b Reading) bool {
	return a.Number == b.Number
}

// absorb folds a sighting into a record and reports whether the displayed
// reading changed.
func absorb(r *Record, reading Reading, d Detection, settings Settings) bool {
	if d.At.After(r.LastSeen) {
		r.LastSeen = d.At
	}
	if reading.Confidence > r.Reading.Confidence {
		r.Reading.Confidence = reading.Confidence
	}
	if r.Location == nil && d.Location != nil {
		r.Location = d.Location
	}
	if d.Box != ([4]int{}) && r.ImageID == "" {
		r.Box = d.Box
	}
	if r.votes == nil {
		r.votes = map[string]map[string]string{}
	}
	before := r.Reading
	frame := d.Frame
	if frame == "" {
		frame = d.At.UTC().Format(time.RFC3339Nano)
	}
	minWidth, minShare := settings.FieldMinWidth, settings.FieldMinShare
	if minWidth == 0 {
		minWidth = FieldMinWidth
	}
	if minShare == 0 {
		minShare = DefaultFieldMinShare
	}
	for _, f := range []struct {
		name string
		dst  *string
		src  string
	}{{"region", &r.Reading.Region, reading.Region}, {"class", &r.Reading.Class, reading.Class}, {"kana", &r.Reading.Kana, reading.Kana}} {
		if d.Box[2] >= minWidth && f.src != "" {
			if r.votes[f.name] == nil {
				r.votes[f.name] = map[string]string{}
			}
			// A repeated delivery from the same frame cannot create another vote.
			if _, exists := r.votes[f.name][frame]; !exists {
				r.votes[f.name][frame] = f.src
			}
		}
		counts := map[string]int{}
		for _, value := range r.votes[f.name] {
			counts[value]++
		}
		*f.dst = FieldConsensus(counts, minShare)
	}
	r.Display = r.Reading.Display()
	return r.Reading.Region != before.Region || r.Reading.Class != before.Class || r.Reading.Kana != before.Kana
}

// FieldConsensus picks a field supported by at least two frames and the
// configured share of nonblank votes. An empty key counts as unresolved.
func FieldConsensus(counts map[string]int, minShare float64) string {
	best, n, total, tied := "", 0, 0, false
	for value, count := range counts {
		total += count
		if value == "" {
			continue
		}
		if count > n {
			best, n, tied = value, count, false
		} else if count == n {
			tied = true
		}
	}
	if !tied && n >= 2 && float64(n) >= float64(total)*minShare {
		return best
	}
	return ""
}

func findCompatible(records map[string]*Record, reading Reading, at time.Time, window time.Duration) *Record {
	var best *Record
	for _, r := range records {
		if !strings.HasPrefix(r.ID, "pt-") && at.Sub(r.LastSeen) <= window && compatible(r.Reading, reading) && (best == nil || r.LastSeen.After(best.LastSeen)) {
			best = r
		}
	}
	return best
}

// applyRules adds matches for rules the record now satisfies and saves the
// frame once when image collection is enabled or a newly matching rule requests it.
func (s *Store) applyRules(r *Record, settings Settings, frame string, frameImage func(string) ([]byte, error)) []Match {
	added := r.addRuleMatches(settings)
	saveImage := s.externalImages != ""
	for _, m := range added {
		saveImage = saveImage || m.SaveImage
	}
	if saveImage && r.ImageID == "" {
		r.ImageID, r.ImageNote = s.saveImage(r.ID, frame, frameImage)
	}
	return added
}

func (r *Record) addRuleMatches(settings Settings) []Match {
	have := map[string]bool{}
	for _, m := range r.Matches {
		have[m.RuleID] = true
	}
	added := []Match{}
	for _, rule := range MatchingRules(settings.Rules, r.Reading) {
		if have[rule.ID] {
			continue
		}
		m := Match{RuleID: rule.ID, RuleName: rule.Name, SaveImage: rule.SaveImage, Speak: rule.Speak}
		r.Matches = append(r.Matches, m)
		added = append(added, m)
	}
	return added
}

// SetSpeech updates the speech state of an open or persisted record.
func (s *Store) SetSpeech(id, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.open {
		if r.ID == id {
			r.Speech = state
			return s.appendRecord(*r)
		}
	}
	return errors.New("record is no longer open")
}

// SetLocation fills in the position of a record that has none, open or persisted (the
// recorder's GNSS log for the detection time appears only after the clip closes). A record
// that already has a position keeps it.
func (s *Store) SetLocation(id string, firstSeen time.Time, loc Location) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.open[id]; ok {
		if r.Location != nil {
			return nil
		}
		next := *r
		next.Location = &loc
		if err := s.appendRecord(next); err != nil {
			return err
		}
		s.open[id] = &next
		return nil
	}
	byID := map[string]Record{}
	if e := readDay(filepath.Join(s.recordsDir(), Day(firstSeen)+".jsonl"), byID); e != nil {
		return e
	}
	r, ok := byID[id]
	if !ok {
		return errors.New("record not found")
	}
	if r.Location != nil {
		return nil
	}
	r.Location = &loc
	return s.appendRecord(r)
}

func (s *Store) saveImage(id, frame string, frameImage func(string) ([]byte, error)) (string, string) {
	if frame == "" || frameImage == nil {
		return "", "フレームを取得できませんでした"
	}
	data, e := frameImage(frame)
	if e != nil || len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return "", "フレームを取得できませんでした"
	}
	dir := s.externalImages
	if dir == "" {
		dir = s.imagesDir()
		if e = os.MkdirAll(dir, 0700); e != nil {
			return "", "画像を保存できませんでした"
		}
	}
	tmp, e := os.CreateTemp(dir, ".img-*")
	if e != nil {
		return "", "画像を保存できませんでした"
	}
	name := tmp.Name()
	_, e = tmp.Write(data)
	if e == nil {
		e = tmp.Sync()
	}
	if ce := tmp.Close(); e == nil {
		e = ce
	}
	if e == nil {
		e = os.Rename(name, filepath.Join(dir, id+".jpg"))
	}
	if e != nil {
		os.Remove(name)
		return "", "画像を保存できませんでした"
	}
	return id, ""
}

// ImagePath returns the stored JPEG for a record ID, or an error when the ID
// is malformed or the image was pruned.
func (s *Store) ImagePath(id string) (string, error) {
	if !recordIDPat.MatchString(id) {
		return "", errors.New("invalid id")
	}
	for _, dir := range [...]string{s.externalImages, s.imagesDir()} {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, id+".jpg")
		if st, e := os.Lstat(p); e == nil && st.Mode().IsRegular() {
			return p, nil
		}
	}
	return "", os.ErrNotExist
}

func (q Query) validate(now time.Time) (Query, error) {
	if q.To == "" {
		q.To = Day(now)
	}
	if q.From == "" {
		q.From = Day(now.AddDate(0, 0, -6))
	}
	if !dayPattern.MatchString(q.From) || !dayPattern.MatchString(q.To) || q.From > q.To {
		return q, errors.New("期間を確認してください")
	}
	from, e1 := time.ParseInLocation("2006-01-02", q.From, jst)
	to, e2 := time.ParseInLocation("2006-01-02", q.To, jst)
	if e1 != nil || e2 != nil || to.Sub(from) > 366*24*time.Hour {
		return q, errors.New("期間は366日以内で指定してください")
	}
	if q.Limit <= 0 || q.Limit > MaxListLimit {
		q.Limit = MaxListLimit
	}
	q.Text = strings.TrimSpace(halfWidth(q.Text))
	return q, nil
}

func (q Query) accepts(r Record) bool {
	day := Day(r.FirstSeen)
	if day < q.From || day > q.To {
		return false
	}
	if q.MatchedOnly && len(r.Matches) == 0 {
		return false
	}
	if q.Text != "" {
		haystack := halfWidth(r.Reading.Region + r.Reading.Class + r.Reading.Kana + r.Reading.Number)
		if !strings.Contains(haystack, katakanaToHiragana(q.Text)) {
			return false
		}
	}
	return true
}

// List returns matching records newest first and whether the limit cut it.
func (s *Store) List(q Query) ([]Record, bool, error) {
	return s.list(q, true)
}

// ListAll returns every matching record in the bounded date range. It is used
// by explicit export/synchronization operations where silently dropping the
// oldest records would be worse than returning a large result.
func (s *Store) ListAll(q Query) ([]Record, error) {
	records, _, err := s.list(q, false)
	return records, err
}

func (s *Store) list(q Query, capped bool) ([]Record, bool, error) {
	q, e := q.validate(s.clock())
	if e != nil {
		return nil, false, e
	}
	byID := map[string]Record{}
	from, _ := time.ParseInLocation("2006-01-02", q.From, jst)
	to, _ := time.ParseInLocation("2006-01-02", q.To, jst)
	var readErr error
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		if e := readDay(filepath.Join(s.recordsDir(), day.Format("2006-01-02")+".jsonl"), byID); e != nil && !errors.Is(e, os.ErrNotExist) {
			readErr = e
		}
	}
	if err := s.loadEncounterRoots(from, byID); err != nil {
		readErr = err
	}
	// A fragment is journaled on its own first day. Complete encounters
	// that began in the requested range and can continue past midnight.
	for boundary := to.AddDate(0, 0, 1); readErr == nil; boundary = boundary.AddDate(0, 0, 1) {
		linked := false
		for _, r := range byID {
			if root, haveRoot := byID[encounterKey(r)]; haveRoot && !root.FirstSeen.Before(from) &&
				root.FirstSeen.Before(to.AddDate(0, 0, 1)) && strings.HasPrefix(r.ID, "pt-") &&
				!r.LastSeen.Before(boundary.Add(-encounterGap)) {
				linked = true
				break
			}
		}
		if !linked {
			break
		}
		err := readDay(filepath.Join(s.recordsDir(), Day(boundary)+".jsonl"), byID)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		readErr = err
	}

	s.mu.Lock()
	for _, r := range s.open {
		byID[r.ID] = *r
	}
	s.mu.Unlock()
	out := []Record{}
	for _, r := range projectEncounters(byID) {
		if q.accepts(r) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if !out[a].FirstSeen.Equal(out[b].FirstSeen) {
			return out[a].FirstSeen.After(out[b].FirstSeen)
		}
		return out[a].ID > out[b].ID
	})
	truncated := capped && len(out) > q.Limit
	if truncated {
		out = out[:q.Limit]
	}
	return out, truncated, readErr
}

func readDay(path string, into map[string]Record) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), maxRecordLine)
	for scanner.Scan() {
		var r Record
		// A torn final line after power loss is skipped, not fatal.
		if json.Unmarshal(scanner.Bytes(), &r) == nil && recordIDPat.MatchString(r.ID) {
			if r.Matches == nil {
				r.Matches = []Match{}
			}
			into[r.ID] = r
		}
	}
	return scanner.Err()
}

type Usage struct {
	RecordBytes int64 `json:"recordBytes"`
	RecordDays  int   `json:"recordDays"`
	ImageBytes  int64 `json:"imageBytes"`
	ImageCount  int   `json:"imageCount"`
	OpenRecords int   `json:"openRecords"`
}

type fileInfo struct {
	path string
	size int64
	mod  time.Time
}

func listFiles(dir, suffix string) []fileInfo {
	entries, _ := os.ReadDir(dir)
	out := []fileInfo{}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), suffix) {
			continue
		}
		if info, e := entry.Info(); e == nil {
			out = append(out, fileInfo{filepath.Join(dir, entry.Name()), info.Size(), info.ModTime()})
		}
	}
	return out
}

func (s *Store) imageFiles() []fileInfo {
	files := listFiles(s.imagesDir(), ".jpg")
	if s.externalImages != "" && s.externalImages != s.imagesDir() {
		files = append(files, listFiles(s.externalImages, ".jpg")...)
	}
	return files
}

func (s *Store) Usage() Usage {
	u := Usage{}
	for _, f := range listFiles(s.recordsDir(), ".jsonl") {
		u.RecordBytes += f.size
		u.RecordDays++
	}
	for _, f := range s.imageFiles() {
		u.ImageBytes += f.size
		u.ImageCount++
	}
	s.mu.Lock()
	u.OpenRecords = len(s.open)
	s.mu.Unlock()
	return u
}

// Prune applies the retention period to records and images and keeps the
// image store under its quota by deleting the oldest images first.
func (s *Store) Prune(settings Settings) (int, error) {
	now := s.clock()
	cutoff := Day(now.AddDate(0, 0, -settings.RetentionDays))
	removed := 0
	var first error
	remove := func(path string) {
		if e := os.Remove(path); e != nil && !errors.Is(e, os.ErrNotExist) && first == nil {
			first = e
		} else if e == nil {
			removed++
		}
	}
	for _, f := range listFiles(s.recordsDir(), ".jsonl") {
		day := strings.TrimSuffix(filepath.Base(f.path), ".jsonl")
		if dayPattern.MatchString(day) && day < cutoff {
			remove(f.path)
		}
	}
	images := s.imageFiles()
	sort.Slice(images, func(a, b int) bool { return images[a].mod.Before(images[b].mod) })
	var total int64
	kept := images[:0]
	for _, f := range images {
		if Day(f.mod) < cutoff {
			remove(f.path)
			continue
		}
		kept = append(kept, f)
		total += f.size
	}
	quota := int64(settings.ImageQuotaMB) << 20
	for i := 0; total > quota && i < len(kept); i++ {
		remove(kept[i].path)
		total -= kept[i].size
	}
	return removed, first
}

func (r Record) String() string { return fmt.Sprintf("%s %s", r.ID, r.Display) }
