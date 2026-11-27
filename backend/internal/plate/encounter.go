package plate

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The source box is in the 1920x1080 camera frame. After two and a half
// minutes with no detection there is too little evidence of continuous travel.
const encounterGap = 150 * time.Second
const encounterRetention = encounterGap + 30*time.Second

func encounterKey(r Record) string {
	if r.EncounterID != "" {
		return r.EncounterID
	}
	return r.ID
}

func encounterFieldsAgree(a, b Reading) bool {
	return (a.Region == "" || b.Region == "" || a.Region == b.Region) &&
		(a.Class == "" || b.Class == "" || a.Class == b.Class) &&
		(a.Kana == "" || b.Kana == "" || a.Kana == b.Kana)
}

// Both inputs have passed the sensor and registration gates. Sightings are
// detector frames, not independent OCR votes. Use them to establish presence.
func followsRecordedTrack(prior, next Record) bool {
	gap := next.FirstSeen.Sub(prior.LastSeen)
	n := len(next.Reading.Number)
	if n < 3 || prior.Reading.Number != next.Reading.Number || gap <= 0 || gap > encounterGap ||
		prior.Sightings < 8 || prior.LastSeen.Sub(prior.FirstSeen) < 4*time.Second ||
		next.Sightings < 2 || min(prior.Reading.Confidence, next.Reading.Confidence) < .72 ||
		max(prior.Reading.Confidence, next.Reading.Confidence) < .84 || !encounterFieldsAgree(prior.Reading, next.Reading) {
		return false
	}
	a, b := prior.Box, next.Box
	if min(a[2], a[3], b[2], b[3]) <= 0 || max(a[2], b[2]) > 2*min(a[2], b[2]) {
		return false
	}
	ax, ay := float64(2*a[0]+a[2])/2, float64(2*a[1]+a[3])/2
	bx, by := float64(2*b[0]+b[2])/2, float64(2*b[1]+b[3])/2
	// Stable lead car before the interruption. A plate low in the cabin
	// or one passing at the edge provides no such continuity evidence.
	if math.Abs(ax-960) > 320 || ay < 355 || ay > 745 || math.Abs(by-ay) > 90 {
		return false
	}
	dx := math.Abs(bx - ax)
	if gap <= 30*time.Second && n == 4 {
		// A lane change can move a plate horizontally while keeping its
		// scale and row. A larger jump requires sustained prior observation.
		return dx <= 180 || (dx <= 480 && math.Abs(by-ay) <= 40 &&
			prior.Sightings >= 20 && prior.Reading.Confidence >= .90 && next.Reading.Confidence >= .82 &&
			max(a[2], b[2])*4 <= min(a[2], b[2])*5)
	}
	if dx > 125 || math.Abs(bx-960) > 320 {
		return false
	}
	if n == 3 {
		// A shorter number is common: require a very clear read on one side
		// and a nearly identical forward position over a shorter gap.
		return gap <= 90*time.Second && dx <= 75 && math.Abs(ay-by) <= 45 &&
			prior.Sightings >= 12 && next.Sightings >= 3 &&
			max(prior.Reading.Confidence, next.Reading.Confidence) >= .94
	}
	return prior.Sightings >= 16 && next.Sightings >= 3 && prior.LastSeen.Sub(prior.FirstSeen) >= 8*time.Second
}

// A short serial in a row of parked cars is weak on its own. A crossing
// vehicle can nevertheless observe the same row from several angles. Require
// two distinct long serials in the same scene, including one continuously
// tracked neighbor on the same side of the candidate across the interruption.
func (s *Store) followsInVehicleRow(a, b Record) bool {
	n := len(a.Reading.Number)
	gap := b.FirstSeen.Sub(a.LastSeen)
	if n < 2 || n > 3 || a.Reading.Number != b.Reading.Number || gap <= 0 || gap > 12*time.Second ||
		min(a.Reading.Confidence, b.Reading.Confidence) < .82 || min(a.Sightings, b.Sightings) < 2 ||
		!encounterFieldsAgree(a.Reading, b.Reading) {
		return false
	}
	ab, bb := a.Box, b.Box
	ax, ay := ab[0]+ab[2]/2, ab[1]+ab[3]/2
	bx, by := bb[0]+bb[2]/2, bb[1]+bb[3]/2
	if min(ab[2], bb[2]) < 32 || min(ab[3], bb[3]) < 15 || max(ab[2], bb[2]) > 2*min(ab[2], bb[2]) ||
		min(ax, bx) < 240 || max(ax, bx) > 1680 || min(ay, by) < 380 || max(ay, by) > 720 ||
		math.Abs(float64(ax-bx)) > 480 || math.Abs(float64(ay-by)) > 65 {
		return false
	}
	var firstNeighbor string
	otherNeighbor, spansGap := false, false
	for _, r := range s.open {
		if len(r.Reading.Number) != 4 || r.Reading.Confidence < .80 || r.Sightings < 5 ||
			r.FirstSeen.After(b.FirstSeen) || r.LastSeen.Before(a.LastSeen) {
			continue
		}
		x, y := r.Box[0]+r.Box[2]/2, r.Box[1]+r.Box[3]/2
		if r.Box[2] < 32 || r.Box[3] < 15 || math.Abs(float64(y-ay)) > 72 || math.Abs(float64(y-by)) > 72 ||
			(x-ax)*(x-bx) <= 0 || min(math.Abs(float64(x-ax)), math.Abs(float64(x-bx))) < 80 {
			continue
		}
		if firstNeighbor == "" {
			firstNeighbor = r.Reading.Number
		} else if r.Reading.Number != firstNeighbor {
			otherNeighbor = true
		}
		spansGap = spansGap || !r.FirstSeen.After(a.LastSeen) && !r.LastSeen.Before(b.FirstSeen)
		if otherNeighbor && spansGap {
			return true
		}
	}
	return false
}

