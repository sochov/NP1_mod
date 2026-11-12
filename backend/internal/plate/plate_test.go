package plate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeAndDisplay(t *testing.T) {
	r := Normalize(Reading{Region: " 品川 ", Class: "３０ａ", Kana: "ア", Number: "１２－３４", Confidence: 1.4})
	if r.Region != "品川" || r.Class != "30A" || r.Kana != "あ" || r.Number != "1234" || r.Confidence != 1 {
		t.Fatalf("normalize = %+v", r)
	}
	if got := r.Display(); got != "品川 30A あ 12-34" {
		t.Fatalf("display = %q", got)
	}
	if got := Normalize(Reading{Number: "・・・8"}).Display(); got != "・・・8" {
		t.Fatalf("short display = %q", got)
	}
}

func TestRuleMatching(t *testing.T) {
	reading := Normalize(Reading{Region: "品川", Class: "300", Kana: "あ", Number: "1234"})
	cases := []struct {
		rule Rule
		want bool
	}{
		{Rule{Enabled: true, Number: "1234"}, true},
		{Rule{Enabled: true, Number: "12**"}, true},
		{Rule{Enabled: true, Number: "123"}, false},
		{Rule{Enabled: true, Region: "品川", Class: "3**"}, true},
		{Rule{Enabled: true, Region: "練馬", Number: "1234"}, false},
		{Rule{Enabled: true, Kana: "あ"}, true},
		{Rule{Enabled: false, Number: "1234"}, false},
	}
	for i, c := range cases {
		if got := c.rule.Matches(reading); got != c.want {
			t.Errorf("case %d: %+v = %v", i, c.rule, got)
		}
	}
	unread := Normalize(Reading{Number: "1234"})
	if (Rule{Enabled: true, Region: "品川", Number: "1234"}).Matches(unread) {
		t.Fatal("unread region must not satisfy a region condition")
	}
}

func validSettings(rules ...Rule) Settings {
	s := DefaultSettings()
	s.Rules = rules
	s.MinSightings = 1 // most tests exercise single-frame behavior
	return s
}

func TestValidateSettings(t *testing.T) {
	ok, e := ValidateSettings(validSettings(Rule{ID: "r1", Name: " 家族の車 ", Enabled: true, Class: "３００", Kana: "サ", Number: "0012"}))
	if e != nil {
		t.Fatal(e)
	}
	if r := ok.Rules[0]; r.Name != "家族の車" || r.Class != "300" || r.Kana != "さ" || r.Number != "12" {
		t.Fatalf("normalized rule = %+v", r)
	}
	bad := []Rule{
		{ID: "r1", Name: "空", Enabled: true},
		{ID: "R1", Name: "x", Number: "1"},
		{ID: "r1", Name: "x", Number: "12345"},
		{ID: "r1", Name: "x", Class: "3000"},
		{ID: "r1", Name: "x", Kana: "あい"},
		{ID: "r1", Name: "x", Kana: "A"},
		{ID: "r1", Name: "", Number: "1"},
		{ID: "r1", Name: "x\ty", Number: "1"},
	}
	for i, rule := range bad {
		if _, e := ValidateSettings(validSettings(rule)); e == nil {
			t.Errorf("bad rule %d accepted: %+v", i, rule)
		}
	}
	if _, e := ValidateSettings(validSettings(Rule{ID: "a1", Name: "x", Number: "1"}, Rule{ID: "a1", Name: "y", Number: "2"})); e == nil {
		t.Fatal("duplicate IDs accepted")
	}
	s := DefaultSettings()
	s.RetentionDays = 0
	if _, e := ValidateSettings(s); e == nil {
		t.Fatal("retention 0 accepted")
	}
}

var jpeg = []byte{0xFF, 0xD8, 0xFF, 0xE0, 1, 2, 3}

func newTestStore(t *testing.T, now *time.Time) *Store {
	s := NewStore(t.TempDir())
	s.clock = func() time.Time { return *now }
	return s
}

