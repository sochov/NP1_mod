package plate

import (
	"os"
	"testing"
	"time"
)

func leadDetection(id string, at time.Time, elapsed time.Duration, hits int, reading Reading) Detection {
	return Detection{At: at, Reading: reading, Box: [4]int{980, 480, 75, 40}, Frame: id,
		Track: &TrackSpan{ID: id, Frames: hits, Support: 2, FieldsChecked: true, LastSeen: at.Add(elapsed)}}
}

func TestRegisterFollowingAcrossMidnightAndRestart(t *testing.T) {
	dir := t.TempDir()
	images := t.TempDir()
	store := NewStoreWithExternalImages(dir, images)
	settings := DefaultSettings()
	settings.Rules = []Rule{{ID: "both", Enabled: true, Region: "横浜", Kana: "ら", Number: "1234", SaveImage: true}}
	first := time.Date(2026, 11, 26, 23, 59, 45, 0, jst)
	a := leadDetection("pt-session-1-0", first, 10*time.Second, 35, Reading{Region: "横浜", Number: "1234", Confidence: .92})
	o, e := store.Ingest(a, settings, nil)
	if e != nil || !o.New || len(o.NewMatches) != 0 {
		t.Fatalf("first: %+v %v", o, e)
	}
	store = NewStoreWithExternalImages(dir, images)
	b := leadDetection("pt-session-40-0", first.Add(80*time.Second), 3*time.Second, 8, Reading{Kana: "ら", Number: "1234", Confidence: .87})
	image := func(id string) ([]byte, error) {
		if id != b.Frame {
			t.Fatalf("unexpected frame %s", id)
		}
		return []byte{0xff, 0xd8, 0xff, 0xd9}, nil
	}
	o, e = store.Ingest(b, settings, image)
	if e != nil || o.New || o.Record.ID != a.Track.ID || o.Record.Sightings != 43 || len(o.NewMatches) != 1 || o.Record.ImageID != b.Track.ID ||
		o.Record.Reading.Region != "横浜" || o.Record.Reading.Kana != "ら" || !o.Record.FirstSeen.Equal(a.At) || !o.Record.LastSeen.Equal(b.Track.LastSeen) {
		t.Fatalf("returned: %+v %v", o, e)
	}
	if _, e := store.ImagePath(o.Record.ImageID); e != nil {
		t.Fatal(e)
	}
	if e := store.SetSpeech(o.Record.ID, SpeechSpoken); e != nil {
		t.Fatal(e)
	}
	loc := Location{At: a.At, Lat: 35, Lon: 139}
	if e := store.SetLocation(o.Record.ID, a.At, loc); e != nil {
		t.Fatal(e)
	}
	// No new image or new notification on a cumulative update after restart.
	store = NewStoreWithExternalImages(dir, images)
	b.Track.Frames += 5
	b.Track.LastSeen = b.Track.LastSeen.Add(time.Second)
	b.Track.Update = true
	o, e = store.Ingest(b, settings, nil)
	if e != nil || o.New || o.Record.Sightings != 48 || len(o.NewMatches) != 0 || o.Record.Speech != SpeechSpoken || o.Record.Location == nil {
		t.Fatalf("update: %+v %v", o, e)
	}
	if o, e := store.Ingest(b, settings, nil); e != nil || o.Skipped != "stale-track" {
		t.Fatalf("duplicate: %+v %v", o, e)
	}
	store = NewStoreWithExternalImages(dir, images)
	for _, q := range []Query{{From: Day(first), To: Day(first), Text: "ら1234", MatchedOnly: true}, {From: Day(first), To: Day(b.At)}} {
		rows, e := store.ListAll(q)
		if e != nil || len(rows) != 1 || rows[0].ID != a.Track.ID || rows[0].Sightings != 48 || rows[0].ImageID != b.Track.ID {
			t.Fatalf("list %+v: %+v %v", q, rows, e)
		}
	}
	if rows, e := store.ListAll(Query{From: Day(b.At), To: Day(b.At)}); e != nil || len(rows) != 0 {
		t.Fatalf("continued encounter appears the next day: %+v %v", rows, e)
	}
}

