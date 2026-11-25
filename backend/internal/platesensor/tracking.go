package platesensor

import (
	"math"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

const (
	trackGap           = 2 * time.Second
	trackRejoinGap     = 5 * time.Second
	trackRecentGap     = 16 * time.Second
	trackReadGrace     = time.Second
	trackUpdate        = 2 * time.Second
	trackMaxMisses     = 3
	trackMissWindow    = trackMaxMisses * 250 * time.Millisecond
	trackLimit         = 16
	trackOCRWindow     = 12
	trackEarlyVotes    = 3
	trackWeakReadGrace = 5 * time.Second
	trackFieldLimit    = 24
	trackInitialFrames = 48
	trackRecentLimit   = 4 * trackLimit
)

type observation struct {
	reading    plate.Reading
	box        [4]int
	frame      string
	weight     float64
	detector   float64
	appearance []byte
	at         time.Time
}

type serialCheck uint8

const (
	localizedCheck serialCheck = 1 << iota
	sequenceCheck
)

type serialEvidence struct {
	shrinkingSeed          observation
	digitDetailMeasured    bool
	digitDetailAdequate    bool
	weight                 float64
	support                int
	best                   observation
	wideVerification       float64
	jointConfidence        float64
	primaryProof           bool
	independentProof       bool
	sequenceProof          bool
	localizedProof         bool
	narrowSupport          int
	alignedNarrowSupport   int
	widerCropSupport       int
	smallCropSupport       int
	smallWideSupport       int
	smallSequenceFrames    [2]string
	smallLocalizedProof    bool
	repeatSequenceSupport  int
	repeatLocalizedSupport int
	repeatPrimaryCue       bool
	repeatWeakPrimaryCue   bool
	regularShape           bool
	lessObliqueRead        bool
	wellShapedFrames       [2]string
	wellShapedCrop         observation
	fields                 [3]map[string]int
}

type plateTrack struct {
	id                    string
	first, last, finished time.Time
	firstBox, box         [4]int
	velocity              [2]float64
	frames, misses        int
	sent                  int
	sentAt                time.Time
	reads                 int
	votes                 map[string]serialEvidence
	ocr                   []observation
	fields                map[string][3]fieldObservation
	fieldPolicy           fieldPolicy
	best                  observation
	outwardRead           observation
	outwardPrior          string
	support               int
	conflict              bool
	ambiguous             bool
	hasVerification       bool
	localized             map[string]float64
	localizedFrames       map[string][2]string
	challengers           map[string]serialCheck
	startFrames           []trackFrame
	openingFrames         []trackFrame
	appearanceSamples     []observation
	rejoinWaitAt          time.Time
	recentFrames          []trackFrame

	openingStart, reportedOpening time.Time
	openingView                   []byte
	openingClaimed                bool
}

type trackFrame struct {
	at, finished  time.Time
	plate         EnginePlate
	id            string
	minConfidence float64
}

type fieldObservation struct {
	value  string
	weight float64
}

func (t *plateTrack) observe(at, finished time.Time, p EnginePlate, frame string, minConf float64) bool {
	if t.frames == 0 {
		t.firstBox = p.Box
	} else if dt := at.Sub(t.last).Seconds(); dt > 0 {
		oldX, oldY := center(t.box)
		x, y := center(p.Box)
		measured := [2]float64{(x - oldX) / dt, (y - oldY) / dt}
		if t.frames > 1 {
			for i := range measured {
				measured[i] = .55*measured[i] + .45*t.velocity[i]
			}
		}
		t.velocity = measured
	}
	t.last, t.finished, t.box = at, finished, p.Box
	t.frames++
	t.misses = 0
	if (t.frames == 1 && len(p.continuityView) != 0 || len(t.openingFrames) > 0) && t.frames <= trackInitialFrames && t.sent == 0 {
		t.openingFrames = append(t.openingFrames, trackFrame{at, finished, p, frame, minConf})
	} else {
		t.openingFrames = nil
	}
	if t.frames <= trackInitialFrames && (p.Verifier != nil || len(t.startFrames) > 0) {
		t.startFrames = append(t.startFrames, trackFrame{at, finished, p, frame, minConf})
	} else {
		t.startFrames = nil
	}
	if p.Verifier != nil || len(t.recentFrames) > 0 {
		if len(t.recentFrames) == overlapHistoryLimit {
			copy(t.recentFrames, t.recentFrames[1:])
			t.recentFrames = t.recentFrames[:overlapHistoryLimit-1]
		}
		t.recentFrames = append(t.recentFrames, trackFrame{at, finished, p, frame, minConf})
	}
	// A detection clipped by the camera can still extend a known interval,
	// but missing image content is unsuitable for choosing its serial.
	if p.Box[0] < 0 || p.Box[1] < 0 || p.Box[0]+p.Box[2] > FrameWidth || p.Box[1]+p.Box[3] > FrameHeight {
		return false
	}
	if p.Box[2] >= 45 && p.Box[2] <= 70 {
		if p.Score >= .28 && p.Localized != nil && len(p.Localized.Number) == 4 && p.Localized.Confidence >= .78 {
			t.rememberChallenger(p.Localized.Number, localizedCheck)
		}
		if p.Score >= .35 && p.Verifier != nil && p.Verifier.Confidence >= .50 &&
			len(p.SequenceNumber) == 4 && p.Verifier.Number == p.SequenceNumber && p.NumberScores[p.SequenceNumber] >= .80 {
			t.rememberChallenger(p.SequenceNumber, sequenceCheck)
		}
	}
	if p.Localized != nil && p.Box[2] >= 40 && p.Score >= .35 {
		local := plate.Normalize(*p.Localized)
		if local.Readable() && local.Confidence >= .78 {
			t.rememberLocalized(local.Number, local.Confidence)
			wideDigitRead := len(local.Number) == 4 && local.Confidence >= .80
			shortDigitRead := len(local.Number) == 3 && p.Box[2] <= 65 && local.Confidence >= .82
			if p.Box[2] >= 45 && p.Score >= .40 && (wideDigitRead || shortDigitRead) {
				t.rememberLocalizedFrame(local.Number, frame)
			}
		}
	}
	// A localized read can corroborate an already plausible serial even if
	// neither whole-plate reader is confident on this particular frame.
	localizedRepeat := isNarrowRepeatCrop(p) && p.Localized != nil && len(p.SequenceNumber) == 4 &&
		p.Localized.Number == p.SequenceNumber && p.Localized.Confidence >= .82 && p.NumberScores[p.SequenceNumber] >= .88
	if localizedRepeat {
		primaryMatches := p.Reading.Number == p.SequenceNumber
		t.addEvidence(p.SequenceNumber, serialEvidence{repeatLocalizedSupport: 1,
			repeatPrimaryCue: primaryMatches && p.Reading.Confidence >= .55,
			repeatWeakPrimaryCue: primaryMatches && p.Reading.Confidence >= .40 &&
				p.Localized.Confidence >= .85 && p.NumberScores[p.SequenceNumber] >= .94})
	}
	// Reflections can hide part of one digit in a three-digit serial. Keep the
	// verifier's competing read even when the whole-plate vote favors another.
	if p.Box[2] >= 45 && p.Box[2] <= 65 && p.Score >= .40 && len(p.SequenceNumber) == 3 &&
		p.Verifier != nil && p.Verifier.Number == p.SequenceNumber && p.Verifier.Confidence >= .65 && p.NumberScores[p.SequenceNumber] >= .93 {
		t.rememberChallenger(p.SequenceNumber, sequenceCheck)
	}
	t.hasVerification = t.hasVerification || len(p.NumberScores) > 0
	reading := plate.Normalize(p.Reading)
	valid := func(r plate.Reading) bool {
		return r.Readable() && !math.IsNaN(r.Confidence) && r.Confidence >= minConf
	}
	primaryOK := valid(reading)
	var alternate plate.Reading
	if p.Verifier != nil {
		alternate = plate.Normalize(*p.Verifier)
	}
	alternateOK := valid(alternate)
	if !primaryOK && !alternateOK {
		if localizedRepeat && t.votes[p.SequenceNumber].hasRepeatedNumericAgreement() {
			t.updateConsensus()
		}
		return false
	}
	if p.Verifier == nil {
		t.vote(reading, p, frame, 1, 0)
	} else {
		switch {
		case primaryOK && alternateOK && reading.Number == alternate.Number:
			joint := min(reading.Confidence, alternate.Confidence)
			reading.Confidence = max(reading.Confidence, alternate.Confidence)
			t.vote(reading, p, frame, 1.10, joint)
		case primaryOK && alternateOK:
			reading.Confidence = min(reading.Confidence, .74)
			alternate.Confidence = min(alternate.Confidence, .74)
			t.vote(reading, p, frame, .70, 0)
			t.vote(alternate, p, frame, .90, 0)
		case primaryOK:
			t.vote(reading, p, frame, .70, 0)
		default:
			alternate.Confidence = min(alternate.Confidence, .74)
			t.vote(alternate, p, frame, .90, 0)
		}
	}
	t.reads++
	t.updateConsensus()
	return true
}

func (t *plateTrack) vote(r plate.Reading, p EnginePlate, frame string, factor, joint float64) {
	// Agreement outweighs one exceptionally confident read. Large, clear
	// crops carry more weight and can also supply the representative image.
	weight := (.5 + .5*r.Confidence) * (.85 + .3*min(1, float64(p.Box[2])/100)) * (.9 + .1*p.Score)
	sample := observation{reading: r, box: p.Box, frame: frame, weight: weight, detector: p.Score, appearance: p.appearance, at: t.last}
	if len(t.ocr) == trackOCRWindow {
		copy(t.ocr, t.ocr[1:])
		t.ocr = t.ocr[:trackOCRWindow-1]
	}
	t.ocr = append(t.ocr, sample)
	t.rememberAppearance(sample, p)
	verification := p.NumberScores[r.Number]
	if len(p.NumberScores) > 0 && verification < .70 {
		factor *= max(.27, verification/.70)
	}
	vote := serialEvidence{weight: factor * weight * r.Confidence * r.Confidence, support: 1,
		best: sample, jointConfidence: joint, primaryProof: p.Reading.Number == r.Number && p.Reading.Confidence >= .60}
	if len(r.Number) == 4 {
		vote.digitDetailMeasured = p.DigitEdges != nil
		vote.digitDetailAdequate = p.Box[2] > 55 ||
			p.DigitEdges != nil && *p.DigitEdges >= .08 && !digitBandObscured(p.appearance)
	}
	if len(r.Number) == 4 && p.Box[2] >= 35 && p.Box[2] <= 44 && p.Score >= .58 && readersAgreeOnNumber(p, r.Number, .80, .73, .85) &&
		p.SequenceNumber == r.Number && verification >= .97 {
		vote.shrinkingSeed = sample
	}
	if p.Box[2] >= 40 && 4*p.Box[2] >= 5*p.Box[3] {
		vote.widerCropSupport = 1
	}
	if p.Box[2] >= 35 && p.Score >= .32 && verification >= .96 &&
		p.SequenceNumber == r.Number && p.Reading.Number == r.Number && p.Reading.Confidence >= .65 &&
		p.Verifier != nil && p.Verifier.Number == r.Number && p.Verifier.Confidence >= .70 &&
		p.Localized != nil && p.Localized.Number == r.Number && p.Localized.Confidence >= .78 {
		vote.narrowSupport = 1
		if frame != "" && len(r.Number) == 4 && 4*p.Box[2] >= 5*p.Box[3] && p.Localized.Confidence >= .80 {
			vote.wellShapedFrames[0] = frame
			vote.wellShapedCrop = sample
		}
	}
	// For a narrow crop, a weaker sequence reader can still corroborate the
	// primary read when digit localization agrees on three separate frames.
	if p.Box[2] >= 35 && p.Box[2] < 45 && p.Score >= .43 && verification >= .82 &&
		p.SequenceNumber == r.Number && p.Reading.Number == r.Number && p.Reading.Confidence >= .74 &&
		p.Localized != nil && p.Localized.Number == r.Number && p.Localized.Confidence >= .85 {
		vote.alignedNarrowSupport = 1
	}
	if isNarrowRepeatCrop(p) && len(r.Number) == 4 && verification >= .94 && p.SequenceNumber == r.Number &&
		p.Verifier != nil && p.Verifier.Number == r.Number && p.Verifier.Confidence >= .69 {
		vote.repeatSequenceSupport = 1
	}
	w, h := p.Box[2], p.Box[3]
	// On matching reads joint is the smaller confidence and r.Confidence the larger.
	if len(r.Number) == 4 && w >= 35 && w < 45 && p.Score >= .31 && verification >= .98 && joint >= .75 &&
		joint*r.Confidence >= .64 && p.SequenceNumber == r.Number && p.Reading.Number == r.Number {
		vote.smallCropSupport = 1
		if 4*w >= 5*h {
			vote.smallWideSupport = 1
		}
	}
	// Two different images can confirm a small serial even when their ordinary
	// reads have lower confidence. One crop must also agree with the digit reader.
	if frame != "" && len(r.Number) == 4 && w >= 40 && w < 45 && 5*w >= 6*h && p.Score >= .45 &&
		verification >= .96 && p.SequenceNumber == r.Number &&
		p.Verifier != nil && p.Verifier.Number == r.Number && p.Verifier.Confidence >= .75 {
		vote.smallSequenceFrames[0] = frame
		vote.smallLocalizedProof = verification >= .98 && 2*w >= 3*h &&
			p.Reading.Number == r.Number && p.Reading.Confidence >= .74 &&
			p.Localized != nil && p.Localized.Number == r.Number && p.Localized.Confidence >= .82
	}
	if w >= 45 {
		vote.wideVerification = verification
		if p.SequenceNumber == r.Number && p.Reading.Number == r.Number && p.Score >= .35 {
			vote.sequenceProof = p.Reading.Confidence >= .90 && verification >= .80
			if verification >= .97 && p.Verifier != nil && p.Verifier.Number == r.Number {
				vote.independentProof = p.Reading.Confidence >= .82 && p.Verifier.Confidence >= .78
				vote.localizedProof = p.Score >= .53 && p.Reading.Confidence >= .80 && p.Verifier.Confidence >= .65 &&
					p.Localized != nil && p.Localized.Number == r.Number && p.Localized.Confidence >= .80 &&
					p.Reading.Confidence*p.Verifier.Confidence*p.Localized.Confidence >= .50
			}
		}
	}
	if w >= 60 && p.Score >= .35 && verification >= .80 {
		primary := p.Reading.Number == r.Number && p.Reading.Confidence >= .80
		aligned := p.Localized != nil && p.Localized.Number == r.Number && p.Localized.Confidence >= .82
		vote.regularShape = aligned || primary && (10*w <= 23*h || 10*w <= 26*h && p.Reading.Confidence >= .88)
	}
	if len(r.Number) == 3 && w >= 45 && 5*w >= 8*h && verifiedContinuingRead(p, r.Number) {
		vote.lessObliqueRead = true
	}
	t.addEvidence(r.Number, vote)
	var fields [3]fieldObservation
	tally, voted := t.votes[r.Number]
	minWidth := t.fieldPolicy.MinWidth
	if minWidth == 0 {
		minWidth = plate.FieldMinWidth
	}
	for i, value := range [...]string{r.Region, r.Class, r.Kana} {
		if value != "" && fieldVisible(p.appearance, i) {
			fields[i] = fieldObservation{value: value, weight: weight}
			if voted && p.Box[2] >= minWidth {
				addFieldVotes(&tally.fields[i], value, 1)
			}
		}
	}
	if voted {
		t.votes[r.Number] = tally
	}
	t.rememberFields(r.Number, fields)
}

func addFieldVotes(counts *map[string]int, value string, count int) {
	if *counts == nil {
		*counts = make(map[string]int)
	}
	if _, exists := (*counts)[value]; !exists && len(*counts) >= trackFieldLimit {
		// Keep contradictory reads in the denominator without storing
		// an unbounded set of distinct values on a long track.
		value = ""
	}
	(*counts)[value] += count
}

func (t *plateTrack) addEvidence(number string, incoming serialEvidence) {
	vote, exists := t.votes[number]
	if !exists && len(t.votes) >= trackFieldLimit {
		return
	}
	if t.votes == nil {
		t.votes = make(map[string]serialEvidence)
	}
	if incoming.shrinkingSeed.at.After(vote.shrinkingSeed.at) {
		vote.shrinkingSeed = incoming.shrinkingSeed
	}
	vote.digitDetailMeasured = vote.digitDetailMeasured || incoming.digitDetailMeasured
	vote.digitDetailAdequate = vote.digitDetailAdequate || incoming.digitDetailAdequate
	vote.weight += incoming.weight
	vote.support += incoming.support
	vote.wideVerification = max(vote.wideVerification, incoming.wideVerification)
	vote.jointConfidence = max(vote.jointConfidence, incoming.jointConfidence)
	vote.primaryProof = vote.primaryProof || incoming.primaryProof
	vote.independentProof = vote.independentProof || incoming.independentProof
	vote.sequenceProof = vote.sequenceProof || incoming.sequenceProof
	vote.localizedProof = vote.localizedProof || incoming.localizedProof
	vote.narrowSupport += incoming.narrowSupport
	vote.alignedNarrowSupport += incoming.alignedNarrowSupport
	vote.widerCropSupport += incoming.widerCropSupport
	vote.smallCropSupport += incoming.smallCropSupport
	vote.smallWideSupport += incoming.smallWideSupport
	if incoming.smallSequenceFrames[0] != "" {
		vote.smallSequenceFrames = joinEvidenceFrames(vote.smallSequenceFrames, incoming.smallSequenceFrames)
	}
	vote.smallLocalizedProof = vote.smallLocalizedProof || incoming.smallLocalizedProof
	vote.repeatSequenceSupport += incoming.repeatSequenceSupport
	vote.repeatLocalizedSupport += incoming.repeatLocalizedSupport
	vote.repeatPrimaryCue = vote.repeatPrimaryCue || incoming.repeatPrimaryCue
	vote.repeatWeakPrimaryCue = vote.repeatWeakPrimaryCue || incoming.repeatWeakPrimaryCue
	vote.regularShape = vote.regularShape || incoming.regularShape
	vote.lessObliqueRead = vote.lessObliqueRead || incoming.lessObliqueRead
	if incoming.wellShapedFrames[0] != "" {
		vote.wellShapedFrames = joinEvidenceFrames(vote.wellShapedFrames, incoming.wellShapedFrames)
		if incoming.wellShapedCrop.weight > vote.wellShapedCrop.weight {
			vote.wellShapedCrop = incoming.wellShapedCrop
		}
	}
	for i, counts := range incoming.fields {
		for value, count := range counts {
			addFieldVotes(&vote.fields[i], value, count)
		}
	}
	if incoming.best.weight >= vote.best.weight {
		vote.best = incoming.best
	}
	t.votes[number] = vote
}

func (t *plateTrack) rememberFields(number string, fields [3]fieldObservation) {
	if fields == ([3]fieldObservation{}) {
		return
	}
	old, exists := t.fields[number]
	if !exists && len(t.fields) >= trackFieldLimit {
		return
	}
	if t.fields == nil {
		t.fields = make(map[string][3]fieldObservation)
	}
	for i, field := range fields {
		if field.weight > old[i].weight {
			old[i] = field
		}
	}
	t.fields[number] = old
}

func isNarrowRepeatCrop(p EnginePlate) bool {
	w, h := p.Box[2], p.Box[3]
	return w >= 40 && w < 55 && 10*w >= 15*h && 10*w <= 23*h && p.Score >= .45
}

func (v serialEvidence) hasRepeatedNumericAgreement() bool {
	if v.repeatSequenceSupport >= 4 && v.repeatLocalizedSupport >= 2 && v.repeatPrimaryCue {
		return true
	}
	// A weaker ordinary read needs an extra frame from both the sequence
	// reader and the independently localized digit reader.
	return v.repeatSequenceSupport >= 5 && v.repeatLocalizedSupport >= 3 && v.repeatWeakPrimaryCue
}

func (v serialEvidence) hasDigitDetail() bool {
	return !v.digitDetailMeasured || v.digitDetailAdequate
}

func (t *plateTrack) updateConsensus() {
	// Repeated agreement between independent readers can outweigh a longer
	// run of crops on which only one reader was confident.
	strongAgreement := false
	for _, vote := range t.votes {
		if vote.narrowSupport >= 3 || vote.hasRepeatedNumericAgreement() {
			strongAgreement = true
			break
		}
	}
	var number string
	var winner serialEvidence
	var runner float64
	votedSerials := 0
	for value, vote := range t.votes {
		if vote.support == 0 {
			continue
		}
		votedSerials++
		if strongAgreement && vote.narrowSupport < 3 && !vote.hasRepeatedNumericAgreement() {
			continue
		}
		if vote.weight > winner.weight || (vote.weight == winner.weight && value < number) {
			runner = winner.weight
			number, winner = value, vote
		} else {
			runner = max(runner, vote.weight)
		}
	}
	t.best, t.support = winner.best, winner.support
	// A foreshortened crop can have the highest OCR score while two other
	// images confirm the same serial with a more legible aspect ratio.
	if w, h := t.best.box[2], t.best.box[3]; w < 40 && 5*w < 6*h && winner.widerCropSupport < 2 && winner.wellShapedFrames[1] != "" {
		t.best = winner.wellShapedCrop
	}
	margin := 1.20
	if t.hasVerification {
		margin = 1.32
	}
	t.conflict, t.ambiguous = votedSerials > 1, runner > 0 && runner*margin >= winner.weight
	fields := t.fields[number]
	var recentFields [3]map[string]float64
	for i := range recentFields {
		recentFields[i] = map[string]float64{}
	}
	for _, sample := range t.ocr {
		if sample.reading.Number != number {
			continue
		}
		for i, value := range [...]string{sample.reading.Region, sample.reading.Class, sample.reading.Kana} {
			if value != "" {
				recentFields[i][value] += sample.weight
			}
		}
	}
	for i, value := range [...]*string{&t.best.reading.Region, &t.best.reading.Class, &t.best.reading.Kana} {
		var current string
		for field, weight := range recentFields[i] {
			if weight > recentFields[i][current] || (weight == recentFields[i][current] && field < current) {
				current = field
			}
		}
		if current == "" {
			current = fields[i].value
		}
		*value = current
	}
}

func (t *plateTrack) detection(final bool) (plate.Detection, bool) {
	if t.support == 0 || t.sent == t.frames {
		return plate.Detection{}, false
	}
	support, ambiguous := t.support, t.ambiguous
	verified := !t.hasVerification || t.numberVerified()
	hold := t.sent == 0 && t.waitForOutwardRead()
	if (final || hold) && t.outwardPrior == "" {
		if corrected, ok := t.lateOutwardRead(); ok {
			t.outwardPrior, t.outwardRead = t.best.reading.Number, corrected
		}
	}
	best := t.representative()
	corrected := best.reading.Number != t.best.reading.Number
	if corrected {
		support, ambiguous, verified = 2, false, t.votes[best.reading.Number].hasDigitDetail()
		hold = false
	}
	if final && t.sent == 0 && ambiguous && verified && t.confirmsInwardRead(best) {
		ambiguous = false
	}
	if final && t.sent == 0 && (!verified || ambiguous) {
		if recovered, frames := t.recoverMovingSerial(); frames > 0 {
			best, support, ambiguous, verified = recovered, frames, false, true
		}
	}
	if t.sent == 0 && (ambiguous || (!final && hold)) {
		return plate.Detection{}, false
	}
	if !verified && t.sent == 0 {
		return plate.Detection{}, false
	}
	if t.sent == 0 && support == 1 && !t.plausibleSingleRead() {
		return plate.Detection{}, false
	}
	if !final {
		if t.sent == 0 && !corrected && t.hasVerification && t.last.Sub(t.first) < trackWeakReadGrace {
			// Sparse agreement on a weak crop can precede a separate digit read.
			// Allow that reader to weigh in before the first number is persisted.
			number := best.reading.Number
			if len(number) == 4 && best.box[2] >= 55 && best.box[2] <= 65 && best.detector < .45 {
				vote := t.votes[number]
				if vote.wideVerification < .94 && vote.jointConfidence < .65 && t.localized[number] < .80 {
					return plate.Detection{}, false
				}
			}
		}
		if t.hasVerification && t.sent == 0 && t.best.box[2] <= 65 && t.last.Sub(t.first) < 650*time.Millisecond {
			return plate.Detection{}, false
		}
		if t.sent == 0 && !corrected && (t.support < trackEarlyVotes || t.support <= t.reads/2) {
			return plate.Detection{}, false
		}
		if t.sent != 0 && t.last.Sub(t.sentAt) < trackUpdate {
			return plate.Detection{}, false
		}
	}
	reading := best.reading
	fields := t.votes[reading.Number].fields
	minShare := t.fieldPolicy.MinShare
	if minShare == 0 {
		minShare = plate.DefaultFieldMinShare
	}
	reading.Region = plate.FieldConsensus(fields[0], minShare)
	reading.Class = plate.FieldConsensus(fields[1], minShare)
	reading.Kana = plate.FieldConsensus(fields[2], minShare)
	d := plate.Detection{At: t.first, Reading: reading, Box: best.box, Frame: best.frame,
		Track: &plate.TrackSpan{ID: t.id, LastSeen: t.last, Frames: t.frames, Support: min(support, trackOCRWindow),
			Conflict: t.conflict, Ambiguous: ambiguous || !verified, Update: t.sent != 0, FieldsChecked: true}}
	t.sent, t.sentAt = t.frames, t.last
	t.openingFrames = nil
	return d, true
}

// Recover a narrow plate only when independently localized digits and the
// sequence reader keep agreeing as it moves across the image. The ordinary
// crop reader can lose the same stroke several times during the pass.
func (t *plateTrack) recoverMovingSerial() (observation, int) {
	if t.frames > 40 || t.last.Sub(t.first) > 5*time.Second || len(t.recentFrames) < 3 {
		return observation{}, 0
	}
	// Prefer an independently confirmed frame already retained by the sensor.
	preferredFrame := t.representative().frame
	var best observation
	for _, sample := range t.recentFrames {
		p := sample.plate
		if !movingSerialCrop(sample) || p.Localized == nil || len(p.Localized.Number) != 4 || p.Localized.Confidence < .82 {
			continue
		}
		if best.reading.Number != "" && p.Localized.Number != best.reading.Number {
			return observation{}, 0
		}
		if best.frame == "" || sample.id == preferredFrame ||
			best.frame != preferredFrame && p.Localized.Confidence > best.reading.Confidence {
			best = observation{reading: plate.Reading{Number: p.Localized.Number, Confidence: p.Localized.Confidence},
				box: p.Box, frame: sample.id, at: sample.at, detector: p.Score, appearance: p.appearance}
		}
	}
	if best.reading.Number == "" || !best.reading.Readable() {
		return observation{}, 0
	}
	number := best.reading.Number
	var first, last trackFrame
	var localized, sequence, frames int
	var overlap bool
	for _, sample := range t.recentFrames {
		if !movingSerialCrop(sample) || !sample.at.After(last.at) || sample.id == last.id {
			continue
		}
		p := sample.plate
		local := p.Localized != nil && p.Localized.Number == number && p.Localized.Confidence >= .82
		var sequential bool
		if p.Verifier != nil && p.Verifier.Number == p.SequenceNumber {
			if p.SequenceNumber != number && p.Verifier.Confidence >= .68 && p.NumberScores[p.SequenceNumber] >= .90 {
				return observation{}, 0
			}
			sequential = p.SequenceNumber == number && p.Verifier.Confidence >= .50 && p.NumberScores[number] >= .92
		}
		if !local && !sequential {
			continue
		}
		if frames == 0 {
			first = sample
		}
		last = sample
		frames++
		if local {
			localized++
			overlap = overlap || p.SequenceNumber == number && p.NumberScores[number] >= .80
		}
		if sequential {
			sequence++
		}
	}
	span := last.at.Sub(first.at)
	if localized < 2 || sequence < 3 || !overlap || span < 350*time.Millisecond || span > 2*time.Second {
		return observation{}, 0
	}
	x0, y0 := center(first.plate.Box)
	x1, y1 := center(last.plate.Box)
	side := x0 - FrameWidth/2
	if math.Abs(side) < .15*FrameWidth || side*(x1-x0) <= 0 ||
		math.Abs(x1-x0) < 2*float64(best.box[2]) || y1-y0 < .4*float64(best.box[3]) {
		return observation{}, 0
	}
	best.reading.Confidence = min(best.reading.Confidence, .82)
	return best, frames
}

func movingSerialCrop(sample trackFrame) bool {
	p := sample.plate
	x, y, w, h := p.Box[0], p.Box[1], p.Box[2], p.Box[3]
	return sample.id != "" && w >= 35 && w <= 60 && h > 0 && 10*w >= 9*h && w <= 2*h &&
		x >= 0 && y >= 0 && x+w <= FrameWidth && y+h <= FrameHeight &&
		p.Score >= .35 && p.DigitEdges != nil && *p.DigitEdges >= .16 && sample.minConfidence <= .82
}

func (t *plateTrack) representative() observation {
	if t.outwardPrior != "" && t.best.reading.Number == t.outwardPrior {
		return t.outwardRead
	}
	return t.best
}

// A small plate crossing out of view quickly can repeat the same distorted
// digit before the closer frames reach the OCR. Let those frames arrive first.
func (t *plateTrack) waitForOutwardRead() bool {
	w, h := t.best.box[2], t.best.box[3]
	if !t.hasVerification || len(t.best.reading.Number) != 4 || w < 40 || w >= 55 || 3*w > 5*h ||
		t.box[2] >= 55 || t.localizedFrames[t.best.reading.Number][1] != "" {
		return false
	}
	x, _ := center(t.best.box)
	initial, _ := center(t.firstBox)
	side := x - FrameWidth/2
	return math.Abs(side) >= .23*FrameWidth && side*(x-initial) > 0 &&
		math.Abs(x-initial) >= 2.3*float64(w) && side*t.velocity[0] > 0 &&
		math.Abs(t.velocity[0]) >= 3*float64(w)
}

// Two later crops can recover a digit distorted in a narrow oblique view.
// Require the original car's appearance and outward motion at both samples.
func (t *plateTrack) lateOutwardRead() (observation, bool) {
	current := t.best
	if !t.hasVerification || t.support < 3 || len(current.reading.Number) != 4 ||
		current.box[2] < 40 || current.box[2] >= 55 || len(current.appearance) != appearanceWidth*appearanceHeight {
		return observation{}, false
	}
	cx, _ := center(current.box)
	side := cx - FrameWidth/2
	if math.Abs(side) < .23*FrameWidth {
		return observation{}, false
	}
	var result observation
	for i, sample := range t.recentFrames {
		p := sample.plate
		number := p.Reading.Number
		age := sample.at.Sub(current.at)
		if age < 350*time.Millisecond || age > 1250*time.Millisecond || t.last.Sub(sample.at) > 500*time.Millisecond ||
			sample.id == "" || p.Box[2] < 40 || p.Box[2] >= 55 || 4*p.Box[2] > 5*p.Box[3] || p.Score < .50 ||
			!oneDigitDiffers(number, current.reading.Number) || p.SequenceNumber != number || p.NumberScores[number] < .98 ||
			!readersAgreeOnNumber(p, number, max(.88, sample.minConfidence), .65, .78) || len(p.appearance) != len(current.appearance) {
			continue
		}
		x, _ := center(p.Box)
		if side*(x-cx) <= 0 || math.Abs(x-cx) < 2.3*float64(current.box[2]) ||
			!appearanceMatches(current.appearance, p.appearance, 7, 1, 34, 16, 14, 4, .70) ||
			!appearanceMatches(current.appearance, p.appearance, 5, 5, 37, 19, 11, 9, .82) {
			continue
		}
		if !corroboratesOutwardRead(side, sample, t.recentFrames[i+1:]) {
			continue
		}
		if result.reading.Number != "" && result.reading.Number != number {
			return observation{}, false
		}
		if p.Reading.Confidence > result.reading.Confidence {
			result = observation{reading: p.Reading, box: p.Box, frame: sample.id, at: sample.at, detector: p.Score, appearance: p.appearance}
		}
	}
	return result, result.reading.Number != ""
}

func readersAgreeOnNumber(p EnginePlate, number string, primary, verifier, localized float64) bool {
	return p.Reading.Number == number && p.Reading.Confidence >= primary &&
		p.Verifier != nil && p.Verifier.Number == number && p.Verifier.Confidence >= verifier &&
		p.Localized != nil && p.Localized.Number == number && p.Localized.Confidence >= localized
}

// As a plate turns toward the camera, separately localized digits in two
// views and an agreeing intervening view can resolve an initially oblique read.
func (t *plateTrack) confirmsInwardRead(chosen observation) bool {
	number := chosen.reading.Number
	w, h := chosen.box[2], chosen.box[3]
	if !t.hasVerification || len(number) != 4 || t.support < 3 || t.frames > 10 ||
		t.last.Sub(t.first) > time.Second || w < 45 || w > 65 || 5*w < 7*h ||
		10*t.firstBox[2] > 11*t.firstBox[3] || t.votes[number].narrowSupport < 2 {
		return false
	}
	initial, _ := center(t.firstBox)
	current, _ := center(chosen.box)
	side := initial - FrameWidth/2
	if math.Abs(side) < .25*FrameWidth || side*(current-initial) >= 0 || math.Abs(current-initial) < 3*float64(w) {
		return false
	}
	competitors := 0
	for serial, vote := range t.votes {
		if serial == number || vote.support == 0 {
			continue
		}
		competitors++
		if competitors > 1 || !oneDigitDiffers(serial, number) || vote.narrowSupport > 0 {
			return false
		}
	}
	if competitors != 1 {
		return false
	}

	var anchor, previous trackFrame
	evidence := 0
	localizedEvidence := 0
	for _, sample := range t.recentFrames {
		view := sample.plate
		if sample.id == "" || view.Reading.Number != number || view.Reading.Confidence < .80 ||
			view.Verifier == nil || view.Verifier.Number != number || view.Verifier.Confidence < .75 ||
			view.Score < .50 || view.SequenceNumber != number || view.NumberScores[number] < .98 ||
			len(view.appearance) != appearanceWidth*appearanceHeight || view.Box[2] < view.Box[3] {
			continue
		}
		localized := view.Localized != nil && view.Localized.Number == number && view.Localized.Confidence >= .81
		if evidence == 0 {
			if localized {
				anchor, previous = sample, sample
				evidence = 1
				localizedEvidence = 1
			}
			continue
		}
		span := sample.at.Sub(anchor.at)
		gap := sample.at.Sub(previous.at)
		if sample.id == previous.id || span > 625*time.Millisecond || gap <= 0 || gap > 300*time.Millisecond {
			continue
		}
		x, y := center(view.Box)
		px, py := center(previous.plate.Box)
		dx := x - px
		if side*dx >= 0 || math.Abs(dx) > 3*float64(previous.plate.Box[2]) ||
			math.Abs(y-py) > .52*float64(previous.plate.Box[3]) ||
			!appearanceMatches(previous.plate.appearance, view.appearance, 7, 1, 34, 16, 14, 4, .62) ||
			!appearanceMatches(previous.plate.appearance, view.appearance, 5, 5, 37, 19, 11, 9, .72) {
			continue
		}
		evidence++
		if localized {
			localizedEvidence++
		}
		if evidence >= 3 && localizedEvidence >= 2 && 5*view.Box[2] >= 7*view.Box[3] &&
			view.Box[3] < anchor.plate.Box[3] && span >= 250*time.Millisecond {
			return true
		}
		previous = sample
	}
	return false
}

func corroboratesOutwardRead(side float64, sample trackFrame, later []trackFrame) bool {
	p := sample.plate
	x, y := center(p.Box)
	for _, following := range later {
		gap := following.at.Sub(sample.at)
		if gap > 250*time.Millisecond {
			break
		}
		q := following.plate
		if gap <= 0 || following.id == "" || following.id == sample.id || q.Box[2] < 35 || q.Box[2] >= 55 || q.Score < .35 ||
			!readersAgreeOnNumber(q, p.Reading.Number, max(.72, following.minConfidence), .55, .87) || len(q.appearance) != len(p.appearance) {
			continue
		}
		sx, sy := center(q.Box)
		dx := sx - x
		if side*dx > 0 && math.Abs(dx) >= .4*float64(p.Box[2]) && math.Abs(dx) <= 1.8*float64(p.Box[2]) &&
			math.Abs(sy-y) <= .45*float64(p.Box[3]) &&
			appearanceMatches(p.appearance, q.appearance, 7, 1, 34, 11, 14, 4, .75) &&
			appearanceMatches(p.appearance, q.appearance, 5, 5, 37, 19, 11, 9, .72) {
			return true
		}
	}
	return false
}

// A clearly read crop can anchor two smaller reads after a track is joined.
func (t *plateTrack) verifiedShrinkingRead(number string) bool {
	seed := t.votes[number].shrinkingSeed
	if seed.frame == "" {
		return false
	}
	p := seed.box
	x0, _ := center(p)
	side := x0 - FrameWidth/2
	if math.Abs(side) < .14*FrameWidth {
		return false
	}
	previousBox, previousAt, previousID := p, seed.at, seed.frame
	count := 0
	for _, candidate := range t.recentFrames {
		q := candidate.plate
		if candidate.at.Sub(seed.at) > 500*time.Millisecond {
			break
		}
		gap := candidate.at.Sub(previousAt)
		if gap <= 0 || gap > 250*time.Millisecond || candidate.id == "" || candidate.id == previousID ||
			q.Box[2] < 32 || q.Box[2] > 34 || q.Score < .50 || !readersAgreeOnNumber(q, number, .75, .70, .87) ||
			q.SequenceNumber != number || q.NumberScores[number] < .96 {
			continue
		}
		x, y := center(q.Box)
		ox, oy := center(previousBox)
		dx := x - ox
		if side*dx >= 0 || math.Abs(dx) < .25*float64(p[2]) || math.Abs(dx) > 1.35*float64(p[2]) ||
			math.Abs(y-oy) > .40*float64(p[3]) {
			continue
		}
		previousBox, previousAt, previousID = q.Box, candidate.at, candidate.id
		count++
		if count == 2 {
			return true
		}
	}
	return false
}

func (t *plateTrack) numberVerified() bool {
	image := t.best
	vote := t.votes[image.reading.Number]
	// The same blurred digits can agree across OCR models and frames.
	// Require at least one supporting view with measurable numeral detail.
	if !vote.hasDigitDetail() {
		return false
	}
	w, h := image.box[2], image.box[3]
	// A tiny, nearly square crop can repeat the same distorted digits.
	// Require two votes for that serial from wider views of the plate.
	if w < 40 && 5*w < 6*h && vote.widerCropSupport < 2 {
		return false
	}
	// On a single distant frame, require the reader using separate digit crops
	// to corroborate the serial strongly before publishing it.
	if t.frames == 1 && t.support == 1 && image.box[2] < 48 && t.localized[image.reading.Number] < .90 {
		return false
	}
	// The verifier and its CTC output share a model. Bright reflections can
	// remove the same strokes from both, even when they agree over many frames.
	corroborated := vote.primaryProof || t.localized[image.reading.Number] >= .85 || vote.hasRepeatedNumericAgreement()
	if !corroborated || t.localizedConflicts(image) || t.hasWiderReadConflict(image) {
		return false
	}
	if w >= 32 && w <= 44 && len(image.reading.Number) == 4 && t.support >= 3 && t.verifiedShrinkingRead(image.reading.Number) {
		return true
	}
	// A growing, foreshortened plate crossing toward the edge can repeat
	// the same distorted digit. The newest box reveals this before the
	// highest-confidence OCR crop becomes foreshortened itself.
	if t.isObliqueEdgeApproach(image) && t.localized[image.reading.Number] < .90 && !vote.lessObliqueRead {
		return false
	}
	// Allow a narrow plate only after separate frames agree across readers.
	if vote.narrowSupport >= 2 || vote.alignedNarrowSupport >= 3 || vote.hasRepeatedNumericAgreement() {
		return true
	}
	// A small plate can stay below the wide-crop threshold throughout a track.
	// Require three agreeing frames and two sufficiently wide aspect ratios.
	if w >= 35 && w < 45 && vote.smallCropSupport >= 3 && vote.smallWideSupport >= 2 && t.support > t.reads/2 {
		return true
	}
	if w >= 40 && w < 45 && vote.smallSequenceFrames[1] != "" && vote.smallLocalizedProof && t.support > t.reads/2 {
		return true
	}
	// A reflection can make whole-plate readers agree on the same wrong digit.
	// When multiple frames support only other serials, require a separate digit crop.
	if t.conflict && len(image.reading.Number) == 4 && w >= 45 && w < 80 &&
		t.reads-vote.support >= 2 && t.localized[image.reading.Number] < .78 {
		return false
	}
	// Repeating a faint read from a distant crop can reinforce the same error.
	// Near the verification floor, require one reader to corroborate the winning serial.
	if w < 55 && vote.wideVerification < .85 && vote.jointConfidence < .72 &&
		t.localized[image.reading.Number] < .85 && !vote.sequenceProof && !vote.independentProof && !vote.localizedProof {
		return false
	}
	if vote.wideVerification < .80 {
		return false
	}
	confidence := image.reading.Confidence
	// Shop signs and bumper stickers can produce repeat reads in long, thin
	// boxes. Require an aligned digit read or a stronger crop of the same serial.
	if w >= 85 && 10*w >= 24*h && !vote.regularShape {
		return false
	}
	noFields := image.reading.Region == "" && image.reading.Class == "" && image.reading.Kana == ""
	// A weak detection of a large object can turn reflections into a serial
	// and plausible optional fields with only a few supporting frames.
	weakWideBox := w >= 100 && image.detector < .40 && t.localized[image.reading.Number] < .85
	singleShortRead := t.support == 1 && len(image.reading.Number) <= 2
	if weakWideBox && (singleShortRead || (t.support <= 2 && confidence < .80)) {
		return false
	}
	if t.support <= 3 && w <= 65 && image.detector < .39 &&
		!vote.independentProof && !vote.localizedProof && vote.wideVerification < .95 {
		return false
	}
	weakGeometry := w*10 < 14*h && image.detector > 0 && image.detector < .38 && noFields
	if t.support <= 2 && weakGeometry && !vote.independentProof && !vote.sequenceProof {
		return false
	}
	if vote.independentProof || vote.localizedProof || (vote.sequenceProof && t.support >= 2) {
		return true
	}
	if t.support == 1 {
		return confidence >= .85 && (w >= 60 || vote.jointConfidence >= .82)
	}
	if t.support == 2 && w < 50 && confidence < .875 {
		return false
	}
	return t.support > 3 || w >= 55 || vote.jointConfidence >= .72 || (confidence >= .995 && w >= 50)
}

func (t *plateTrack) isObliqueEdgeApproach(image observation) bool {
	w, h := t.box[2], t.box[3]
	if len(image.reading.Number) != 3 || image.box[2] >= 55 || w < 35 || w >= 55 || 2*w > 3*h {
		return false
	}
	if firstWidth := t.firstBox[2]; firstWidth <= 0 || 2*w < 3*firstWidth {
		return false
	}
	firstX, _ := center(t.firstBox)
	x, _ := center(t.box)
	side := x - FrameWidth/2
	dx := x - firstX
	return math.Abs(side) >= .18*FrameWidth && dx*side > 0 && math.Abs(dx) >= 1.5*float64(w)
}

func (t *plateTrack) rememberLocalized(number string, confidence float64) {
	prior, exists := t.localized[number]
	if confidence <= prior || (!exists && len(t.localized) >= trackFieldLimit) {
		return
	}
	if t.localized == nil {
		t.localized = make(map[string]float64)
	}
	t.localized[number] = confidence
}

func (t *plateTrack) rememberLocalizedFrame(number, frame string) {
	if frame == "" {
		return
	}
	prior, exists := t.localizedFrames[number]
	if prior[1] != "" || prior[0] == frame || (!exists && len(t.localizedFrames) >= trackFieldLimit) {
		return
	}
	if t.localizedFrames == nil {
		t.localizedFrames = make(map[string][2]string)
	}
	t.localizedFrames[number] = joinEvidenceFrames(prior, [2]string{frame})
}

func joinEvidenceFrames(saved, incoming [2]string) [2]string {
	for _, frame := range incoming {
		if saved[1] != "" {
			break
		}
		if frame == "" || frame == saved[0] {
			continue
		}
		if saved[0] == "" {
			saved[0] = frame
		} else {
			saved[1] = frame
		}
	}
	return saved
}

func (t *plateTrack) localizedConflicts(best observation) bool {
	chosen := best.reading.Number
	if len(chosen) == 3 && best.box[2] >= 45 && best.box[2] <= 65 && t.localized[chosen] < .80 {
		for alternative, frames := range t.localizedFrames {
			if frames[1] != "" && t.challengers[alternative]&sequenceCheck != 0 && oneDigitDiffers(alternative, chosen) {
				return true
			}
		}
	}
	// A repeated disagreement from the digit reader can invalidate the whole-plate
	// consensus even when the representative crop is too wide for the narrow checks.
	if len(chosen) == 4 && best.box[2] >= 50 && t.localized[chosen] < .80 {
		for alternative, frames := range t.localizedFrames {
			if frames[1] != "" && oneDigitDiffers(alternative, chosen) {
				return true
			}
		}
	}
	if best.box[2] > 65 || len(chosen) != 4 {
		return false
	}
	// In a weak detection, independently supported evidence for a competing
	// digit can be split between frames and fall below the voting threshold.
	if best.detector < .45 {
		for number, checks := range t.challengers {
			if checks == localizedCheck|sequenceCheck && oneDigitDiffers(number, chosen) {
				return true
			}
		}
	}
	if t.localized[chosen] >= .80 {
		return false
	}
	for number, confidence := range t.localized {
		if confidence >= .90 && oneDigitDiffers(number, chosen) {
			return true
		}
	}
	return false
}

func (t *plateTrack) rememberChallenger(number string, checks serialCheck) {
	if len(number) < 3 || len(number) > 4 || !(plate.Reading{Number: number}).Readable() ||
		(len(t.challengers) >= trackFieldLimit && t.challengers[number] == 0) {
		return
	}
	if t.challengers == nil {
		t.challengers = make(map[string]serialCheck)
	}
	t.challengers[number] |= checks
}

func oneDigitDiffers(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	differences := 0
	for i := range a {
		if a[i] != b[i] {
			differences++
		}
	}
	return differences == 1
}

func (t *plateTrack) plausibleSingleRead() bool {
	box := t.best.box
	x, _ := center(box)
	w, h := float64(box[2]), float64(box[3])
	// Fixtures can have plate-like digits with unusual proportions. Near the
	// edge a passing vehicle can also show a tall, foreshortened plate.
	portrait := w < h && x > .10*FrameWidth && x < .90*FrameWidth
	weakLarge := w >= 100 && t.best.detector > 0 && t.best.detector < .30
	return w <= 2.55*h && !portrait && !weakLarge
}

func center(box [4]int) (float64, float64) {
	return float64(box[0]) + float64(box[2])/2, float64(box[1]) + float64(box[3])/2
}

// Some detector regions give overlapping boxes for one plate. Prior frame
// assignments and distinct strong serials can establish separate vehicles.
func collapseOverlappingDetections(detections []EnginePlate, assigned []int) ([]EnginePlate, []int) {
	if len(detections) < 2 {
		return detections, assigned
	}
	kept := detections[:0]
	tracks := assigned[:0]
	for j, detection := range detections {
		a := detection.Box
		track := assigned[j]
		merged := false
		for i, previous := range kept {
			if track >= 0 && tracks[i] >= 0 && track != tracks[i] {
				continue
			}
			b := previous.Box
			intersection := max(0, min(a[0]+a[2], b[0]+b[2])-max(a[0], b[0])) * max(0, min(a[1]+a[3], b[1]+b[3])-max(a[1], b[1]))
			left, right := detection.Reading, previous.Reading
			union := a[2]*a[3] + b[2]*b[3] - intersection
			if float64(intersection) < .40*float64(union) {
				// A broad ROI can propose a second, vertically displaced crop
				// on an already matched plate. Require contained image area
				// and an uncertain OCR on the unmatched box.
				weak, strong := left, right
				if track >= 0 {
					weak, strong = right, left
				}
				ax, ay := center(a)
				bx, by := center(b)
				if track == tracks[i] || weak.Confidence >= .60 || strong.Confidence < .76 ||
					float64(intersection) < .60*float64(min(a[2]*a[3], b[2]*b[3])) ||
					float64(intersection) < .34*float64(union) ||
					math.Abs(ax-bx) > .15*float64(max(a[2], b[2])) ||
					math.Abs(ay-by) > .46*float64(max(a[3], b[3])) {
					continue
				}
			}
			if left.Number != "" && right.Number != "" && left.Number != right.Number && min(left.Confidence, right.Confidence) >= .75 {
				continue
			}
			if .65*detection.Score+.35*left.Confidence > .65*previous.Score+.35*right.Confidence {
				kept[i] = detection
			}
			tracks[i] = max(tracks[i], track)
			merged = true
			break
		}
		if !merged {
			kept = append(kept, detection)
			tracks = append(tracks, track)
		}
	}
	return kept, tracks
}

func proximity(previous, next [4]int, dx, dy float64) float64 {
	w, h := float64(previous[2]), float64(previous[3])
	nw, nh := float64(next[2]), float64(next[3])
	if ratio := nw * nh / (w * h); ratio < .45 || ratio > 2.2 {
		return 0
	}
	x, y := float64(previous[0])+dx, float64(previous[1])+dy
	nx, ny := float64(next[0]), float64(next[1])
	intersection := max(0, min(x+w, nx+nw)-max(x, nx)) * max(0, min(y+h, ny+nh)-max(y, ny))
	iou := intersection / (w*h + nw*nh - intersection)
	distance := math.Hypot((x+w/2-nx-nw/2)/max(w, nw), (y+h/2-ny-nh/2)/max(h, nh))
	if iou < .30 && distance > .48 {
		return 0
	}
	return iou + .35*max(0, 1-distance)
}

func (t *plateTrack) match(at time.Time, p EnginePlate) float64 {
	dt := at.Sub(t.last).Seconds()
	r := p.Reading
	// A new track has little motion history. After several missed captures,
	// a different passing vehicle can occupy almost the same image position.
	if dt > .5 && t.frames < 3 && t.misses >= 2 && stationaryDistance(t.box, p.Box) > .25 {
		previous := t.best.reading
		if r.Number == "" || r.Number != previous.Number || min(r.Confidence, previous.Confidence) < .75 {
			return 0
		}
	}
	var score float64
	if !t.missed(at) && at.Before(t.finished.Add(trackGap)) {
		score = max(proximity(t.box, p.Box, 0, 0), proximity(t.box, p.Box, t.velocity[0]*dt, t.velocity[1]*dt))
		if score == 0 {
			score = max(t.fastPassingMatch(dt, p), t.projectedPass(dt, p))
		}
		if score == 0 {
			score = max(t.correctingInwardPass(dt, p), t.outwardChallengerMatch(dt, p))
		}
	}
	if score > 0 && t.support >= 2 && r.Number == t.best.reading.Number && r.Confidence >= .7 {
		score += .25 + .08*float64(matchingFields(r, t.best.reading))
	}
	return score
}

func (t *plateTrack) missed(at time.Time) bool {
	return t.misses >= trackMaxMisses && at.Sub(t.last) >= trackMissWindow
}

// Near the edge of the image a passing plate can move more than its width
// between captures. Give that trajectory less weight than a close match.
func (t *plateTrack) fastPassingMatch(dt float64, p EnginePlate) float64 {
	if dt <= 0 || dt > .5 {
		return 0
	}
	x, y := center(t.box)
	nx, ny := center(p.Box)
	dx, dy := nx-x, ny-y
	w, h := float64(t.box[2]), float64(t.box[3])
	nw, nh := float64(p.Box[2]), float64(p.Box[3])
	if math.Abs(x-FrameWidth/2) < 150 || (x-FrameWidth/2)*dx <= 0 ||
		nw/w < .6 || nw/w > 1.75 || nh/h < .55 || nh/h > 1.9 ||
		math.Abs(dy) > .65*max(h, nh) {
		return 0
	}
	old, current := t.best.reading, p.Reading
	if old.Confidence >= .84 && current.Confidence >= .84 && old.Number != current.Number {
		return 0
	}
	allowance := min(172, max(w, nw)*2.6) * min(1, dt/.25)
	shift := math.Abs(dx)
	if t.frames > 1 {
		if t.velocity[0]*dx < 0 {
			return 0
		}
		shift = min(shift, .45*shift+math.Abs(dx-t.velocity[0]*dt))
	}
	if shift > allowance {
		return 0
	}
	return .17 - .06*shift/allowance
}

// A confirmed plate moving toward the edge can pass out of the detector's
// range and reappear farther away than the ordinary short-gap allowance.
// Reuse its measured velocity when the predicted position and size agree.
func (t *plateTrack) projectedPass(dt float64, p EnginePlate) float64 {
	if dt <= 0 || dt > .35 || t.frames < 4 || t.support < 2 || math.Abs(t.velocity[0]) < 170 {
		return 0
	}
	x, y := center(t.box)
	nx, ny := center(p.Box)
	dx, dy := nx-x, ny-y
	w, h := float64(t.box[2]), float64(t.box[3])
	nw, nh := float64(p.Box[2]), float64(p.Box[3])
	if (x-FrameWidth/2)*dx <= 0 || nw/w < .64 || nw/w > 1.60 || nh/h < .63 || nh/h > 1.65 {
		return 0
	}
	expectedX, expectedY := t.velocity[0]*dt, t.velocity[1]*dt
	if dx*expectedX <= 0 || math.Abs(dx-expectedX) > 1.30*max(w, nw) ||
		math.Abs(dy-expectedY) > .60*max(h, nh) {
		return 0
	}
	old, current := t.best.reading, p.Reading
	if current.Confidence >= .70 && current.Number != "" && old.Number != current.Number {
		if len(old.Number) != 4 || len(current.Number) != 4 {
			return 0
		}
		differences := 0
		for i := range old.Number {
			if old.Number[i] != current.Number[i] {
				differences++
			}
		}
		if differences != 1 || current.Confidence >= .88 && matchingFields(old, current) == 0 {
			return 0
		}
	}
	return .115
}

// Continue an unpublished track moving toward the image center when the last
// two frames independently support the same correction to its earlier vote.
func (t *plateTrack) correctingInwardPass(dt float64, p EnginePlate) float64 {
	number := p.Reading.Number
	if t.sent != 0 || t.frames < 3 || dt <= 0 || dt > .25 || len(t.recentFrames) == 0 ||
		len(number) != 4 || !oneDigitDiffers(number, t.best.reading.Number) || !verifiedContinuingRead(p, number) {
		return 0
	}
	last := t.recentFrames[len(t.recentFrames)-1]
	if !last.at.Equal(t.last) || !verifiedContinuingRead(last.plate, number) {
		return 0
	}
	x, y := center(t.box)
	nx, ny := center(p.Box)
	dx, dy := nx-x, ny-y
	side := x - FrameWidth/2
	w, h := float64(t.box[2]), float64(t.box[3])
	nw, nh := float64(p.Box[2]), float64(p.Box[3])
	if math.Abs(side) < .22*FrameWidth || side*dx >= 0 || side*(nx-FrameWidth/2) < 0 ||
		nw/w < .78 || nw/w > 1.18 || nh/h < .72 || nh/h > 1.12 ||
		dx*t.velocity[0] <= 0 || math.Abs(t.velocity[0]) < 200 ||
		math.Abs(dx-t.velocity[0]*dt) > .70*max(w, nw) || math.Abs(dy-t.velocity[1]*dt) > .60*max(h, nh) {
		return 0
	}
	return .10
}

func verifiedContinuingRead(p EnginePlate, number string) bool {
	return p.Reading.Number == number && p.Reading.Confidence >= .80 && p.Verifier != nil &&
		p.Verifier.Number == number && p.Verifier.Confidence >= .80 && p.SequenceNumber == number &&
		p.NumberScores[number] >= .95
}

// A growing plate moving toward an image edge can accelerate beyond the
// ordinary motion gate. Allow its wider, conflicting read to reach the track
// when the size and trajectory agree with the immediately preceding frames.
func (t *plateTrack) outwardChallengerMatch(dt float64, p EnginePlate) float64 {
	if t.sent != 0 || t.support < 2 || t.support > 3 || t.frames < 2 || dt <= 0 || dt > .25 ||
		!widerConflictingRead(t.best, p) {
		return 0
	}
	// A join between two single observations can retain only the newer crop.
	previous := t.firstBox
	if n := len(t.recentFrames); n >= 2 {
		previous = t.recentFrames[n-2].plate.Box
	} else if t.frames != 2 {
		return 0
	}
	x, y := center(t.box)
	nx, ny := center(p.Box)
	dx, dy := nx-x, ny-y
	side := x - FrameWidth/2
	w, h := float64(p.Box[2]), float64(p.Box[3])
	ratioX, ratioY := w/float64(t.box[2]), h/float64(t.box[3])
	if math.Abs(side) < .15*FrameWidth || side*dx <= 0 || dx*t.velocity[0] <= 0 || dy*t.velocity[1] <= 0 ||
		math.Abs(t.velocity[0]) < 250 || ratioX < 1.14 || ratioX > 1.75 || ratioY < 1.13 || ratioY > 1.70 || previous[2] <= 0 {
		return 0
	}
	scale := min(1.75, w/float64(previous[2]))
	if math.Abs(dx-t.velocity[0]*dt*scale) > .65*w || math.Abs(dy-t.velocity[1]*dt*scale) > .50*h {
		return 0
	}
	return .10
}

func widerConflictingRead(best observation, p EnginePlate) bool {
	if len(best.reading.Number) != 4 || best.box[2] >= 50 || !oneDigitDiffers(p.Reading.Number, best.reading.Number) ||
		float64(p.Box[2]) < 1.20*float64(best.box[2]) || p.Score < .40 || p.Localized == nil || p.Verifier == nil {
		return false
	}
	return p.Localized.Number == p.Reading.Number && p.Verifier.Number == p.Reading.Number &&
		p.Localized.Confidence >= .88 && p.Reading.Confidence >= .40 && p.Verifier.Confidence >= .40
}

// A weak but corroborated digit in a substantially wider crop can make the
// shorter track uncertain. This check cannot turn that read into a vote.
func (t *plateTrack) hasWiderReadConflict(best observation) bool {
	if t.sent != 0 || t.support > 3 || len(best.reading.Number) != 4 || best.box[2] >= 50 {
		return false
	}
	for i := len(t.recentFrames) - 1; i >= 0; i-- {
		current := t.recentFrames[i]
		age := current.at.Sub(best.at)
		if age <= 0 {
			break
		}
		if age <= 500*time.Millisecond && widerConflictingRead(best, current.plate) {
			return true
		}
	}
	return false
}

func matchingFields(a, b plate.Reading) int {
	count := 0
	for _, pair := range [3][2]string{{a.Region, b.Region}, {a.Class, b.Class}, {a.Kana, b.Kana}} {
		if pair[0] != "" && pair[0] == pair[1] {
			count++
		}
	}
	return count
}

// canRejoin combines fragments with an exact serial reading and plausible
// image displacement. Geometry alone remains
// sufficient for nearby frames whose OCR is unreadable or temporarily wrong.
func (t *plateTrack) canRejoin(next *plateTrack) bool {
	gap := next.first.Sub(t.last)
	if t == next || next.sent != 0 || t.ambiguous || next.ambiguous ||
		t.support == 0 || next.support == 0 || t.best.reading.Number != next.best.reading.Number {
		return false
	}
	conflicts := 0
	for _, pair := range [3][2]string{{t.best.reading.Region, next.best.reading.Region},
		{t.best.reading.Class, next.best.reading.Class}, {t.best.reading.Kana, next.best.reading.Kana}} {
		if pair[0] != "" && pair[1] != "" && pair[0] != pair[1] {
			conflicts++
		}
	}
	if gap <= 0 && gap >= -2*time.Second && t.misses > 0 && next.last.After(t.last) && len(t.best.reading.Number) == 4 &&
		min(t.support, next.support) >= 3 && min(t.best.reading.Confidence, next.best.reading.Confidence) >= .80 &&
		t.hasVerification && next.hasVerification && t.numberVerified() && next.numberVerified() &&
		len(next.overlapFrames(t.last)) > 0 && sameFrameBoxes(t, next) {
		return true
	}
	if gap > 0 && gap <= 250*time.Millisecond && conflicts == 0 && t.canRejoinInwardPass(next) {
		return true
	}
	if gap > trackRecentGap {
		_, confirmedConflicts := t.compareConfirmedFields(next)
		if confirmedConflicts >= 2 {
			return false
		}
	}
	if t.rejoinFollowing(next, gap) {
		return true
	}
	if t.rejoinAfterTurn(next, gap) {
		return true
	}
	if t.rejoinLongFollowing(next, gap) {
		return true
	}
	if t.rejoinFromDistantBoxes(next, gap) {
		return true
	}
	if t.rejoinReturningAtEdge(next, gap) {
		return true
	}
	if t.rejoinWeakSideFragment(next, gap) {
		return true
	}
	if t.rejoinByAppearance(next, gap) {
		return true
	}
	if gap < -175*time.Millisecond || gap > trackRecentGap {
		return false
	}
	if gap <= 0 {
		return t.canRejoinOverlap(next, conflicts)
	}
	w, h := float64(t.box[2]), float64(t.box[3])
	nw, nh := float64(next.firstBox[2]), float64(next.firstBox[3])
	x, y := center(t.box)
	nx, ny := center(next.firstBox)
	dt := min(gap.Seconds(), 1)
	xAllowance := max(2*max(w, nw), 60+420*dt)
	yAllowance := max(max(h, nh), 20+120*dt)
	dx, dy := nx-x, ny-y
	older, newer := t.best.reading, next.best.reading
	// A fast pass near the edge may leave a fragment too short to estimate
	// velocity. Both independent reads and outward motion must agree.
	clearFourDigitPass := len(older.Number) == 4 && conflicts <= 1 && max(older.Confidence, newer.Confidence) >= .92
	if gap <= 250*time.Millisecond && min(t.support, next.support) <= 2 &&
		min(older.Confidence, newer.Confidence) >= .80 &&
		t.hasVerification && next.hasVerification && t.numberVerified() && next.numberVerified() &&
		math.Abs(x-FrameWidth/2) >= .18*FrameWidth && (x-FrameWidth/2)*dx > 0 &&
		math.Abs(dx) >= .50*max(w, nw) && math.Abs(dx) <= 2.35*max(w, nw) &&
		dy >= -.20*max(h, nh) && dy <= 1.05*max(h, nh) &&
		nw/w >= .72 && nw/w <= 1.65 && nh/h >= .65 && nh/h <= 1.65 &&
		(clearFourDigitPass || t.matchFastSingleFrame(next, gap, conflicts)) {
		return true
	}
	// A short serial needs matching metadata and independently verified reads
	// on both sides before allowing the wider motion-predicted displacement.
	verifiedShort := gap <= 200*time.Millisecond && len(older.Number) == 3 && t.support >= 2 &&
		min(older.Confidence, newer.Confidence) >= .88 && conflicts == 0 && matchingFields(older, newer) > 0 &&
		t.hasVerification && next.hasVerification && t.numberVerified() && next.numberVerified()
	verifiedLong := len(older.Number) == 4 && t.support >= 3
	if gap <= 400*time.Millisecond && (verifiedLong || verifiedShort) && older.Confidence >= .78 &&
		newer.Confidence >= .75 && nw/w >= .64 && nw/w <= 1.60 && nh/h >= .63 && nh/h <= 1.65 &&
		math.Abs(t.velocity[0]) >= 135 && dx*t.velocity[0] > 0 &&
		math.Abs(dx-t.velocity[0]*gap.Seconds()) <= 1.30*max(w, nw) &&
		math.Abs(dy-t.velocity[1]*gap.Seconds()) <= .70*max(h, nh) {
		return true
	}
	if gap > trackRejoinGap || conflicts >= 2 {
		// Compare both boundary boxes and the boxes carrying the accepted
		// readings. The latter help when an unreadable edge drifts on the car.
		edgeDistance := stationaryDistance(t.box, next.firstBox)
		readDistance := stationaryDistance(t.best.box, next.best.box)
		distance := min(edgeDistance, readDistance)
		confidence := min(older.Confidence, newer.Confidence)
		if gap <= 7*time.Second && min(t.frames, next.frames) >= 3 && conflicts < 2 && len(older.Number) == 4 && confidence >= .85 &&
			max(edgeDistance, readDistance) <= .22 {
			return true
		}
		if min(t.support, next.support) < 2 || confidence < .82 || edgeDistance > 1.65 {
			return false
		}
		if len(older.Number) <= 2 {
			corroborated := older.Region != "" && older.Region == newer.Region || older.Class != "" && older.Class == newer.Class
			return confidence >= .89 && (distance <= .45 && gap <= trackRejoinGap || distance <= .85 && corroborated)
		}
		return distance <= .38 || confidence >= .86 && distance <= .95
	}
	// A passing plate can travel several widths in one frame. Widen the
	// horizontal gate only with additional OCR or outward-motion evidence.
	if gap <= 500*time.Millisecond && min(older.Confidence, newer.Confidence) >= .86 {
		outward := (x-FrameWidth/2)*dx > 0
		agreement := matchingFields(older, newer) > 0
		if agreement || (len(older.Number) == 4 && outward && min(older.Confidence, newer.Confidence) >= .90) {
			xAllowance = max(xAllowance, 140+500*dt)
		}
	}
	return nw/w >= .35 && nw/w <= 2.85 && nh/h >= .35 && nh/h <= 2.85 &&
		math.Hypot(dx/xAllowance, dy/yAllowance) <= 1 &&
		!(math.Abs(t.velocity[0]) >= 80 && math.Abs(dx) > max(w, nw)+50 && t.velocity[0]*dx < 0)
}

func (t *plateTrack) canRejoinOverlap(next *plateTrack, fieldConflicts int) bool {
	older, newer := t.best.reading, next.best.reading
	return next.last.After(t.last) && t.misses > 0 && len(next.startFrames) == next.frames &&
		t.hasVerification && next.hasVerification && len(older.Number) == 4 && t.support >= 3 &&
		min(older.Confidence, newer.Confidence) >= .85 && fieldConflicts == 0 && matchingFields(older, newer) >= 2 &&
		stationaryDistance(t.box, next.firstBox) <= .36 && stationaryDistance(t.best.box, next.best.box) <= .40
}

func stationaryDistance(a, b [4]int) float64 {
	w, h, nw, nh := float64(a[2]), float64(a[3]), float64(b[2]), float64(b[3])
	if w <= 0 || h <= 0 || nw/w < .55 || nw/w > 1.8 || nh/h < .55 || nh/h > 1.8 {
		return math.Inf(1)
	}
	x, y := center(a)
	nx, ny := center(b)
	return math.Hypot((nx-x)/max(w, nw), (ny-y)/max(h, nh))
}

func (t *plateTrack) join(next *plateTrack) {
	if next.frames == 1 {
		x, y := center(t.box)
		nx, ny := center(next.box)
		dt := next.last.Sub(t.last).Seconds()
		t.velocity = [2]float64{(nx - x) / dt, (ny - y) / dt}
	} else {
		t.velocity = next.velocity
	}
	t.last, t.finished, t.box, t.misses = next.last, next.finished, next.box, next.misses
	t.frames += next.frames
	t.startFrames = nil
	t.openingFrames = nil
	t.reads += next.reads
	t.appearanceSamples = next.appearanceSamples
	t.recentFrames = next.recentFrames
	t.hasVerification = t.hasVerification || next.hasVerification
	for number, confidence := range next.localized {
		t.rememberLocalized(number, confidence)
	}
	for number, frames := range next.localizedFrames {
		for _, frame := range frames {
			t.rememberLocalizedFrame(number, frame)
		}
	}
	for number, checks := range next.challengers {
		t.rememberChallenger(number, checks)
	}
	if excess := len(t.ocr) + len(next.ocr) - trackOCRWindow; excess > 0 {
		copy(t.ocr, t.ocr[excess:])
		t.ocr = t.ocr[:len(t.ocr)-excess]
	}
	t.ocr = append(t.ocr, next.ocr...)
	for number, vote := range next.votes {
		t.addEvidence(number, vote)
	}
	for number, fields := range next.fields {
		t.rememberFields(number, fields)
	}
	t.updateConsensus()
}

func rejoinTracks(tracks, recent []*plateTrack) []*plateTrack {
	// A join can give an earlier fragment enough new observations to match its
	// predecessor. Recheck before publishing, while a fragment disappears on
	// each successful pass.
	for {
		remaining, joined := rejoinTracksOnce(tracks, recent)
		if !joined {
			return remaining
		}
		tracks = remaining
	}
}

func rejoinTracksOnce(tracks, recent []*plateTrack) ([]*plateTrack, bool) {
	joined := false
	for i, next := range tracks {
		if next == nil || next.misses != 0 || next.sent != 0 || next.support == 0 {
			continue
		}
		var prior *plateTrack
		var overlap []trackFrame
		recentIndex := -1
		choices := 0
		for groupIndex, group := range [][]*plateTrack{tracks, recent} {
			for j, track := range group {
				if track == nil {
					continue
				}
				var samples []trackFrame
				if !track.canRejoin(next) && !track.canRejoinFastOutward(next, tracks) {
					samples = track.interleavedOverlap(next)
					if samples == nil {
						continue
					}
				}
				choices++
				prior, overlap = track, samples
				if groupIndex == 1 {
					recentIndex = j
				}
			}
		}
		if choices != 1 {
			continue
		}
		for j, other := range tracks {
			if j != i && other != nil && other.misses == 0 && (prior.canRejoin(other) ||
				prior.canRejoinFastOutward(other, tracks) || prior.interleavedOverlap(other) != nil) {
				prior = nil // two possible successors
				break
			}
		}
		if prior == nil {
			continue
		}
		joined = true
		if next.first.After(prior.last) {
			prior.join(next)
		} else {
			// The younger fragment can include a second crop in the same
			// frame. Replay only observations after the prior's last one.
			if overlap == nil {
				overlap = next.overlapFrames(prior.last)
			} else {
				prior.countInterleavedFrames(overlap)
			}
			for _, sample := range overlap {
				if sample.at.After(prior.last) {
					prior.observe(sample.at, sample.finished, sample.plate, sample.id, sample.minConfidence)
				}
			}
		}
		if recentIndex >= 0 {
			recent[recentIndex] = nil
			tracks[i] = prior
		} else {
			tracks[i] = nil
		}
	}
	remaining := tracks[:0]
	for _, track := range tracks {
		if track != nil {
			remaining = append(remaining, track)
		}
	}
	clear(tracks[len(remaining):])
	return remaining, joined
}

func (t *plateTrack) countInterleavedFrames(samples []trackFrame) {
	for _, sample := range samples {
		if sample.at.After(t.last) {
			return
		}
		seen := false
		for _, existing := range t.recentFrames {
			if existing.at.Equal(sample.at) {
				seen = true
				break
			}
		}
		if !seen {
			t.frames++
		}
	}
}

// assignTracks maximizes the total weight of the frame's one-to-one assignments.
// A zero-weight edge, including a padded column, leaves a detection unmatched.
func assignTracks(tracks []*plateTrack, at time.Time, detections []EnginePlate) []int {
	assigned := make([]int, len(detections))
	for i := range assigned {
		assigned[i] = -1
	}
	if len(tracks) == 0 || len(detections) == 0 {
		return assigned
	}
	columns := max(len(tracks), len(detections))
	weights := make([][]float64, len(detections))
	for i, detection := range detections {
		detection.Reading = plate.Normalize(detection.Reading)
		weights[i] = make([]float64, columns)
		for j, track := range tracks {
			weights[i][j] = track.match(at, detection)
		}
	}
	// Hungarian algorithm. Each pass augments the assignment by one detection,
	// reversing previous assignments along the cheapest path when necessary.
	rowPotential := make([]float64, len(detections)+1)
	columnPotential := make([]float64, columns+1)
	matchedRow := make([]int, columns+1)
	previous := make([]int, columns+1)
	distance := make([]float64, columns+1)
	used := make([]bool, columns+1)
	for row := 1; row <= len(detections); row++ {
		matchedRow[0] = row
		clear(used)
		for col := 1; col <= columns; col++ {
			distance[col] = math.Inf(1)
		}
		col := 0
		for {
			used[col] = true
			candidateRow := matchedRow[col]
			delta, next := math.Inf(1), 0
			for j := 1; j <= columns; j++ {
				if used[j] {
					continue
				}
				cost := -weights[candidateRow-1][j-1] - rowPotential[candidateRow] - columnPotential[j]
				if cost < distance[j] {
					distance[j], previous[j] = cost, col
				}
				if distance[j] < delta {
					delta, next = distance[j], j
				}
			}
			for j := 0; j <= columns; j++ {
				if used[j] {
					rowPotential[matchedRow[j]] += delta
					columnPotential[j] -= delta
				} else {
					distance[j] -= delta
				}
			}
			col = next
			if matchedRow[col] == 0 {
				break
			}
		}
		for col != 0 {
			prior := previous[col]
			matchedRow[col] = matchedRow[prior]
			col = prior
		}
	}
	for j := range tracks {
		if row := matchedRow[j+1]; row > 0 && weights[row-1][j] > 0 {
			assigned[row-1] = j
		}
	}
	return assigned
}

func (t *plateTrack) canRejoinInwardPass(next *plateTrack) bool {
	a, b := t.best.reading, next.best.reading
	if t.frames != 1 || t.support != 1 {
		return false
	}
	gap := next.first.Sub(t.last)
	short := len(a.Number) <= 2
	if short {
		if a.Number == "" || t.sent != 0 || next.support < 2 || gap > 150*time.Millisecond ||
			min(a.Confidence, b.Confidence) < .82 || t.localized[a.Number] < .90 ||
			min(t.votes[a.Number].wideVerification, next.votes[b.Number].wideVerification) < .97 {
			return false
		}
	} else if next.support < 3 || len(a.Number) > 4 || min(a.Confidence, b.Confidence) < .88 ||
		!t.votes[a.Number].independentProof || !next.votes[b.Number].independentProof {
		return false
	}
	if !t.numberVerified() || !next.numberVerified() {
		return false
	}
	w, h, nw, nh := float64(t.box[2]), float64(t.box[3]), float64(next.firstBox[2]), float64(next.firstBox[3])
	x, y := center(t.box)
	nx, ny := center(next.firstBox)
	side := x - FrameWidth/2
	dx, dy := nx-x, ny-y
	maxShift, maxRise := 2.6, .40
	maxFollow, minSpeedRatio := 450*time.Millisecond, .45
	minSide := .29 * FrameWidth
	samples := next.startFrames
	var firstAppearance []byte
	if short {
		maxShift, maxRise, maxFollow = 2.1, .53, 550*time.Millisecond
		if len(samples) < 3 {
			samples = next.recentFrames
		}
		if len(samples) > 0 && samples[0].at.Equal(next.first) {
			firstAppearance = samples[0].plate.appearance
		} else if next.best.at.Equal(next.first) {
			firstAppearance = next.best.appearance
		}
	}
	if len(a.Number) == 4 {
		// A full serial with strong independent reads can identify the same
		// car slightly farther from the edge. Keep two later motion samples
		// even if the detector missed the intervening frames.
		if min(a.Confidence, b.Confidence) < .90 {
			return false
		}
		minSide, maxFollow = .25*FrameWidth, 550*time.Millisecond
		if len(samples) < 3 {
			samples = next.recentFrames
		}
	}
	if gap > 150*time.Millisecond {
		// A dropped frame can hide the middle of a short pass. Keep the
		// independent OCR checks on both sides and require a longer trajectory.
		if min(a.Confidence, b.Confidence) < .90 {
			return false
		}
		maxShift *= float64(gap) / float64(125*time.Millisecond)
		maxRise, maxFollow, minSpeedRatio = .55, 800*time.Millisecond, .38
	}
	if math.Abs(side) < minSide || side*dx >= 0 || side*(nx-FrameWidth/2) < 0 ||
		math.Abs(dx) < 1.5*w || math.Abs(dx) > maxShift*w || dy >= 0 || -dy > maxRise*h || nw/w < .80 || nw/w > 1.20 || nh/h < .68 || nh/h > 1.04 {
		return false
	}
	// The following frames must continue moving inward in the same lane
	// and at a speed consistent with the earlier, closer observation.
	if len(samples) < 2 {
		return false
	}
	first := 0
	if samples[0].at.Equal(next.first) {
		first++
	}
	if len(samples) < first+2 {
		return false
	}
	f2, f3 := samples[first], samples[first+1]
	x2, y2 := center(f2.plate.Box)
	x3, y3 := center(f3.plate.Box)
	dt := f3.at.Sub(next.first)
	if !f2.at.After(next.first) || dt <= 0 || dt > maxFollow ||
		(x2-nx)*dx <= 0 || (x3-x2)*dx <= 0 || y2 > ny+.10*h || y3 > y2+.15*h {
		return false
	}
	stepRatio := math.Abs(x3-nx) * float64(gap) / float64(dt) / math.Abs(dx)
	return stepRatio >= minSpeedRatio && stepRatio <= 1.15 &&
		(!short || t.matchesShortInwardPass(next.firstBox, firstAppearance, f2.plate))
}

func (t *plateTrack) matchesShortInwardPass(firstBox [4]int, firstAppearance []byte, following EnginePlate) bool {
	a, b, c := t.best.appearance, firstAppearance, following.appearance
	number := t.best.reading.Number
	if t.box[2] < 55 || firstBox[2] < 50 || len(a) != appearanceWidth*appearanceHeight ||
		len(b) != len(a) || len(c) != len(a) || following.NumberScores[number] < .97 ||
		following.SequenceNumber != number || following.Reading.Number != number || following.Verifier == nil || following.Verifier.Number != number {
		return false
	}
	// A short serial can recur in the same place. Compare the body above the
	// plate and a larger crop in each of two later frames.
	return appearanceMatches(a, b, 7, 1, 34, 11, 14, 4, .90) &&
		appearanceMatches(a, b, 5, 5, 37, 19, 11, 9, .79) &&
		appearanceMatches(a, c, 7, 1, 34, 11, 14, 4, .85) &&
		appearanceMatches(a, c, 5, 5, 37, 19, 11, 9, .74)
}
