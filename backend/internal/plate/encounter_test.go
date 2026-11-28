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
		{"overlapping duplicate track", func(a, b *Detection) { b.At = a.At.Add(time.Second) }, true},
		{"fifteen minutes later", func(a, b *Detection) {
			b.At = a.Track.LastSeen.Add(15 * time.Minute)
			b.Track.LastSeen = b.At.Add(2 * time.Second)
		}, false},
		{"confirmed region differs", func(a, b *Detection) { a.Reading.Region = "札幌"; b.Reading.Region = "横浜" }, false},
		{"unusable representative box", func(a, b *Detection) { b.Box[3] = 0 }, true},
		{"large lateral jump with a short number", func(a, b *Detection) { a.Reading.Number = "531"; b.Reading.Number = "531"; b.Box[0] -= 420 }, true},
		{"two different sizes", func(a, b *Detection) { b.Box[2] *= 3 }, true},
		{"same serial on another row after a gap", func(a, b *Detection) { b.Box[1] += 150 }, true},
		{"one new observation", func(a, b *Detection) { b.Track.Frames = 1; b.Track.Support = 1 }, true},
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

func TestSameNumberRejectsStrongContradictions(t *testing.T) {
	at := time.Now()
	base := Record{ID: "pt-a", FirstSeen: at, LastSeen: at, Sightings: 1,
		Reading: Reading{Number: "8692", Confidence: .94}, Box: [4]int{300, 620, 48, 32}}
	for _, tc := range []struct {
		name   string
		adjust func(*Record, *Record)
	}{
		{"simultaneous separate boxes", func(_, next *Record) { next.FirstSeen = at; next.LastSeen = at; next.Box[0] += 120 }},
		{"too late", func(_, next *Record) {
			next.FirstSeen = at.Add(10*time.Minute + time.Second)
			next.LastSeen = next.FirstSeen
		}},
		{"different row", func(_, next *Record) { next.Box[1] += 240 }},
		{"unusable box", func(_, next *Record) { next.Box[2] = 0 }},
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
			if followsSameNumber(prior, next) {
				t.Fatal("unsafe fragments joined")
			}
		})
	}
}

func TestRepairFragmentedEncountersIsExplicitAndIdempotent(t *testing.T) {
	at := time.Now()
	records := map[string]Record{
		"pt-root": {ID: "pt-root", FirstSeen: at, LastSeen: at, Sightings: 1,
			Reading: Reading{Number: "8692", Confidence: .95}, Box: [4]int{300, 620, 48, 32}},
		"pt-next": {ID: "pt-next", FirstSeen: at.Add(125 * time.Millisecond), LastSeen: at.Add(125 * time.Millisecond), Sightings: 1,
			Reading: Reading{Number: "8692", Confidence: .94}, Box: [4]int{340, 615, 47, 31}},
		"pt-other": {ID: "pt-other", FirstSeen: at.Add(250 * time.Millisecond), LastSeen: at.Add(250 * time.Millisecond), Sightings: 1,
			Reading: Reading{Number: "1234", Confidence: .96}, Box: [4]int{380, 610, 47, 31}},
	}
	changed := repairFragmentedEncounters(records)
	if len(changed) != 1 || records["pt-next"].EncounterID != "pt-root" || records["pt-other"].EncounterID != "" {
		t.Fatalf("changed=%+v records=%+v", changed, records)
	}
	if changed = repairFragmentedEncounters(records); len(changed) != 0 {
		t.Fatalf("second repair changed %+v", changed)
	}
}

func TestRepairFragmentedEncountersFlattensExistingChain(t *testing.T) {
	at := time.Now()
	records := map[string]Record{
		"pt-root": {ID: "pt-root", FirstSeen: at, LastSeen: at.Add(time.Second), Sightings: 11,
			Reading: Reading{Number: "1870", Confidence: .94}, Box: [4]int{480, 591, 45, 24}},
		"pt-child": {ID: "pt-child", FirstSeen: at.Add(1100 * time.Millisecond), LastSeen: at.Add(1100 * time.Millisecond), Sightings: 1,
			Reading: Reading{Number: "1870", Confidence: .96}, Box: [4]int{221, 609, 50, 38}},
		"pt-grandchild": {ID: "pt-grandchild", EncounterID: "pt-child", FirstSeen: at.Add(1200 * time.Millisecond), LastSeen: at.Add(1200 * time.Millisecond), Sightings: 1,
			Reading: Reading{Number: "1870", Confidence: .95}, Box: [4]int{210, 610, 50, 38}},
	}
	changed := repairFragmentedEncounters(records)
	if len(changed) != 2 || records["pt-child"].EncounterID != "pt-root" || records["pt-grandchild"].EncounterID != "pt-root" {
		t.Fatalf("changed=%+v records=%+v", changed, records)
	}
	rows := projectEncounters(records)
	if len(rows) != 1 || rows[0].Sightings != 13 || !rows[0].LastSeen.Equal(at.Add(1200*time.Millisecond)) {
		t.Fatalf("rows=%+v", rows)
	}
	if changed = repairFragmentedEncounters(records); len(changed) != 0 {
		t.Fatalf("second repair changed %+v", changed)
	}
}