func TestRegisteredEncounterSeparation(t *testing.T) {
	at := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		name     string
		adjust   func(a, b *Detection)
		wantJoin bool
	}{
		{"brief lane change", func(a, b *Detection) { b.Box[0] -= 440 }, true},
		{"overlapping tracks", func(a, b *Detection) { b.At = a.At.Add(time.Second) }, false},
		{"fifteen minutes later", func(a, b *Detection) {
			b.At = a.Track.LastSeen.Add(15 * time.Minute)
			b.Track.LastSeen = b.At.Add(2 * time.Second)
		}, false},
		{"confirmed region differs", func(a, b *Detection) { a.Reading.Region = "札幌"; b.Reading.Region = "横浜" }, false},
		{"unusable representative box", func(a, b *Detection) { b.Box[3] = 0 }, false},
		{"large lateral jump with a short number", func(a, b *Detection) { a.Reading.Number = "531"; b.Reading.Number = "531"; b.Box[0] -= 420 }, false},
		{"two different sizes", func(a, b *Detection) { b.Box[2] *= 3 }, false},
		{"same serial on another row", func(a, b *Detection) { b.Box[1] += 150 }, false},
		{"one new observation", func(a, b *Detection) { b.Track.Frames = 1; b.Track.Support = 1 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewStore(t.TempDir())
			settings := DefaultSettings()
			a := leadDetection("pt-run-1-0", at, 10*time.Second, 30, Reading{Number: "1234", Confidence: .98})
			b := leadDetection("pt-run-50-0", a.Track.LastSeen.Add(15*time.Second), 2*time.Second, 5, Reading{Number: "1234", Confidence: .94})
			tc.adjust(&a, &b)
			for _, d := range []Detection{a, b} {
				if o, e := s.Ingest(d, settings, nil); e != nil || o.Skipped != "" {
					t.Fatalf("%+v %v", o, e)
				}
			}
			rows, e := s.ListAll(Query{From: Day(at), To: Day(at)})
			if e != nil || (len(rows) == 1) != tc.wantJoin {
				t.Fatalf("%+v %v", rows, e)
			}
		})
	}
}

func TestRegisteredEncounterDoesNotChooseBetweenTwoCars(t *testing.T) {
	s := NewStore(t.TempDir())
	at := time.Now()
	settings := DefaultSettings()
	a := leadDetection("pt-run-10-0", at, 12*time.Second, 24, Reading{Number: "1234", Confidence: .97})
	b := leadDetection("pt-run-10-1", at, 12*time.Second, 24, a.Reading)
	b.Box[0] += 100
	c := leadDetection("pt-run-45-0", a.Track.LastSeen.Add(3*time.Second), time.Second, 6, a.Reading)
	for _, d := range []Detection{a, b, c} {
		if o, e := s.Ingest(d, settings, nil); e != nil || !o.New {
			t.Fatalf("%+v %v", o, e)
		}
	}
}

func TestRegisteredEncounterJoinsFastLateralFragments(t *testing.T) {
	s := NewStore(t.TempDir())
	at := time.Date(2026, 11, 27, 14, 29, 41, 722000000, time.UTC)
	settings := DefaultSettings()
	boxes := [][4]int{{235, 663, 47, 42}, {274, 655, 47, 36}, {339, 637, 50, 34}, {366, 630, 47, 32}, {395, 627, 42, 27}, {458, 610, 42, 25}}
	for i, box := range boxes {
		d := leadDetection("pt-fast-"+string(rune('a'+i)), at.Add(time.Duration(i)*125*time.Millisecond), 0, 1,
			Reading{Number: "8692", Confidence: .95})
		d.Track.Support = 1
		d.Track.InstantVerified = true
		d.Box = box
		if o, err := s.Ingest(d, settings, nil); err != nil || o.Skipped != "" {
			t.Fatalf("fragment %d: %+v %v", i, o, err)
		}
	}
	lead := leadDetection("pt-fast-lead", at.Add(750*time.Millisecond), 14*time.Second, 98,
		Reading{Number: "8692", Confidence: .89})
	lead.Box = [4]int{478, 605, 40, 24}
	if o, err := s.Ingest(lead, settings, nil); err != nil || o.Skipped != "" || o.New {
		t.Fatalf("lead: %+v %v", o, err)
	}
	rows, err := s.ListAll(Query{From: Day(at), To: Day(at)})
	if err != nil || len(rows) != 1 || rows[0].Sightings != 104 || !rows[0].FirstSeen.Equal(at) || !rows[0].LastSeen.Equal(lead.Track.LastSeen) {
		t.Fatalf("rows: %+v %v", rows, err)
	}
}