// followsFastFragment joins the short track IDs produced when a nearby plate
// crosses the frame faster than geometry-only assignment can follow it. The
// serial alone is not sufficient: time, row, scale and physically possible
// motion must all agree, and at least one side must actually be a short
// fragment. Simultaneous identical serials therefore remain separate.
func followsFastFragment(a, b Record) bool {
	gap := b.FirstSeen.Sub(a.LastSeen)
	if len(a.Reading.Number) < 3 || a.Reading.Number != b.Reading.Number || gap <= 0 || gap > 600*time.Millisecond ||
		min(a.Reading.Confidence, b.Reading.Confidence) < .80 || !encounterFieldsAgree(a.Reading, b.Reading) {
		return false
	}
	aDuration, bDuration := a.LastSeen.Sub(a.FirstSeen), b.LastSeen.Sub(b.FirstSeen)
	if !((a.Sightings <= 2 && aDuration <= 500*time.Millisecond) ||
		(b.Sightings <= 2 && bDuration <= 500*time.Millisecond)) {
		return false
	}
	ab, bb := a.Box, b.Box
	if min(ab[2], ab[3], bb[2], bb[3]) <= 0 ||
		max(ab[2], bb[2])*5 > min(ab[2], bb[2])*8 || max(ab[3], bb[3])*5 > min(ab[3], bb[3])*9 {
		return false
	}
	ax, ay := float64(2*ab[0]+ab[2])/2, float64(2*ab[1]+ab[3])/2
	bx, by := float64(2*bb[0]+bb[2])/2, float64(2*bb[1]+bb[3])/2
	seconds := gap.Seconds()
	return math.Abs(bx-ax) <= 70+480*seconds && math.Abs(by-ay) <= 25+180*seconds
}

func (s *Store) follows(a, b Record) bool {
	return followsFastFragment(a, b) || followsRecordedTrack(a, b) || s.followsInVehicleRow(a, b)
}

// repairFastFragmentEncounters upgrades records written by an older process
// before the fast-fragment rule existed. It only adds explicit membership;
// source records remain independently recoverable in the append-only journal.
func repairFastFragmentEncounters(records map[string]Record) []Record {
	ordered := make([]Record, 0, len(records))
	for _, record := range records {
		ordered = append(ordered, record)
	}
	sort.Slice(ordered, func(i, j int) bool { return encounterOrder(ordered[i], ordered[j]) })
	processed := make([]Record, 0, len(ordered))
	changed := []Record{}
	for _, next := range ordered {
		if next.EncounterID == "" && strings.HasPrefix(next.ID, "pt-") {
			candidate := ""
			ambiguous := false
			for _, prior := range processed {
				if prior.ID == next.ID || !strings.HasPrefix(prior.ID, "pt-") || !followsFastFragment(prior, next) {
					continue
				}
				key := encounterKey(prior)
				if candidate != "" && candidate != key {
					ambiguous = true
					break
				}
				candidate = key
			}
			if candidate != "" && !ambiguous {
				next.EncounterID = candidate
				records[next.ID] = next
				changed = append(changed, next)
			}
		}
		processed = append(processed, next)
	}
	return changed
}

func (s *Store) encounterMembers(key string, next Record) []Record {
	group := []Record{next}
	for _, r := range s.open {
		if r.ID != next.ID && encounterKey(*r) == key {
			group = append(group, *r)
		}
	}
	return group
}

func (s *Store) registerEncounter(next *Record) {
	// Keep each append independently retryable: membership and the new
	// track snapshot are written together. Never infer links on a list query.
	if next.EncounterID != "" {
		for _, r := range s.open {
			if r.ID != next.ID && encounterKey(*r) == next.EncounterID &&
				(r.Reading.Number != next.Reading.Number || !encounterFieldsAgree(r.Reading, next.Reading) ||
					(!next.FirstSeen.After(r.LastSeen) && !r.FirstSeen.After(next.LastSeen))) {
				next.EncounterID = ""
				break
			}
		}
		return
	}
	var candidate string
	for _, r := range s.open {
		if r.EncounterID == next.ID {
			return
		}
		if r.ID == next.ID || !strings.HasPrefix(r.ID, "pt-") ||
			r.Reading.Number != next.Reading.Number {
			continue
		}
		// A group root cannot become its own descendant. Two simultaneous
		// registrations can be identical serials on two different cars.
		if !next.FirstSeen.After(r.LastSeen) && !r.FirstSeen.After(next.LastSeen) {
			return
		}
		if !s.follows(*r, *next) {
			continue
		}
		key := encounterKey(*r)
		if candidate != "" && candidate != key {
			return
		}
		candidate = key
	}
	if candidate != "" {
		// Do not jump over a more recent, incompatible member of the same
		// encounter just because one earlier track matched the new plate.
		var last *Record
		for _, r := range s.open {
			if encounterKey(*r) == candidate {
				if !encounterFieldsAgree(r.Reading, next.Reading) || !next.FirstSeen.After(r.LastSeen) {
					return
				}
				if last == nil || r.LastSeen.After(last.LastSeen) {
					last = r
				}
			}
		}
		if last != nil && s.follows(*last, *next) {
			next.EncounterID = candidate
		}
	}
}