func TestRepairFragmentedEncountersRetriesAfterFlattening(t *testing.T) {
	at := time.Now()
	records := map[string]Record{
		"pt-a": {ID: "pt-a", FirstSeen: at, LastSeen: at, Sightings: 1,
			Reading: Reading{Number: "3113", Confidence: .89}, Box: [4]int{1731, 771, 61, 47}},
		"pt-b": {ID: "pt-b", EncounterID: "pt-a", FirstSeen: at.Add(632 * time.Millisecond), LastSeen: at.Add(632 * time.Millisecond), Sightings: 1,
			Reading: Reading{Number: "3113", Confidence: .88}, Box: [4]int{1510, 732, 64, 44}},
		"pt-c": {ID: "pt-c", EncounterID: "pt-b", FirstSeen: at.Add(1008 * time.Millisecond), LastSeen: at.Add(1008 * time.Millisecond), Sightings: 1,
			Reading: Reading{Number: "3113", Confidence: .87}, Box: [4]int{1396, 714, 61, 36}},
		"pt-d": {ID: "pt-d", FirstSeen: at.Add(1134 * time.Millisecond), LastSeen: at.Add(5 * time.Second), Sightings: 37,
			Reading: Reading{Number: "3113", Confidence: .91}, Box: [4]int{1171, 674, 54, 31}},
	}
	changed := repairFragmentedEncounters(records)
	if len(changed) != 2 || records["pt-c"].EncounterID != "pt-a" || records["pt-d"].EncounterID != "pt-a" {
		t.Fatalf("changed=%+v records=%+v", changed, records)
	}
	rows := projectEncounters(records)
	if len(rows) != 1 || rows[0].Sightings != 40 {
		t.Fatalf("rows=%+v", rows)
	}
}

func TestSameOneDigitReacquisitionUsesHighConfidence(t *testing.T) {
	at := time.Now()
	a := Record{ID: "pt-a", FirstSeen: at, LastSeen: at.Add(29 * time.Second), Sightings: 227,
		Reading: Reading{Number: "1", Confidence: .946}, Box: [4]int{500, 600, 48, 28}}
	b := Record{ID: "pt-b", FirstSeen: at.Add(126 * time.Second), LastSeen: at.Add(153 * time.Second), Sightings: 213,
		Reading: Reading{Number: "1", Confidence: .953}, Box: [4]int{700, 610, 45, 26}}
	if !followsSameNumber(a, b) {
		t.Fatal("high-confidence one-digit reacquisition was not joined")
	}
	b.Reading.Confidence = .93
	a.Reading.Confidence = .93
	if followsSameNumber(a, b) {
		t.Fatal("weak one-digit reacquisition was joined")
	}
}

func TestRepairFragmentedEncountersJoinsCloseOverlappingDuplicate(t *testing.T) {
	at := time.Now()
	records := map[string]Record{
		"pt-long": {ID: "pt-long", FirstSeen: at, LastSeen: at.Add(38 * time.Second), Sightings: 168,
			Reading: Reading{Number: "5140", Confidence: .94}, Box: [4]int{1341, 624, 52, 28}},
		"pt-duplicate": {ID: "pt-duplicate", FirstSeen: at.Add(18 * time.Second), LastSeen: at.Add(19 * time.Second), Sightings: 6,
			Reading: Reading{Number: "5140", Confidence: .926}, Box: [4]int{1317, 624, 48, 27}},
	}
	if changed := repairFragmentedEncounters(records); len(changed) != 1 || records["pt-duplicate"].EncounterID != "pt-long" {
		t.Fatalf("changed=%+v records=%+v", changed, records)
	}
	rows := projectEncounters(records)
	if len(rows) != 1 || rows[0].Sightings != 168 || !rows[0].LastSeen.Equal(at.Add(38*time.Second)) {
		t.Fatalf("rows=%+v", rows)
	}
}

