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

func sameOverlappingPlate(a, b Record) bool {
	if a.Reading.Number == "" || a.Reading.Number != b.Reading.Number ||
		!encounterFieldsAgree(a.Reading, b.Reading) ||
		a.LastSeen.Before(b.FirstSeen) || b.LastSeen.Before(a.FirstSeen) {
		return false
	}
	ab, bb := a.Box, b.Box
	if min(ab[2], ab[3], bb[2], bb[3]) <= 0 ||
		max(ab[2], bb[2])*5 > min(ab[2], bb[2])*8 ||
		max(ab[3], bb[3])*5 > min(ab[3], bb[3])*9 {
		return false
	}
	ax, ay := float64(2*ab[0]+ab[2])/2, float64(2*ab[1]+ab[3])/2
	bx, by := float64(2*bb[0]+bb[2])/2, float64(2*bb[1]+bb[3])/2
	return math.Hypot(bx-ax, by-ay) <= 75
}

// followsSameNumber implements the registration policy: an exact serial seen
// again during one driving encounter is the same vehicle unless there is
// positive evidence to separate it. Geometry-only tracking is deliberately
// not a veto; a close plate can cross hundreds of pixels between 8 fps frames.
func followsSameNumber(prior, next Record) bool {
	gap := next.FirstSeen.Sub(prior.LastSeen)
	n := len(prior.Reading.Number)
	if n == 0 || prior.Reading.Number != next.Reading.Number || gap > encounterGap ||
		min(prior.Reading.Confidence, next.Reading.Confidence) < .80 ||
		!encounterFieldsAgree(prior.Reading, next.Reading) {
		return false
	}
	if gap <= 0 {
		return sameOverlappingPlate(prior, next)
	}

	// A valid fix is negative evidence only when the camera would have needed
	// to travel at an impossible road speed. Missing or estimated positions do
	// not prevent registration-time merging.
	if prior.Location != nil && next.Location != nil {
		lat := (prior.Location.Lat + next.Location.Lat) * math.Pi / 360
		dy := (next.Location.Lat - prior.Location.Lat) * 111320
		dx := (next.Location.Lon - prior.Location.Lon) * 111320 * math.Cos(lat)
		if math.Hypot(dx, dy) > 150+65*gap.Seconds() {
			return false
		}
	}

	// Adjacent fragments are allowed to move across the frame, but not jump
	// to a clearly different vertical row. This also covers one- and two-digit
	// serials, for which a long time-only match would be too weak.
	if gap <= 2*time.Second {
		a, b := prior.Box, next.Box
		if min(a[2], a[3], b[2], b[3]) <= 0 {
			return false
		}
		ay := float64(2*a[1]+a[3]) / 2
		by := float64(2*b[1]+b[3]) / 2
		return math.Abs(by-ay) <= 180
	}

	switch n {
	case 1:
		return max(prior.Reading.Confidence, next.Reading.Confidence) >= .94
	case 2:
		return max(prior.Reading.Confidence, next.Reading.Confidence) >= .92
	case 3:
		return max(prior.Reading.Confidence, next.Reading.Confidence) >= .90
	default:
		return true
	}
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

func (s *Store) follows(a, b Record) bool {
	return followsSameNumber(a, b) || s.followsInVehicleRow(a, b)
}

// repairFragmentedEncounters upgrades records written before the current
// registration policy. It only adds explicit membership; source records remain
// independently recoverable in the append-only journal.
func repairFragmentedEncounters(records map[string]Record) []Record {
	ordered := make([]Record, 0, len(records))
	for _, record := range records {
		ordered = append(ordered, record)
	}
	sort.Slice(ordered, func(i, j int) bool { return encounterOrder(ordered[i], ordered[j]) })
	changedIDs := map[string]bool{}
	flatten := func() bool {
		flattened := false
		for _, saved := range ordered {
			next := records[saved.ID]
			root := next.EncounterID
			seen := map[string]bool{}
			cycle := false
			for root != "" {
				if seen[root] {
					cycle = true
					break
				}
				seen[root] = true
				parent, ok := records[root]
				if !ok || parent.EncounterID == "" {
					break
				}
				root = parent.EncounterID
			}
			if !cycle && root != "" && root != next.EncounterID {
				next.EncounterID = root
				records[next.ID] = next
				changedIDs[next.ID] = true
				flattened = true
			}
		}
		return flattened
	}
	// An older process may already have joined C to B. If this repair then
	// joins B to A, leaving C pointed at B creates a nested membership that the
	// UI projection intentionally does not infer. Iterate until both existing
	// and newly-created memberships point directly at their canonical root.
	for {
		passChanged := flatten()
		processed := make([]Record, 0, len(ordered))
		for _, saved := range ordered {
			next := records[saved.ID]
			if next.EncounterID == "" && strings.HasPrefix(next.ID, "pt-") {
				candidate := ""
				ambiguous := false
				for _, prior := range processed {
					if prior.ID == next.ID || !strings.HasPrefix(prior.ID, "pt-") || !followsSameNumber(prior, next) {
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
					changedIDs[next.ID] = true
					passChanged = true
				}
			}
			processed = append(processed, next)
		}
		if flatten() {
			passChanged = true
		}
		if !passChanged {
			break
		}
	}
	changed := make([]Record, 0, len(changedIDs))
	for _, saved := range ordered {
		if changedIDs[saved.ID] {
			changed = append(changed, records[saved.ID])
		}
	}
	return changed
}

// RepairRecent persists encounter membership for records written by an older
// process. It runs when the Web/API service starts, so already-recorded rows
// are repaired even when the sensor has no new detection after the restart.
// The current and preceding JST day are enough to cover encounterGap across
// midnight. The append-only journal keeps every source record recoverable.
func (s *Store) RepairRecent(at time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := map[string]Record{}
	for day := at.In(jst).AddDate(0, 0, -1); !day.After(at.In(jst)); day = day.AddDate(0, 0, 1) {
		err := readDay(filepath.Join(s.recordsDir(), Day(day)+".jsonl"), records)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
	}
	changed := repairFragmentedEncounters(records)
	for _, record := range changed {
		if err := s.appendRecord(record); err != nil {
			return 0, err
		}
	}
	return len(changed), nil
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
				(r.Reading.Number != next.Reading.Number || !encounterFieldsAgree(r.Reading, next.Reading)) {
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
		if !next.FirstSeen.After(r.LastSeen) && !r.FirstSeen.After(next.LastSeen) && !sameOverlappingPlate(*r, *next) {
			continue
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
		// A matching member is sufficient once every confirmed field in the
		// explicit group agrees. A long root can overlap a moving series of
		// short duplicate tracks, so its representative box is not a veto for
		// a later child that matches the immediately preceding child.
		for _, r := range s.open {
			if encounterKey(*r) == candidate {
				if !encounterFieldsAgree(r.Reading, next.Reading) {
					return
				}
			}
		}
		next.EncounterID = candidate
	}
}

func encounterOrder(a, b Record) bool {
	if a.FirstSeen.Equal(b.FirstSeen) {
		return a.ID < b.ID
	}
	return a.FirstSeen.Before(b.FirstSeen)
}

func combineTrack(out *Record, p Record) {
	previousLast := out.LastSeen
	if p.FirstSeen.After(previousLast) {
		out.Sightings += p.Sightings
	} else if p.LastSeen.After(previousLast) {
		duration := p.LastSeen.Sub(p.FirstSeen)
		if duration > 0 {
			additional := int(math.Round(float64(p.Sightings) * float64(p.LastSeen.Sub(previousLast)) / float64(duration)))
			out.Sightings += min(p.Sightings, max(1, additional))
		}
	}
	if p.LastSeen.After(out.LastSeen) {
		out.LastSeen = p.LastSeen
	}
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
			!encounterFieldsAgree(current.Reading, source.Reading) ||
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
