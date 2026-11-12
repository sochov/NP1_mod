// Package platespeech turns plate matches into spoken announcements.
//
// Synthesis happens ahead of time: rule messages are synthesized through the
// WEB版VOICEVOX API (su-shiki / tts.quest) when settings are saved, and the
// plate vocabulary (area names, digits, kana, number pairs) is synthesized
// once per voice style. A detection is then announced by concatenating stored
// clips and playing them on the device speaker through PulseAudio, so the
// plates of other vehicles are never sent to the API.
package platespeech

import (
	"strings"

	"github.com/np1-control-center/ncc/internal/plate"
)

var digitYomi = [10]string{"ぜろ", "いち", "にー", "さん", "よん", "ごー", "ろく", "なな", "はち", "きゅう"}

var letterYomi = map[rune]string{'A': "えー", 'C': "しー", 'F': "えふ", 'H': "えいち", 'K': "けー", 'L': "える", 'M': "えむ", 'P': "ぴー", 'X': "えっくす", 'Y': "わい"}

// PlateKana are the hiragana the reader can output.
const PlateKana = "あいうえかきくけこをさすせそたちつてとなにぬねのはひふほまみむめもやゆよらりるろわれ"

// Area names Open JTalk misreads (checked against the VOICEVOX dictionary
// for all Regions); the rest are sent as written for natural accent.
var regionYomi = map[string]string{"熊谷": "くまがや", "豊田": "とよた", "久留米": "くるめ"}

func regionText(region string) string {
	if y, ok := regionYomi[region]; ok {
		return y
	}
	return region
}

func digit(b byte) string { return digitYomi[b-'0'] }

// pair reads two serial digits the way plates are read aloud: "25" as
// にじゅうご, "05" as まるごー, "00" as まるまる.
func pair(p string) string {
	if p[0] != '0' {
		return p
	}
	if p[1] == '0' {
		return "まるまる"
	}
	return "まる" + digit(p[1])
}

// Tokens are the clip texts that read a plate, in order. Fields the engine
// could not read are skipped rather than guessed.
func Tokens(r plate.Reading) []string {
	out := []string{}
	if r.Region != "" {
		out = append(out, regionText(r.Region))
	}
	for _, c := range r.Class {
		switch {
		case c >= '0' && c <= '9':
			out = append(out, digit(byte(c)))
		case letterYomi[c] != "":
			out = append(out, letterYomi[c])
		}
	}
	if r.Kana != "" {
		out = append(out, r.Kana)
	}
	n := r.Number
	if strings.Trim(n, "0123456789") != "" {
		return out
	}
	switch len(n) {
	case 1:
		out = append(out, digit(n[0]))
	case 2:
		out = append(out, n)
	case 3:
		out = append(out, digit(n[0]), pair(n[1:]))
	case 4:
		out = append(out, pair(n[:2]), pair(n[2:]))
	}
	return out
}

// Sentence is the plate read as one sentence for whole-utterance synthesis.
func Sentence(r plate.Reading) string {
	t := Tokens(r)
	if len(t) == 0 {
		return ""
	}
	return strings.Join(t, "、") + "。"
}

// Vocabulary lists every token text Tokens can produce for engine output.
func Vocabulary() []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, r := range Regions {
		add(regionText(r))
	}
	for _, d := range digitYomi {
		add(d)
	}
	for _, c := range "ACFHKLMPXY" {
		add(letterYomi[c])
	}
	for _, k := range PlateKana {
		add(string(k))
	}
	for a := '0'; a <= '9'; a++ {
		for b := '0'; b <= '9'; b++ {
			add(pair(string([]rune{a, b})))
		}
	}
	return out
}