func TestRegisteredEncounterFollowsMovingChildrenInsideLongRoot(t *testing.T) {
	s := NewStore(t.TempDir())
	at := time.Now().Add(time.Second)
	settings := DefaultSettings()
	settings.MinSightings = 1
	root := leadDetection("pt-long-root", at, 38*time.Second, 168, Reading{Number: "5140", Confidence: .94})
	root.Box = [4]int{1341, 624, 52, 28}
	first := leadDetection("pt-moving-a", at.Add(18*time.Second), 0, 1, Reading{Number: "5140", Confidence: .926})
	first.Track.Support = 1
	first.Box = [4]int{1317, 624, 48, 27}
	second := leadDetection("pt-moving-b", at.Add(18125*time.Millisecond), 0, 1, Reading{Number: "5140", Confidence: .90})
	second.Track.Support = 1
	second.Box = [4]int{1185, 604, 44, 24}
	for _, detection := range []Detection{root, first, second} {
		if outcome, err := s.Ingest(detection, settings, nil); err != nil || outcome.Skipped != "" {
			t.Fatalf("outcome=%+v err=%v", outcome, err)
		}
	}
	if s.open[first.Track.ID].EncounterID != root.Track.ID || s.open[second.Track.ID].EncounterID != root.Track.ID {
		t.Fatalf("first=%+v second=%+v", s.open[first.Track.ID], s.open[second.Track.ID])
	}
	rows, err := s.ListAll(Query{From: Day(at), To: Day(at)})
	if err != nil || len(rows) != 1 || rows[0].Sightings != 168 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

func TestSameNumberJoinsWithoutFragileHorizontalGeometry(t *testing.T) {
	at := time.Now()
	prior := Record{ID: "pt-a", FirstSeen: at, LastSeen: at, Sightings: 1,
		Reading: Reading{Number: "9873", Confidence: .88}, Box: [4]int{1421, 619, 46, 28}}
	next := Record{ID: "pt-b", FirstSeen: at.Add(130 * time.Millisecond), LastSeen: at.Add(130 * time.Millisecond), Sightings: 1,
		Reading: Reading{Number: "9873", Confidence: .95}, Box: [4]int{1556, 636, 50, 34}}
	if !followsSameNumber(prior, next) {
		t.Fatal("adjacent fragments were not joined")
	}
	prior.Reading.Number, next.Reading.Number = "267", "267"
	next.FirstSeen, next.LastSeen = at.Add(25*time.Second), at.Add(25*time.Second)
	if !followsSameNumber(prior, next) {
		t.Fatal("three-digit reacquisition was not joined")
	}
}

func TestRepairFragmentedEncountersJoinsObservedNovember28Breaks(t *testing.T) {
	day := time.Date(2026, 11, 28, 5, 0, 0, 0, time.UTC)
	records := map[string]Record{}
	add := func(id, number string, first, last time.Duration, sightings int, confidence float64, box [4]int) {
		records[id] = Record{ID: id, FirstSeen: day.Add(first), LastSeen: day.Add(last), Sightings: sightings,
			Reading: Reading{Number: number, Confidence: confidence}, Box: box}
	}
	add("pt-37-a", "37", 7*time.Minute+52*time.Second, 7*time.Minute+52*time.Second, 1, .8595, [4]int{126, 582, 49, 47})
	add("pt-37-b", "37", 7*time.Minute+52*time.Second+127*time.Millisecond, 7*time.Minute+52*time.Second+127*time.Millisecond, 1, .9066, [4]int{150, 577, 62, 51})
	add("pt-37-c", "37", 7*time.Minute+52*time.Second+378*time.Millisecond, 8*time.Minute+24*time.Second+185*time.Millisecond, 252, .9264, [4]int{418, 567, 57, 30})
	add("pt-267-a", "267", 7*time.Minute+57*time.Second+534*time.Millisecond, 7*time.Minute+57*time.Second+786*time.Millisecond, 3, .9295, [4]int{1648, 645, 56, 43})
	add("pt-267-b", "267", 8*time.Minute+23*time.Second+415*time.Millisecond, 8*time.Minute+24*time.Second+185*time.Millisecond, 7, .9228, [4]int{1270, 593, 42, 23})
	add("pt-267-c", "267", 8*time.Minute+24*time.Second+311*time.Millisecond, 8*time.Minute+24*time.Second+437*time.Millisecond, 2, .9144, [4]int{1475, 625, 58, 34})
	add("pt-8383-a", "8383", 8*time.Minute+11*time.Second+706*time.Millisecond, 8*time.Minute+12*time.Second+488*time.Millisecond, 6, .9676, [4]int{1718, 772, 47, 45})
	add("pt-8383-b", "8383", 8*time.Minute+22*time.Second+11*time.Millisecond, 8*time.Minute+23*time.Second+534*time.Millisecond, 12, .9639, [4]int{1240, 649, 36, 21})
	add("pt-9873-a", "9873", 9*time.Minute+31*time.Second+184*time.Millisecond, 9*time.Minute+31*time.Second+184*time.Millisecond, 1, .8807, [4]int{1421, 619, 46, 28})
	add("pt-9873-b", "9873", 9*time.Minute+31*time.Second+314*time.Millisecond, 9*time.Minute+31*time.Second+314*time.Millisecond, 1, .9550, [4]int{1556, 636, 50, 34})
	add("pt-1870-a", "1870", 9*time.Minute+37*time.Second+111*time.Millisecond, 9*time.Minute+38*time.Second+907*time.Millisecond, 11, .9433, [4]int{480, 591, 45, 24})
	add("pt-1870-b", "1870", 9*time.Minute+39*time.Second+39*time.Millisecond, 9*time.Minute+39*time.Second+180*time.Millisecond, 2, .9622, [4]int{221, 609, 50, 38})
	add("pt-7241-a", "7241", 10*time.Minute+5*time.Second+84*time.Millisecond, 10*time.Minute+5*time.Second+341*time.Millisecond, 2, .9107, [4]int{147, 594, 40, 53})
	add("pt-7241-b", "7241", 10*time.Minute+51*time.Second+585*time.Millisecond, 11*time.Minute+1*time.Second+432*time.Millisecond, 74, .8965, [4]int{318, 589, 45, 28})

	changed := repairFragmentedEncounters(records)
	if len(changed) != 8 {
		t.Fatalf("changed=%d want=8", len(changed))
	}
	want := map[string]int{"37": 254, "267": 12, "8383": 18, "9873": 2, "1870": 13, "7241": 76}
	rows := projectEncounters(records)
	if len(rows) != len(want) {
		t.Fatalf("rows=%d want=%d: %+v", len(rows), len(want), rows)
	}
	for _, row := range rows {
		if row.Sightings != want[row.Reading.Number] {
			t.Errorf("number=%s sightings=%d want=%d", row.Reading.Number, row.Sightings, want[row.Reading.Number])
		}
	}
	if changed = repairFragmentedEncounters(records); len(changed) != 0 {
		t.Fatalf("second repair changed %+v", changed)
	}
}

func TestRepairFragmentedEncountersJoinsLaterNovember28Breaks(t *testing.T) {
	at := time.Date(2026, 11, 28, 7, 30, 0, 0, time.UTC)
	loc := func(lat, lon float64, offset time.Duration) *Location {
		return &Location{At: at.Add(offset), Lat: lat, Lon: lon}
	}
	records := map[string]Record{
		"pt-519-a": {ID: "pt-519-a", FirstSeen: at.Add(11*time.Minute + 40*time.Second), LastSeen: at.Add(12*time.Minute + 8*time.Second), Sightings: 210,
			Reading: Reading{Number: "519", Confidence: .806}, Box: [4]int{934, 606, 66, 34}, Location: loc(34.6946755, 135.1977095, 11*time.Minute+40*time.Second)},
		"pt-519-b": {ID: "pt-519-b", FirstSeen: at.Add(12*time.Minute + 19*time.Second), LastSeen: at.Add(13*time.Minute + 17*time.Second), Sightings: 440,
			Reading: Reading{Number: "519", Confidence: .825}, Box: [4]int{911, 606, 71, 34}, Location: loc(34.6935872, 135.1954738, 12*time.Minute+19*time.Second)},
		"pt-919-a": {ID: "pt-919-a", FirstSeen: at.Add(6*time.Minute + 180*time.Millisecond), LastSeen: at.Add(6*time.Minute + 815*time.Millisecond), Sightings: 2,
			Reading: Reading{Number: "919", Confidence: .913}, Box: [4]int{1378, 613, 50, 32}},
		"pt-919-b": {ID: "pt-919-b", FirstSeen: at.Add(6*time.Minute + 309*time.Millisecond), LastSeen: at.Add(6*time.Minute + 431*time.Millisecond), Sightings: 2,
			Reading: Reading{Number: "919", Confidence: .929}, Box: [4]int{1470, 629, 53, 36}},
		"pt-5903-a": {ID: "pt-5903-a", FirstSeen: at.Add(4*time.Minute + 28*time.Second), LastSeen: at.Add(9*time.Minute + 27*time.Second), Sightings: 1524,
			Reading: Reading{Region: "神戸", Class: "505", Kana: "ち", Number: "5903", Confidence: .964}, Box: [4]int{887, 591, 37, 21}, Location: loc(34.7012781, 135.213548, 4*time.Minute+28*time.Second)},
		"pt-5903-b": {ID: "pt-5903-b", FirstSeen: at.Add(12*time.Minute + 37*time.Second), LastSeen: at.Add(12*time.Minute + 37*time.Second + 115*time.Millisecond), Sightings: 2,
			Reading: Reading{Number: "5903", Confidence: .935}, Box: [4]int{249, 604, 52, 37}, Location: loc(34.6935505, 135.1951249, 12*time.Minute+37*time.Second)},
		"pt-7250-a": {ID: "pt-7250-a", FirstSeen: at.Add(3*time.Minute + 32*time.Second), LastSeen: at.Add(3*time.Minute + 32*time.Second + 257*time.Millisecond), Sightings: 2,
			Reading: Reading{Number: "7250", Confidence: .898}, Box: [4]int{1540, 650, 58, 34}, Location: loc(34.702705, 135.21928, 3*time.Minute+32*time.Second)},
		"pt-7250-b": {ID: "pt-7250-b", FirstSeen: at.Add(11*time.Minute + 52*time.Second), LastSeen: at.Add(12*time.Minute + 9*time.Second), Sightings: 135,
			Reading: Reading{Number: "7250", Confidence: .942}, Box: [4]int{309, 605, 52, 32}, Location: loc(34.694667, 135.1973822, 11*time.Minute+52*time.Second)},
	}

	changed := repairFragmentedEncounters(records)
	if len(changed) != 4 {
		t.Fatalf("changed=%d want=4: %+v", len(changed), changed)
	}
	rows := projectEncounters(records)
	if len(rows) != 4 {
		t.Fatalf("rows=%d want=4: %+v", len(rows), rows)
	}
	want := map[string]int{"519": 650, "919": 2, "5903": 1526, "7250": 137}
	for _, row := range rows {
		if row.Sightings != want[row.Reading.Number] {
			t.Errorf("number=%s sightings=%d want=%d", row.Reading.Number, row.Sightings, want[row.Reading.Number])
		}
	}
}

func TestSameNumberStillSeparatesAfterTenMinutes(t *testing.T) {
	at := time.Now()
	a := Record{ID: "pt-a", FirstSeen: at, LastSeen: at.Add(time.Minute), Sightings: 100,
		Reading: Reading{Number: "1234", Confidence: .97}, Box: [4]int{500, 600, 48, 28}}
	b := Record{ID: "pt-b", FirstSeen: at.Add(11*time.Minute + time.Second), LastSeen: at.Add(12*time.Minute + time.Second), Sightings: 100,
		Reading: Reading{Number: "1234", Confidence: .97}, Box: [4]int{700, 610, 45, 26}}
	if followsSameNumber(a, b) {
		t.Fatal("separate vehicle after ten minutes was joined")
	}
}

func TestRepairRecentRunsWithoutANewDetection(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 11, 28, 14, 10, 0, 0, jst)
	store := NewStore(dir)
	for _, record := range []Record{
		{ID: "pt-before-restart", FirstSeen: at, LastSeen: at.Add(time.Second), Sightings: 11,
			Reading: Reading{Number: "1870", Confidence: .94}, Box: [4]int{480, 591, 45, 24}},
		{ID: "pt-after-restart", FirstSeen: at.Add(1100 * time.Millisecond), LastSeen: at.Add(1200 * time.Millisecond), Sightings: 2,
			Reading: Reading{Number: "1870", Confidence: .96}, Box: [4]int{221, 609, 50, 38}},
	} {
		if err := store.appendRecord(record); err != nil {
			t.Fatal(err)
		}
	}
	if repaired, err := NewStore(dir).RepairRecent(at.Add(time.Minute)); err != nil || repaired != 1 {
		t.Fatalf("repaired=%d err=%v", repaired, err)
	}
	rows, err := NewStore(dir).ListAll(Query{From: Day(at), To: Day(at)})
	if err != nil || len(rows) != 1 || rows[0].Sightings != 13 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if repaired, err := NewStore(dir).RepairRecent(at.Add(time.Minute)); err != nil || repaired != 0 {
		t.Fatalf("second repair=%d err=%v", repaired, err)
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
	if o, e := s.Ingest(a, settings, nil); e != nil || !o.New {
		t.Fatalf("root %+v %v", o, e)
	}
	if o, e := s.Ingest(b, settings, nil); e != nil || o.New || o.Record.ID != a.Track.ID {
		t.Fatalf("initial fragment %+v %v", o, e)
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