func TestFastFragmentsRequireContinuity(t *testing.T) {
	at := time.Now()
	base := Record{ID: "pt-a", FirstSeen: at, LastSeen: at, Sightings: 1,
		Reading: Reading{Number: "8692", Confidence: .94}, Box: [4]int{300, 620, 48, 32}}
	for _, tc := range []struct {
		name   string
		adjust func(*Record, *Record)
	}{
		{"simultaneous", func(_, next *Record) { next.FirstSeen = at; next.LastSeen = at }},
		{"too late", func(_, next *Record) { next.FirstSeen = at.Add(time.Second); next.LastSeen = next.FirstSeen }},
		{"different row", func(_, next *Record) { next.Box[1] += 120 }},
		{"impossible jump", func(_, next *Record) { next.Box[0] += 500 }},
		{"different region", func(prior, next *Record) { prior.Reading.Region = "横浜"; next.Reading.Region = "品川" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prior := base
			next := base
			next.ID = "pt-b"
			next.FirstSeen = at.Add(125 * time.Millisecond)
			next.LastSeen = next.FirstSeen
			next.Box[0] += 40
			tc.adjust(&prior, &next)
			if followsFastFragment(prior, next) {
				t.Fatal("unsafe fragments joined")
			}
		})
	}
}

func TestRepairFastFragmentEncountersIsExplicitAndIdempotent(t *testing.T) {
	at := time.Now()
	records := map[string]Record{
		"pt-root": {ID: "pt-root", FirstSeen: at, LastSeen: at, Sightings: 1,
			Reading: Reading{Number: "8692", Confidence: .95}, Box: [4]int{300, 620, 48, 32}},
		"pt-next": {ID: "pt-next", FirstSeen: at.Add(125 * time.Millisecond), LastSeen: at.Add(125 * time.Millisecond), Sightings: 1,
			Reading: Reading{Number: "8692", Confidence: .94}, Box: [4]int{340, 615, 47, 31}},
		"pt-other": {ID: "pt-other", FirstSeen: at.Add(250 * time.Millisecond), LastSeen: at.Add(250 * time.Millisecond), Sightings: 1,
			Reading: Reading{Number: "1234", Confidence: .96}, Box: [4]int{380, 610, 47, 31}},
	}
	changed := repairFastFragmentEncounters(records)
	if len(changed) != 1 || records["pt-next"].EncounterID != "pt-root" || records["pt-other"].EncounterID != "" {
		t.Fatalf("changed=%+v records=%+v", changed, records)
	}
	if changed = repairFastFragmentEncounters(records); len(changed) != 0 {
		t.Fatalf("second repair changed %+v", changed)
	}
}

