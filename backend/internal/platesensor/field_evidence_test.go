package platesensor

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

func TestCorroboratedOptionalFieldOnClearSmallCrops(t *testing.T) {
	start := time.Unix(10, 0)
	for _, tc := range []struct {
		name          string
		fieldScores   bool
		policy        fieldPolicy
		width, frames int
		score         float64
		lastValue     string
		gap           time.Duration
		want          bool
	}{
		{name: "three distinct exposures", fieldScores: true, width: 60, frames: 3, score: .94, gap: 250 * time.Millisecond, want: true},
		{name: "old engine without field scores", width: 60, frames: 3, score: .94, gap: 250 * time.Millisecond},
		{name: "one exposure delivered twice", fieldScores: true, width: 60, frames: 4, score: .94},
		{name: "two frames", fieldScores: true, width: 60, frames: 2, score: .94, gap: time.Second},
		{name: "sustained moderate scores", fieldScores: true, width: 60, frames: 9, score: .84, gap: 250 * time.Millisecond, want: true},
		{name: "no high-confidence field", fieldScores: true, width: 60, frames: 4, score: .85, gap: 250 * time.Millisecond},
		{name: "conflicting field", fieldScores: true, width: 60, frames: 4, score: .94, gap: 250 * time.Millisecond, lastValue: "301"},
		{name: "too small", fieldScores: true, width: 50, frames: 3, score: .94, gap: 250 * time.Millisecond},
		{name: "explicit width setting", fieldScores: true, policy: fieldPolicy{MinWidth: 70, explicitWidth: true}, width: 65, frames: 4, score: .94, gap: 250 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := plateTrack{first: start, fieldPolicy: tc.policy}
			for i := 0; i < tc.frames; i++ {
				at := start.Add(time.Duration(i) * tc.gap)
				p := EnginePlate{Box: [4]int{720, 460, tc.width, 35}, Score: .82, Reading: plate.Reading{Number: "1234", Class: "300", Kana: "あ", Confidence: .90}}
				if tc.fieldScores {
					p.FieldScores = &[3]float64{0, tc.score, .64}
				}
				if i == tc.frames-1 && tc.lastValue != "" {
					p.Reading.Class = tc.lastValue
				}
				tr.observe(at, at, p, strconv.Itoa(i), .6)
			}
			d, ok := tr.detection(true)
			if !ok || d.Reading.Number != "1234" || (d.Reading.Class == "300") != tc.want || d.Reading.Kana != "" {
				t.Fatalf("reading %+v, expected class %v", d.Reading, tc.want)
			}
		})
	}
}

func TestCorroboratedOptionalFieldWithWideOrJoinedEvidence(t *testing.T) {
	start := time.Unix(100, 0)
	var first, second fieldEvidence
	first.observe("300", .94, "1", start)
	first.observe("300", .91, "2", start.Add(time.Second))
	second.observe("300", .85, "3", start.Add(2*time.Second))
	second.observe("300", .87, "4", start.Add(3*time.Second))
	first.merge(second)
	if got := first.consensus(nil, .67, false); got != "300" {
		t.Fatalf("joined %q", got)
	}
	if got := first.consensus(map[string]int{"301": 1}, .67, false); got != "" {
		t.Fatalf("conflicting wide field %q", got)
	}
	var overlapping fieldEvidence
	overlapping.observe("300", .95, "2", start.Add(time.Second))
	overlapping.observe("300", .95, "3", start.Add(2*time.Second))
	first.merge(overlapping)
	if got := first.consensus(nil, .67, false); got != "" {
		t.Fatalf("overlapping evidence %q", got)
	}
}
func TestExplicitFieldWidthIsRespected(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "plate")
	if err := os.MkdirAll(p, 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		data   string
		strict bool
	}{{"{}", false}, {`{"fieldMinWidth":70}`, true}, {`{"fieldMinWidth":95}`, true}, {`{"fieldMinWidth":-1}`, false}} {
		if err := os.WriteFile(filepath.Join(p, "settings.json"), []byte(tc.data), 0600); err != nil {
			t.Fatal(err)
		}
		if got := readFieldPolicy(dir).explicitWidth; got != tc.strict {
			t.Fatalf("%s explicit = %v", tc.data, got)
		}
	}
}

func TestShortClassificationNeedsRepeatedLengthConflict(t *testing.T) {
	for _, tc := range []struct {
		description      string
		winner           string
		long             string
		count, conflicts int
		disputed         bool
	}{
		{"two readers disagree and another length repeats", "3", "330", 2, 2, true},
		{"one conflicting frame", "3", "330", 2, 1, false},
		{"one longer read", "3", "330", 1, 8, false},
		{"different prefix", "3", "530", 5, 4, false},
		{"old legitimate short classification", "3", "", 0, 10, false},
		{"old sensor report", "30", "300", 3, 0, false},
		{"complete classification", "300", "300", 4, 3, false},
	} {
		t.Run(tc.description, func(t *testing.T) {
			got := classLengthDisputed(tc.winner, map[string]int{tc.winner: 10, tc.long: tc.count}, map[string]int{tc.winner: tc.conflicts})
			if got != tc.disputed {
				t.Fatalf("disputed = %v", got)
			}
		})
	}
}

func TestFieldDuplicateDeliveryCannotSupplyExtraFrames(t *testing.T) {
	var f fieldEvidence
	now := time.Unix(10, 0)
	for i := range 5 {
		f.observe("301", .95, "same-frame", now.Add(time.Duration(i)*time.Second))
	}
	if len(f.values) != 1 || f.strong["301"].count != 1 || f.consensus(nil, .67, false) != "" {
		t.Fatalf("duplicate accepted: %+v", f)
	}
}

func TestDominantCompleteClassAcrossSustainedClearFrames(t *testing.T) {
	for _, tc := range []struct {
		partial string
		count   int
		want    bool
	}{
		{"30", 30, true}, {"35", 30, true}, {"5", 30, true},
		{"50", 30, false}, {"315", 30, false}, {"9", 30, false}, {"30", 8, false},
	} {
		var f fieldEvidence
		start := time.Unix(1, 0)
		f.observe(tc.partial, .94, "start", start)
		for i := range tc.count {
			f.observe("305", .83, strconv.Itoa(i), start.Add(time.Duration(i+1)*300*time.Millisecond))
		}
		got := f.consensus(nil, .67, true)
		if (got == "305") != tc.want {
			t.Fatalf("%s (%d): %q", tc.partial, tc.count, got)
		}
		if f.consensus(nil, .67, false) != "" {
			t.Fatal("partial matching must be limited to class")
		}
	}
}
