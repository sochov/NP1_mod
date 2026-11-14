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
	ID        string    `json:"id"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
	Sightings int       `json:"sightings"`
	Reading   Reading   `json:"reading"`
	Display   string    `json:"display"`
	Location  *Location `json:"location,omitempty"`
	Box       [4]int    `json:"box"`
	Matches   []Match   `json:"matches"`
	ImageID   string    `json:"imageId,omitempty"`
	ImageNote string    `json:"imageNote,omitempty"`
	Speech    string    `json:"speech,omitempty"`
	// votes holds confidence-weighted readings per field while the record is
	// open; the displayed field is the majority (single frames misread
	// regions and kana, especially at night).
	votes map[string]map[string]float64
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
	dir   string
	clock func() time.Time
	mu    sync.Mutex
	open  map[string]*Record
	cand  map[string]*Record // provisional: not yet seen MinSightings times
}

func NewStore(dir string) *Store {
	return &Store{dir: dir, clock: time.Now, open: map[string]*Record{}, cand: map[string]*Record{}}
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
	for key, r := range s.open {
		if now.Sub(r.LastSeen) > window {
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
	return s.closeExpired(time.Unix(1<<40, 0), 0)
}

// Ingest records one detection. frameImage returns the JPEG for the
// detection's frame; it is called only when a matching rule asks for it.
func (s *Store) Ingest(d Detection, settings Settings, frameImage func(string) ([]byte, error)) (Outcome, error) {
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
		if !absorb(r, reading, d) {
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
		r = &Record{ID: newID(d.At), FirstSeen: d.At, LastSeen: d.At, Sightings: 0, Reading: reading, Location: d.Location, Box: d.Box, Matches: []Match{}}
		s.cand[r.ID] = r
	}
	r.Sightings++
	absorb(r, reading, d)
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
func absorb(r *Record, reading Reading, d Detection) bool {
	if d.At.After(r.LastSeen) {
		r.LastSeen = d.At
	}
	if reading.Confidence > r.Reading.Confidence {
		r.Reading.Confidence = reading.Confidence
	}
	if r.Location == nil && d.Location != nil {
		r.Location = d.Location
	}
	if d.Box != ([4]int{}) {
		r.Box = d.Box
	}
	if r.votes == nil {
		r.votes = map[string]map[string]float64{}
	}
	before := r.Reading
	for _, f := range []struct {
		name string
		dst  *string
		src  string
	}{{"region", &r.Reading.Region, reading.Region}, {"class", &r.Reading.Class, reading.Class}, {"kana", &r.Reading.Kana, reading.Kana}} {
		if f.src == "" {
			continue
		}
		if r.votes[f.name] == nil {
			r.votes[f.name] = map[string]float64{}
		}
		r.votes[f.name][f.src] += max(reading.Confidence, 0.01)
		best, bestVote := *f.dst, -1.0
		for value, vote := range r.votes[f.name] {
			if vote > bestVote || (vote == bestVote && value < best) {
				best, bestVote = value, vote
			}
		}
		*f.dst = best
	}
	r.Display = r.Reading.Display()
	return r.Reading.Region != before.Region || r.Reading.Class != before.Class || r.Reading.Kana != before.Kana
}

func findCompatible(records map[string]*Record, reading Reading, at time.Time, window time.Duration) *Record {
	var best *Record
	for _, r := range records {
		if at.Sub(r.LastSeen) <= window && compatible(r.Reading, reading) && (best == nil || r.LastSeen.After(best.LastSeen)) {
			best = r
		}
	}
	return best
}

// applyRules adds matches for rules the record now satisfies and saves the
// frame once when a newly matching rule asks for an image.
func (s *Store) applyRules(r *Record, settings Settings, frame string, frameImage func(string) ([]byte, error)) []Match {
	have := map[string]bool{}
	for _, m := range r.Matches {
		have[m.RuleID] = true
	}
	added := []Match{}
	saveImage := false
	for _, rule := range MatchingRules(settings.Rules, r.Reading) {
		if have[rule.ID] {
			continue
		}
		m := Match{RuleID: rule.ID, RuleName: rule.Name, SaveImage: rule.SaveImage, Speak: rule.Speak}
		r.Matches = append(r.Matches, m)
		added = append(added, m)
		saveImage = saveImage || rule.SaveImage
	}
	if saveImage && r.ImageID == "" {
		r.ImageID, r.ImageNote = s.saveImage(r.ID, frame, frameImage)
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

func (s *Store) saveImage(id, frame string, frameImage func(string) ([]byte, error)) (string, string) {
	if frame == "" || frameImage == nil {
		return "", "フレームを取得できませんでした"
	}
	data, e := frameImage(frame)
	if e != nil || len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return "", "フレームを取得できませんでした"
	}
	if e = os.MkdirAll(s.imagesDir(), 0700); e != nil {
		return "", "画像を保存できませんでした"
	}
	tmp, e := os.CreateTemp(s.imagesDir(), ".img-*")
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
		e = os.Rename(name, filepath.Join(s.imagesDir(), id+".jpg"))
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
	p := filepath.Join(s.imagesDir(), id+".jpg")
	if st, e := os.Lstat(p); e != nil || !st.Mode().IsRegular() {
		return "", os.ErrNotExist
	}
	return p, nil
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
	s.mu.Lock()
	for _, r := range s.open {
		byID[r.ID] = *r
	}
	s.mu.Unlock()
	out := []Record{}
	for _, r := range byID {
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
	truncated := len(out) > q.Limit
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

func (s *Store) Usage() Usage {
	u := Usage{}
	for _, f := range listFiles(s.recordsDir(), ".jsonl") {
		u.RecordBytes += f.size
		u.RecordDays++
	}
	for _, f := range listFiles(s.imagesDir(), ".jpg") {
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
	images := listFiles(s.imagesDir(), ".jpg")
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