func encounterOrder(a, b Record) bool {
	if a.FirstSeen.Equal(b.FirstSeen) {
		return a.ID < b.ID
	}
	return a.FirstSeen.Before(b.FirstSeen)
}

func combineTrack(out *Record, p Record) {
	out.LastSeen = p.LastSeen
	out.Sightings += p.Sightings
	out.Reading.Confidence = max(out.Reading.Confidence, p.Reading.Confidence)
	if out.Reading.Region == "" {
		out.Reading.Region = p.Reading.Region
	}
	if out.Reading.Class == "" {
		out.Reading.Class = p.Reading.Class
	}
	if out.Reading.Kana == "" {
		out.Reading.Kana = p.Reading.Kana
	}
	if out.Location == nil {
		out.Location = p.Location
	}
	if out.Speech == "" {
		out.Speech = p.Speech
	}
	if out.ImageID == "" && p.ImageID != "" {
		out.ImageID, out.Box, out.ImageNote = p.ImageID, p.Box, p.ImageNote
	}
	for _, m := range p.Matches {
		found := false
		for _, have := range out.Matches {
			if have.RuleID == m.RuleID {
				found = true
				break
			}
		}
		if !found {
			out.Matches = append(out.Matches, m)
		}
	}
	out.Display = out.Reading.Display()
}

// Project explicit memberships only. If the sensor later corrects the root,
// its revised serial, fields and interval can split a previously linked group.
func summarizeEncounter(group []Record, wanted string) (rows []Record, selected Record) {
	sort.Slice(group, func(i, j int) bool { return encounterOrder(group[i], group[j]) })
	var current Record
	var hasWanted bool
	for _, source := range group {
		if current.ID != "" && (source.Reading.Number != current.Reading.Number ||
			!encounterFieldsAgree(current.Reading, source.Reading) || !source.FirstSeen.After(current.LastSeen) ||
			source.FirstSeen.Sub(current.LastSeen) > encounterGap) {
			rows = append(rows, current)
			if hasWanted {
				selected = current
			}
			current = Record{}
			hasWanted = false
		}
		if current.ID == "" {
			current = source
			current.EncounterID = ""
			if len(group) > 1 {
				current.Matches = append([]Match{}, source.Matches...)
			}
		} else {
			combineTrack(&current, source)
		}
		hasWanted = hasWanted || source.ID == wanted
	}
	rows = append(rows, current)
	if hasWanted {
		selected = current
	}
	return rows, selected
}

func projectEncounters(records map[string]Record) []Record {
	out := make([]Record, 0, len(records))
	groups := map[string][]Record{}
	for _, r := range records {
		if r.EncounterID != "" {
			groups[r.EncounterID] = append(groups[r.EncounterID], r)
		}
	}
	for id, r := range records {
		if r.EncounterID != "" {
			if _, ok := records[r.EncounterID]; !ok {
				r.EncounterID = ""
				out = append(out, r)
			}
			continue
		}
		if children := groups[id]; len(children) > 0 {
			rows, _ := summarizeEncounter(append(children, r), "")
			out = append(out, rows...)
		} else {
			out = append(out, r)
		}
	}
	return out
}

// Return the same encounter to alerts and sharing that List returns to the UI.
func (s *Store) encounterRecord(next Record) Record {
	group := s.encounterMembers(encounterKey(next), next)
	if len(group) == 1 {
		return next
	}
	_, r := summarizeEncounter(group, next.ID)
	return r
}

// A query starting after midnight can contain a child before reading its root.
// Only pull members of these specific encounters from preceding days.
func (s *Store) loadEncounterRoots(day time.Time, records map[string]Record) error {
	missing := map[string]bool{}
	for _, r := range records {
		if r.EncounterID != "" {
			if _, ok := records[r.EncounterID]; !ok {
				missing[r.EncounterID] = true
			}
		}
	}
	for len(missing) > 0 {
		day = day.AddDate(0, 0, -1)
		prior := map[string]Record{}
		err := readDay(filepath.Join(s.recordsDir(), Day(day)+".jsonl"), prior)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, r := range prior {
			if missing[encounterKey(r)] {
				records[r.ID] = r
			}
		}
		for id := range missing {
			if _, ok := records[id]; ok {
				delete(missing, id)
			}
		}
	}
	return nil
}
