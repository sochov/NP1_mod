// Package plate holds the policy side of Japanese licence-plate recognition:
// reading normalization, owner-defined match rules, record retention and the
// image store. It never talks to the camera or the inference engine; those
// live in the separate sensor process and arrive here as Detections.
package plate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Reading is one recognized plate. An empty field means the engine could not
// read it; it is never guessed.
type Reading struct {
	Region     string  `json:"region"`     // 地名 (e.g. 品川)
	Class      string  `json:"class"`      // 分類番号, 1–3 of [0-9A-Z]
	Kana       string  `json:"kana"`       // ひらがな, one rune
	Number     string  `json:"number"`     // 一連指定番号, 1–4 digits without dots or hyphen
	Confidence float64 `json:"confidence"` // 0–1, engine supplied
}

// Location is the stock GNSS fix nearest to the detection time.
type Location struct {
	At        time.Time `json:"at"`
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	AccuracyM *float64  `json:"accuracyM,omitempty"`
}

// Detection is what the sensor process reports for one plate in one frame.
type Detection struct {
	Seq      uint64    `json:"seq"`
	At       time.Time `json:"at"`
	Reading  Reading   `json:"reading"`
	Box      [4]int    `json:"box"` // x, y, w, h in the 1920x1080 source frame
	Frame    string    `json:"frame,omitempty"`
	Location *Location `json:"location,omitempty"`
}

// Rule is an owner-defined condition. Empty fields match anything, but at
// least one field must be set so a rule can never match every vehicle.
type Rule struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	Region    string `json:"region"`
	Class     string `json:"class"`
	Kana      string `json:"kana"`
	Number    string `json:"number"`
	SaveImage bool   `json:"saveImage"`
	Speak     bool   `json:"speak"`
}

type Settings struct {
	Revision      int     `json:"revision"`
	Enabled       bool    `json:"enabled"`
	Rules         []Rule  `json:"rules"`
	RetentionDays int     `json:"retentionDays"`
	ImageQuotaMB  int     `json:"imageQuotaMB"`
	MergeSeconds  int     `json:"mergeSeconds"`
	MinConfidence float64 `json:"minConfidence"`
	// MinSightings is how many compatible readings (within MergeSeconds) a
	// vehicle needs before it becomes a record. Multi-frame agreement removes
	// most single-frame misreads on dash-camera footage.
	MinSightings int `json:"minSightings"`
}

const (
	MaxRules       = 100
	maxNameRunes   = 40
	maxRegionRunes = 8
)

var (
	classPattern  = regexp.MustCompile(`^[0-9A-Z*]{1,3}$`)
	numberPattern = regexp.MustCompile(`^[0-9*]{1,4}$`)
	idPattern     = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)
)

func DefaultSettings() Settings {
	return Settings{Revision: 1, Rules: []Rule{}, RetentionDays: 365, ImageQuotaMB: 256, MergeSeconds: 30, MinConfidence: 0.6, MinSightings: 2}
}

// halfWidth folds full-width ASCII (Ｂ, ３) to half-width and removes the
// separators that appear on plates or in typed input.
func halfWidth(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if r >= 0xFF01 && r <= 0xFF5E {
			r -= 0xFEE0
		}
		switch r {
		case ' ', '　', '-', '－', '‐', '・', '･', '.', '·', '•':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// katakanaToHiragana lets owners type ア and still match あ.
func katakanaToHiragana(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'ァ' && r <= 'ヶ' {
			return r - 0x60
		}
		return r
	}, s)
}

// Normalize returns the canonical form used for storage and matching.
func Normalize(r Reading) Reading {
	r.Region = strings.TrimSpace(halfWidth(r.Region))
	r.Class = strings.ToUpper(halfWidth(r.Class))
	r.Kana = katakanaToHiragana(halfWidth(r.Kana))
	r.Number = strings.TrimLeft(halfWidth(r.Number), "0")
	if r.Confidence < 0 {
		r.Confidence = 0
	}
	if r.Confidence > 1 {
		r.Confidence = 1
	}
	return r
}

// Key identifies the same vehicle across frames. Partially read plates only
// merge with identical partial reads.
func (r Reading) Key() string {
	return r.Region + "|" + r.Class + "|" + r.Kana + "|" + r.Number
}

// Readable reports whether the engine produced at least the serial number.
func (r Reading) Readable() bool { return r.Number != "" }

// Display formats a reading the way it appears on a plate: 品川 300 あ 12-34.
func (r Reading) Display() string {
	number := r.Number
	switch len(number) {
	case 4:
		number = number[:2] + "-" + number[2:]
	case 1, 2, 3:
		number = strings.Repeat("・", 4-len(number)) + number
	case 0:
		number = "????"
	}
	parts := []string{}
	for _, p := range []string{r.Region, r.Class, r.Kana, number} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " ")
}

