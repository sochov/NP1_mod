package platesensor

import (
	"strconv"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

func TestFollowingJoinsOnlyCorroboratedSerials(t *testing.T) {
	position := [4]int{860, 565, 85, 46}
	at := time.Unix(100, 0)
	oldImage, nextImage := makeAppearanceForTest(77), makeAppearanceForTest(79)
	for _, tc := range []struct {
		name   string
		gap    time.Duration
		edit   func(previous, next *EnginePlate)
		joined bool
	}{
		{name: "continued despite different lighting", gap: 138 * time.Second, joined: true},
		{name: "one weak field can differ", gap: 40 * time.Second, joined: true, edit: func(a, b *EnginePlate) {
			a.Reading.Kana, b.Reading.Kana = "あ", "か"
		}},
		{name: "different well-read metadata", gap: 40 * time.Second, edit: func(a, b *EnginePlate) {
			a.Reading.Region, b.Reading.Region = "横浜", "相模"
			a.Reading.Class, b.Reading.Class = "505", "300"
		}},
		{name: "late return", gap: 15 * time.Minute},
		{name: "different position", gap: 40 * time.Second, edit: func(_, b *EnginePlate) {
			b.Box[0] = 1500
		}},
		{name: "number differs by one digit", gap: 40 * time.Second, edit: func(_, b *EnginePlate) {
			box := b.Box
			*b = verifiedPlateForRejoin("1239", box)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := verifiedPlateForRejoin("1234", position)
			next := verifiedPlateForRejoin("1234", position)
			if tc.edit != nil {
				tc.edit(&previous, &next)
			}
			previous.appearance, next.appearance = oldImage, nextImage
			old := observeFollowingTestTrack(at, previous, 25)
			old.sent = old.frames
			following := observeFollowingTestTrack(old.last.Add(tc.gap), next, 5)
			if got := old.canRejoin(following); got != tc.joined {
				t.Fatalf("continuation = %v, want %v", got, tc.joined)
			}
			if previous.Reading.Number != next.Reading.Number && !waitingForAppearance(following, nil, []*plateTrack{old}) {
				t.Fatal("published an uncertain initial reading without giving later OCR a chance")
			}
		})
	}
}

func TestFollowingKeepsLegibleFieldsWhenLaterFramesAreClipped(t *testing.T) {
	box := [4]int{860, 565, 85, 46}
	at := time.Unix(100, 0)
	clear := verifiedPlateForRejoin("4112", box)
	clear.appearance = makeAppearanceForTest(51)
	clear.Reading.Region, clear.Reading.Class, clear.Reading.Kana = "横浜", "505", "ら"
	old := observeFollowingTestTrack(at, clear, 25)
	old.sent = old.frames
	glare := clear
	glare.Reading.Kana = "ち"
	glare.appearance = append([]byte(nil), clear.appearance...)
	for row := 19; row < 24; row++ {
		for col := 13; col < 17; col++ {
			glare.appearance[row*appearanceWidth+col] = 245
		}
	}
	later := observeFollowingTestTrack(old.last.Add(43*time.Second), glare, 100)
	if !old.canRejoin(later) {
		t.Fatal("clipping prevented a continuation with the same verified serial")
	}
	old.join(later)
	d, ok := old.detection(true)
	if !ok || d.Reading.Region != "横浜" || d.Reading.Class != "505" || d.Reading.Kana != "ら" ||
		d.Track.Frames != 125 || !d.At.Equal(at) || !d.Track.LastSeen.Equal(later.last) {
		t.Fatalf("merged detection = %+v", d)
	}
}

func observeFollowingTestTrack(at time.Time, p EnginePlate, count int) *plateTrack {
	p.Localized = &plate.Reading{Number: p.Reading.Number, Confidence: .95}
	result := &plateTrack{first: at}
	for i := range count {
		seen := at.Add(time.Duration(i) * 250 * time.Millisecond)
		result.observe(seen, seen, p, strconv.Itoa(i), .60)
	}
	return result
}