func TestIngestMergesSameVehicleAndSavesMatchedImage(t *testing.T) {
	now := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	s := newTestStore(t, &now)
	settings, _ := ValidateSettings(validSettings(Rule{ID: "r1", Name: "対象", Enabled: true, Number: "1234", SaveImage: true, Speak: true}))
	fetches := 0
	frame := func(id string) ([]byte, error) { fetches++; return jpeg, nil }
	loc := &Location{At: now, Lat: 35.6, Lon: 139.7}
	d := Detection{At: now, Reading: Reading{Region: "品川", Class: "300", Kana: "あ", Number: "12-34", Confidence: 0.9}, Frame: "f1"}
	first, e := s.Ingest(d, settings, frame)
	if e != nil || !first.New || len(first.NewMatches) != 1 || first.Record.ImageID == "" {
		t.Fatalf("first = %+v, %v", first, e)
	}
	d.At, d.Location, d.Reading.Confidence = now.Add(10*time.Second), loc, 0.95
	second, _ := s.Ingest(d, settings, frame)
	if second.New || second.Record.Sightings != 2 || second.Record.Location == nil || second.Record.Reading.Confidence != 0.95 {
		t.Fatalf("merge = %+v", second)
	}
	if fetches != 1 {
		t.Fatalf("image fetched %d times", fetches)
	}
	if _, e := s.ImagePath(first.Record.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ImagePath("../x"); e == nil {
		t.Fatal("traversal id accepted")
	}
	d.At = now.Add(2 * time.Minute)
	third, _ := s.Ingest(d, settings, frame)
	if !third.New {
		t.Fatal("record after merge window must be new")
	}
	records, truncated, e := s.List(Query{From: "2026-11-12", To: "2026-11-12"})
	if e != nil || truncated || len(records) != 2 || records[0].ID != third.Record.ID {
		t.Fatalf("list = %+v %v %v", records, truncated, e)
	}
	if records[1].Sightings != 2 {
		t.Fatalf("closed record lost merge: %+v", records[1])
	}
}

func TestIngestSkipsUnreadableAndLowConfidence(t *testing.T) {
	now := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	s := newTestStore(t, &now)
	settings := validSettings()
	if o, _ := s.Ingest(Detection{At: now, Reading: Reading{Region: "品川", Confidence: 0.99}}, settings, nil); o.Skipped != "unreadable" {
		t.Fatalf("unreadable = %+v", o)
	}
	if o, _ := s.Ingest(Detection{At: now, Reading: Reading{Number: "1", Confidence: 0.2}}, settings, nil); o.Skipped != "low-confidence" {
		t.Fatalf("low = %+v", o)
	}
}

func TestImageFailureIsRecordedNotFaked(t *testing.T) {
	now := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	s := newTestStore(t, &now)
	settings, _ := ValidateSettings(validSettings(Rule{ID: "r1", Name: "x", Enabled: true, Number: "1", SaveImage: true}))
	o, _ := s.Ingest(Detection{At: now, Reading: Reading{Number: "1", Confidence: 0.9}, Frame: "gone"}, settings, func(string) ([]byte, error) { return nil, errors.New("expired") })
	if o.Record.ImageID != "" || o.Record.ImageNote == "" {
		t.Fatalf("image failure = %+v", o.Record)
	}
}

func TestListFiltersAndTornLines(t *testing.T) {
	now := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	s := newTestStore(t, &now)
	settings, _ := ValidateSettings(validSettings(Rule{ID: "r1", Name: "x", Enabled: true, Number: "77"}))
	for i, n := range []string{"77", "1234"} {
		_, _ = s.Ingest(Detection{At: now.Add(time.Duration(i) * time.Second), Reading: Reading{Kana: "わ", Number: n, Confidence: 0.9}}, settings, nil)
	}
	_ = s.Flush()
	f, _ := os.OpenFile(filepath.Join(s.recordsDir(), "2026-11-12.jsonl"), os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString(`{"id":"ptorn`)
	f.Close()
	matched, _, e := s.List(Query{From: "2026-11-12", To: "2026-11-12", MatchedOnly: true})
	if e != nil || len(matched) != 1 || matched[0].Reading.Number != "77" {
		t.Fatalf("matched = %+v %v", matched, e)
	}
	text, _, _ := s.List(Query{From: "2026-11-12", To: "2026-11-12", Text: "ワ１２"})
	if len(text) != 1 || text[0].Reading.Number != "1234" {
		t.Fatalf("text = %+v", text)
	}
	if _, _, e := s.List(Query{From: "2026-11-13", To: "2026-11-12"}); e == nil {
		t.Fatal("reversed range accepted")
	}
}

func TestPruneRetentionAndQuota(t *testing.T) {
	now := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	s := newTestStore(t, &now)
	os.MkdirAll(s.recordsDir(), 0700)
	os.MkdirAll(s.imagesDir(), 0700)
	os.WriteFile(filepath.Join(s.recordsDir(), "2025-01-01.jsonl"), []byte("{}\n"), 0600)
	os.WriteFile(filepath.Join(s.recordsDir(), "2026-11-11.jsonl"), []byte("{}\n"), 0600)
	big := []byte(strings.Repeat("x", 10<<20))
	for i, name := range []string{"pold-1.jpg", "pmid-1.jpg", "pnew-1.jpg"} {
		p := filepath.Join(s.imagesDir(), name)
		os.WriteFile(p, big, 0600)
		at := now.Add(time.Duration(i-3) * time.Hour)
		os.Chtimes(p, at, at)
	}
	settings := DefaultSettings()
	settings.RetentionDays = 30
	settings.ImageQuotaMB = 16
	if _, e := s.Prune(settings); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(s.recordsDir(), "2025-01-01.jsonl")); !os.IsNotExist(e) {
		t.Fatal("old day kept")
	}
	if _, e := os.Stat(filepath.Join(s.recordsDir(), "2026-11-11.jsonl")); e != nil {
		t.Fatal("recent day removed")
	}
	u := s.Usage()
	if u.ImageCount != 1 {
		t.Fatalf("quota kept %d images", u.ImageCount)
	}
	if _, e := os.Stat(filepath.Join(s.imagesDir(), "pnew-1.jpg")); e != nil {
		t.Fatal("newest image removed")
	}
}

func TestPartialReadingsMergeAndLateRuleMatch(t *testing.T) {
	now := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	s := newTestStore(t, &now)
	settings, _ := ValidateSettings(validSettings(Rule{ID: "r1", Name: "千葉", Enabled: true, Region: "千葉", SaveImage: true, Speak: true}))
	frames := 0
	frame := func(string) ([]byte, error) { frames++; return jpeg, nil }
	first, _ := s.Ingest(Detection{At: now, Reading: Reading{Number: "5501", Confidence: 0.8}, Frame: "f1"}, settings, frame)
	if !first.New || len(first.NewMatches) != 0 {
		t.Fatalf("first = %+v", first)
	}
	later, _ := s.Ingest(Detection{At: now.Add(5 * time.Second), Reading: Reading{Region: "千葉", Kana: "か", Number: "5501", Confidence: 0.9}, Frame: "f2"}, settings, frame)
	if later.New || later.Record.ID != first.Record.ID || later.Record.Reading.Region != "千葉" || later.Record.Display != "千葉 か 55-01" {
		t.Fatalf("partial readings not merged: %+v", later.Record)
	}
	if len(later.NewMatches) != 1 || later.Record.ImageID == "" || frames != 1 {
		t.Fatalf("late match = %+v frames=%d", later, frames)
	}
	again, _ := s.Ingest(Detection{At: now.Add(8 * time.Second), Reading: Reading{Region: "千葉", Number: "5501", Confidence: 0.9}, Frame: "f3"}, settings, frame)
	if len(again.NewMatches) != 0 || frames != 1 || again.Record.Sightings != 3 {
		t.Fatalf("repeated match = %+v frames=%d", again, frames)
	}
	other, _ := s.Ingest(Detection{At: now.Add(9 * time.Second), Reading: Reading{Region: "横浜", Number: "5501", Confidence: 0.9}}, settings, frame)
	if !other.New {
		t.Fatal("a different region must not merge into the same vehicle")
	}
}

func TestMinSightingsDropsSingleFrameReads(t *testing.T) {
	now := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	s := newTestStore(t, &now)
	settings, _ := ValidateSettings(Settings{RetentionDays: 30, ImageQuotaMB: 64, MergeSeconds: 30, MinConfidence: 0.6, Rules: []Rule{{ID: "r1", Name: "x", Enabled: true, Number: "1234", SaveImage: true}}})
	if settings.MinSightings != 2 {
		t.Fatalf("missing minSightings must default to 2, got %d", settings.MinSightings)
	}
	frames := 0
	frame := func(string) ([]byte, error) { frames++; return jpeg, nil }
	if o, _ := s.Ingest(Detection{At: now, Reading: Reading{Number: "1234", Confidence: 0.9}, Frame: "f1"}, settings, frame); o.Skipped != "provisional" || frames != 0 {
		t.Fatalf("first sighting = %+v", o)
	}
	// A one-off misread expires without ever becoming a record.
	s.Ingest(Detection{At: now.Add(time.Second), Reading: Reading{Number: "9999", Confidence: 0.9}}, settings, frame)
	o, _ := s.Ingest(Detection{At: now.Add(2 * time.Second), Reading: Reading{Kana: "わ", Number: "1234", Confidence: 0.8}, Frame: "f2"}, settings, frame)
	if !o.New || o.Record.Sightings != 2 || o.Record.FirstSeen != now || len(o.NewMatches) != 1 || o.Record.ImageID == "" || o.Record.Reading.Kana != "わ" {
		t.Fatalf("confirmed = %+v", o)
	}
	now = now.Add(time.Minute)
	records, _, _ := s.List(Query{From: "2026-11-12", To: "2026-11-12"})
	if len(records) != 1 || records[0].Reading.Number != "1234" {
		t.Fatalf("records = %+v", records)
	}
}