func normalizeRule(rule Rule) Rule {
	n := Normalize(Reading{Region: rule.Region, Class: rule.Class, Kana: rule.Kana, Number: rule.Number})
	rule.Name = strings.TrimSpace(rule.Name)
	rule.Region, rule.Class, rule.Kana = n.Region, n.Class, n.Kana
	// Keep wildcards and leading digits as typed; "0" padding is not used on plates.
	rule.Number = strings.TrimLeft(halfWidth(rule.Number), "0")
	return rule
}

func printable(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// ValidateSettings normalizes and checks owner input. It returns a
// user-facing Japanese reason on failure.
func ValidateSettings(s Settings) (Settings, error) {
	if s.RetentionDays < 1 || s.RetentionDays > 3650 {
		return s, errors.New("保存日数は1〜3650日で指定してください")
	}
	if s.ImageQuotaMB < 16 || s.ImageQuotaMB > 1024 {
		return s, errors.New("画像の保存容量は16〜1024MBで指定してください")
	}
	if s.MergeSeconds < 5 || s.MergeSeconds > 300 {
		return s, errors.New("同一車両とみなす時間は5〜300秒で指定してください")
	}
	if s.MinConfidence < 0.3 || s.MinConfidence > 0.99 {
		return s, errors.New("記録する信頼度の下限は0.3〜0.99で指定してください")
	}
	if s.MinSightings == 0 {
		s.MinSightings = 2 // settings saved before this field existed
	}
	if s.MinSightings < 1 || s.MinSightings > 5 {
		return s, errors.New("記録に必要な検出回数は1〜5回で指定してください")
	}
	if len(s.Rules) > MaxRules {
		return s, fmt.Errorf("条件は%d件までです", MaxRules)
	}
	if s.Rules == nil {
		s.Rules = []Rule{}
	}
	seen := map[string]bool{}
	for i, rule := range s.Rules {
		rule = normalizeRule(rule)
		label := fmt.Sprintf("条件%d", i+1)
		if !idPattern.MatchString(rule.ID) || seen[rule.ID] {
			return s, fmt.Errorf("%sの識別子が不正です", label)
		}
		seen[rule.ID] = true
		if rule.Name == "" || utf8.RuneCountInString(rule.Name) > maxNameRunes || !printable(rule.Name) {
			return s, fmt.Errorf("%sの名前は1〜%d文字で指定してください", label, maxNameRunes)
		}
		if rule.Region == "" && rule.Class == "" && rule.Kana == "" && rule.Number == "" {
			return s, fmt.Errorf("「%s」は地名・分類番号・ひらがな・番号のいずれかを指定してください", rule.Name)
		}
		if rule.Region != "" && (utf8.RuneCountInString(rule.Region) > maxRegionRunes || !printable(rule.Region)) {
			return s, fmt.Errorf("「%s」の地名は%d文字以内で指定してください", rule.Name, maxRegionRunes)
		}
		if rule.Class != "" && !classPattern.MatchString(rule.Class) {
			return s, fmt.Errorf("「%s」の分類番号は数字・英字・*の1〜3文字で指定してください", rule.Name)
		}
		if rule.Kana != "" {
			r, size := utf8.DecodeRuneInString(rule.Kana)
			if size != len(rule.Kana) || !unicode.Is(unicode.Hiragana, r) {
				return s, fmt.Errorf("「%s」のひらがなは1文字で指定してください", rule.Name)
			}
		}
		if rule.Number != "" && !numberPattern.MatchString(rule.Number) {
			return s, fmt.Errorf("「%s」の番号は数字と*の1〜4桁で指定してください", rule.Name)
		}
		s.Rules[i] = rule
	}
	return s, nil
}

// wildcard compares equal-length strings where '*' in pattern matches one character.
func wildcard(pattern, value string) bool {
	p, v := []rune(pattern), []rune(value)
	if len(p) != len(v) {
		return false
	}
	for i := range p {
		if p[i] != '*' && p[i] != v[i] {
			return false
		}
	}
	return true
}

// Matches reports whether a normalized reading satisfies the rule. A field
// the engine could not read never satisfies a condition on that field.
func (rule Rule) Matches(r Reading) bool {
	if !rule.Enabled {
		return false
	}
	if rule.Region != "" && rule.Region != r.Region {
		return false
	}
	if rule.Class != "" && !wildcard(rule.Class, r.Class) {
		return false
	}
	if rule.Kana != "" && rule.Kana != r.Kana {
		return false
	}
	if rule.Number != "" && !wildcard(rule.Number, r.Number) {
		return false
	}
	return true
}

// MatchingRules returns every enabled rule satisfied by the reading.
func MatchingRules(rules []Rule, r Reading) []Rule {
	out := []Rule{}
	for _, rule := range rules {
		if rule.Matches(r) {
			out = append(out, rule)
		}
	}
	return out
}
