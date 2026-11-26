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

// digitYomi reads each plate digit on its own, as plates are read aloud:
// 「相模503 あ12-34」 is さがみ ご まる さん あ いち に さん よん.
var digitYomi = [10]string{"まる", "いち", "に", "さん", "よん", "ご", "ろく", "なな", "はち", "きゅう"}

var letterYomi = map[rune]string{'A': "えー", 'C': "しー", 'F': "えふ", 'H': "えいち", 'K': "けー", 'L': "える", 'M': "えむ", 'P': "ぴー", 'X': "えっくす", 'Y': "わい"}

// PlateKana are the hiragana the reader can output.
const PlateKana = "あいうえかきくけこをさすせそたちつてとなにぬねのはひふほまみむめもやゆよらりるろわれ"

// Hiragana is every basic kana; all of them are cached so a kana the engine
// learns later, or one typed in a rule, is still read from the cache.
const Hiragana = "あいうえおかきくけこさしすせそたちつてとなにぬねのはひふへほまみむめもやゆよらりるれろわをん"

// Area names Open JTalk misreads (checked against the VOICEVOX dictionary
// for all Regions); the rest are sent as written for natural accent.
var regionYomi = map[string]string{"熊谷": "くまがや", "豊田": "とよた", "久留米": "くるめ"}

func regionText(region string) string {
	if y, ok := regionYomi[region]; ok {
		return y
	}
	return region
}

func digit(r rune) string { return digitYomi[r-'0'] }

// Tokens are the clip texts that read a plate, in order: area, class and
// serial digit by digit, kana. Fields the engine could not read are skipped
// rather than guessed; the serial's leading dots are not spoken.
func Tokens(r plate.Reading) []string {
	out := []string{}
	if r.Region != "" {
		out = append(out, regionText(r.Region))
	}
	for _, c := range r.Class {
		switch {
		case c >= '0' && c <= '9':
			out = append(out, digit(c))
		case letterYomi[c] != "":
			out = append(out, letterYomi[c])
		}
	}
	if r.Kana != "" {
		out = append(out, r.Kana)
	}
	n := r.Number
	if n == "" || len(n) > 4 || strings.Trim(n, "0123456789") != "" {
		return out
	}
	for _, c := range n {
		out = append(out, digit(c))
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

// Vocabulary lists every token text Tokens can produce: all plate area
// names, the digits 0-9, the class letters and every hiragana.
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
	for _, k := range PlateKana + Hiragana {
		add(string(k))
	}
	return out
}