func TestRegisteredEncounterHandlesCorrectionsAndAppendRetry(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	at := time.Now()
	cfg := DefaultSettings()
	a := leadDetection("pt-run-10-0", at, 12*time.Second, 24, Reading{Number: "1234", Region: "札幌", Confidence: .97})
	b := leadDetection("pt-run-32-0", a.Track.LastSeen.Add(5*time.Second), time.Second, 6, Reading{Number: "1234", Confidence: .91})
	if _, e := s.Ingest(a, cfg, nil); e != nil {
		t.Fatal(e)
	}
	journal := s.recordsDir()
	if e := os.Rename(journal, journal+".bak"); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(journal, []byte("unavailable"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Ingest(b, cfg, nil); e == nil {
		t.Fatal("storage failure accepted")
	}
	if e := os.Remove(journal); e != nil {
		t.Fatal(e)
	}
	if e := os.Rename(journal+".bak", journal); e != nil {
		t.Fatal(e)
	}
	if o, e := s.Ingest(b, cfg, nil); e != nil || o.New || o.Record.Sightings != 30 {
		t.Fatalf("retry %+v %v", o, e)
	}
	// The next independent update yields two incompatible confirmed regions.
	b.Track.Frames++
	b.Track.LastSeen = b.Track.LastSeen.Add(time.Second)
	b.Reading.Region = "横浜"
	if o, e := s.Ingest(b, cfg, nil); e != nil || o.Record.ID != b.Track.ID {
		t.Fatalf("conflict %+v %v", o, e)
	}
	rows, e := NewStore(dir).ListAll(Query{From: Day(at), To: Day(at)})
	if e != nil || len(rows) != 2 || rows[0].Sightings != 7 || rows[1].Sightings != 24 {
		t.Fatalf("%+v %v", rows, e)
	}
}
func TestRegisteredEncounterCanJoinAfterStrongerUpdate(t *testing.T) {
	s := NewStore(t.TempDir())
	at := time.Now()
	settings := DefaultSettings()
	a := leadDetection("pt-old-10-0", at, 12*time.Second, 24, Reading{Number: "1234", Confidence: .97})
	b := leadDetection("pt-new-45-0", a.Track.LastSeen.Add(5*time.Second), time.Second, 1, a.Reading)
	b.Track.Support = 1
	for _, d := range []Detection{a, b} {
		if o, e := s.Ingest(d, settings, nil); e != nil || !o.New {
			t.Fatalf("%+v %v", o, e)
		}
	}
	b.Track.Frames = 5
	b.Track.Support = 3
	b.Track.LastSeen = b.Track.LastSeen.Add(time.Second)
	o, e := s.Ingest(b, settings, nil)
	if e != nil || o.New || o.Record.ID != a.Track.ID || o.Record.Sightings != 29 {
		t.Fatalf("rejoin %+v %v", o, e)
	}
	// A later correction of the root may split its children; even if a
	// preceding candidate exists, a root with children cannot become a child.
	a.Track.Frames++
	a.Track.LastSeen = a.Track.LastSeen.Add(time.Second)
	if _, e = s.Ingest(a, settings, nil); e != nil {
		t.Fatal(e)
	}
	if s.open[a.Track.ID].EncounterID != "" || s.open[b.Track.ID].EncounterID != a.Track.ID {
		t.Fatal("root identity changed")
	}
}
func TestOrphanedEncounterMemberRemainsVisible(t *testing.T) {
	s := NewStore(t.TempDir())
	at := time.Now()
	r := Record{ID: "pt-child-1-0", EncounterID: "pt-pruned-1-0", FirstSeen: at, LastSeen: at, Sightings: 3, Reading: Reading{Number: "1234"}}
	if e := s.appendRecord(r); e != nil {
		t.Fatal(e)
	}
	rows, e := s.ListAll(Query{From: Day(at), To: Day(at)})
	if e != nil || len(rows) != 1 || rows[0].ID != r.ID {
		t.Fatalf("orphan %+v %v", rows, e)
	}
}
func TestShortSerialNeedsTwoIndependentNeighborsAcrossGap(t *testing.T) {
	at := time.Now()
	a := Record{ID: "pt-run-22-0", FirstSeen: at, LastSeen: at.Add(time.Second), Sightings: 3, Box: [4]int{780, 550, 65, 35}, Reading: Reading{Number: "35", Confidence: .88}}
	b := a
	b.ID = "pt-run-80-0"
	b.FirstSeen = a.LastSeen.Add(8 * time.Second)
	b.LastSeen = b.FirstSeen.Add(time.Second)
	b.Box[0] = 360
	host := Record{ID: "pt-run-21-1", FirstSeen: at, LastSeen: b.LastSeen, Sightings: 18, Box: [4]int{1050, 555, 60, 35}, Reading: Reading{Number: "5821", Confidence: .94}}
	neighbor := host
	neighbor.ID = "pt-run-27-2"
	neighbor.Reading.Number = "7256"
	neighbor.Box[0] = 1250
	neighbor.FirstSeen = at.Add(4 * time.Second)
	for _, tc := range []struct {
		name   string
		modify func(a, b, h, n *Record)
		want   bool
	}{
		{"stable neighbors", func(a, b, h, n *Record) {}, true},
		{"neighbor tracked twice", func(a, b, h, n *Record) { n.Reading.Number = h.Reading.Number }, false},
		{"no neighbor spans the gap", func(a, b, h, n *Record) { h.LastSeen = b.FirstSeen.Add(-time.Second) }, false},
		{"crossed the neighbor", func(a, b, h, n *Record) { b.Box[0] = 1120 }, false},
		{"other lane", func(a, b, h, n *Record) { n.Box[1] += 130 }, false},
		{"inconsistent metadata", func(a, b, h, n *Record) { a.Reading.Region = "秋田"; b.Reading.Region = "山形" }, false},
		{"weak short number", func(a, b, h, n *Record) { b.Reading.Confidence = .7 }, false},
		{"same frame", func(a, b, h, n *Record) { b.FirstSeen = a.LastSeen }, false},
		{"too long", func(a, b, h, n *Record) { b.FirstSeen = a.LastSeen.Add(30 * time.Second); h.LastSeen = b.FirstSeen }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b, h, n := a, b, host, neighbor
			tc.modify(&a, &b, &h, &n)
			s := NewStore(t.TempDir())
			s.open[h.ID] = &h
			s.open[n.ID] = &n
			if got := s.followsInVehicleRow(a, b); got != tc.want {
				t.Fatalf("join=%v", got)
			}
		})
	}
}
