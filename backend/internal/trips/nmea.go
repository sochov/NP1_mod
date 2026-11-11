// Package trips derives journeys from measured GNSS samples. Missing fixes never become zero distance.
package trips

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

const MaxSidecarBytes = 2 * 1024 * 1024

type Sample struct {
	At          time.Time `json:"at"`
	Lat         float64   `json:"lat"`
	Lon         float64   `json:"lon"`
	Valid       bool      `json:"valid"`
	SpeedMPS    *float64  `json:"speedMps,omitempty"`
	Heading     *float64  `json:"heading,omitempty"`
	AccuracyM   *float64  `json:"accuracyM,omitempty"`
	HDOP        *float64  `json:"hdop,omitempty"`
	Satellites  *int      `json:"satellites,omitempty"`
	Quality     int       `json:"quality"`
	Source      string    `json:"source"`
	BreakBefore bool      `json:"breakBefore,omitempty"`
}

type ParseReport struct {
	Samples     []Sample
	RMC         int
	ValidFixes  int
	NoFix       int
	BadChecksum int
	Malformed   int
}
type gga struct {
	quality    int
	hdop       *float64
	satellites *int
}

func sentence(line string) ([]string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "$") {
		return nil, false
	}
	star := strings.LastIndexByte(line, '*')
	if star < 1 || len(line) < star+3 {
		return nil, false
	}
	expected, e := strconv.ParseUint(line[star+1:star+3], 16, 8)
	if e != nil {
		return nil, false
	}
	var sum byte
	for i := 1; i < star; i++ {
		sum ^= line[i]
	}
	if sum != byte(expected) {
		return nil, false
	}
	return strings.Split(line[1:star], ","), true
}
func finite(s string) *float64 {
	v, e := strconv.ParseFloat(s, 64)
	if e != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}
func coordinate(raw, hemisphere string, latitude bool) (float64, error) {
	v := finite(raw)
	if v == nil || *v < 0 {
		return 0, errors.New("invalid coordinate")
	}
	degrees := math.Floor(*v / 100)
	minutes := *v - degrees*100
	limit := 180.0
	if latitude {
		limit = 90
	}
	if minutes >= 60 || degrees > limit || (degrees == limit && minutes > 0) {
		return 0, errors.New("coordinate out of range")
	}
	result := degrees + minutes/60
	if latitude {
		if hemisphere != "N" && hemisphere != "S" {
			return 0, errors.New("invalid latitude hemisphere")
		}
		if hemisphere == "S" {
			result = -result
		}
	} else {
		if hemisphere != "E" && hemisphere != "W" {
			return 0, errors.New("invalid longitude hemisphere")
		}
		if hemisphere == "W" {
			result = -result
		}
	}
	return result, nil
}
func nmeaTime(clock, date string) (time.Time, error) {
	if len(clock) < 6 || len(date) != 6 {
		return time.Time{}, errors.New("missing GNSS time")
	}
	value, e := time.Parse("020106", date)
	if e != nil {
		return time.Time{}, e
	}
	h, e1 := strconv.Atoi(clock[:2])
	m, e2 := strconv.Atoi(clock[2:4])
	seconds := finite(clock[4:])
	if e1 != nil || e2 != nil || seconds == nil || h > 23 || m > 59 || *seconds < 0 || *seconds >= 60 {
		return time.Time{}, errors.New("invalid GNSS time")
	}
	whole := int(*seconds)
	ns := int(math.Round((*seconds - float64(whole)) * 1e9))
	return time.Date(value.Year(), value.Month(), value.Day(), h, m, whole, ns, time.UTC), nil
}

// ParseNMEA accepts standard checksum-protected RMC/GGA. Proprietary sensor lines are ignored.
// GGA HDOP is retained as HDOP; it is never relabelled as an accuracy in metres.
func ParseNMEA(reader io.Reader, source string) (ParseReport, error) {
	report := ParseReport{Samples: []Sample{}}
	raw, e := io.ReadAll(io.LimitReader(reader, MaxSidecarBytes+1))
	if e != nil {
		return report, e
	}
	if len(raw) > MaxSidecarBytes {
		return report, errors.New("NMEA sidecar exceeds size limit")
	}
	lines := []string{}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 1024), 4096)
	ggas := map[string]gga{}
	for scanner.Scan() {
		line := scanner.Text()
		lines = append(lines, line)
		f, ok := sentence(line)
		if !ok || len(f) < 10 || !strings.HasSuffix(f[0], "GGA") {
			continue
		}
		q, e := strconv.Atoi(f[6])
		if e != nil {
			continue
		}
		g := gga{quality: q}
		if v, e := strconv.Atoi(f[7]); e == nil && v >= 0 && v <= 100 {
			g.satellites = &v
		}
		if v := finite(f[8]); v != nil && *v > 0 && *v <= 100 {
			g.hdop = v
		}
		ggas[f[1]] = g
	}
	if e = scanner.Err(); e != nil {
		return report, e
	}
	// Start a separate segment: the preceding sidecar may end in a no-fix
	// sentence that has no UTC timestamp and cannot be placed in the timeline.
	pendingBreak := true
	for _, line := range lines {
		// Only GNSS sentences count as malformed/checksum failures; GSENSORD has a different contract.
		isRMC := strings.HasPrefix(line, "$GPRMC,") || strings.HasPrefix(line, "$GNRMC,")
		if !isRMC {
			continue
		}
		report.RMC++
		f, ok := sentence(line)
		if !ok {
			report.BadChecksum++
			pendingBreak = true
			continue
		}
		if len(f) < 10 {
			report.Malformed++
			pendingBreak = true
			continue
		}
		at, e := nmeaTime(f[1], f[9])
		if e != nil {
			if f[2] != "A" {
				report.NoFix++
			} else {
				report.Malformed++
			}
			pendingBreak = true
			continue
		}
		s := Sample{At: at, Source: source, BreakBefore: pendingBreak, Quality: 1}
		mode := ""
		if len(f) > 12 {
			mode = f[12]
		}
		if f[2] != "A" || mode == "N" || mode == "E" {
			report.NoFix++
			pendingBreak = true
			report.Samples = append(report.Samples, s)
			continue
		}
		lat, e1 := coordinate(f[3], f[4], true)
		lon, e2 := coordinate(f[5], f[6], false)
		if e1 != nil || e2 != nil || (lat == 0 && lon == 0) {
			report.Malformed++
			pendingBreak = true
			report.Samples = append(report.Samples, s)
			continue
		}
		if g, ok := ggas[f[1]]; ok {
			s.Quality = g.quality
			s.HDOP = g.hdop
			s.Satellites = g.satellites
			if g.quality <= 0 {
				report.NoFix++
				pendingBreak = true
				report.Samples = append(report.Samples, s)
				continue
			}
		}
		s.Lat, s.Lon, s.Valid = lat, lon, true
		if speed := finite(f[7]); speed != nil && *speed >= 0 && *speed <= 150 {
			v := *speed * 0.5144444444
			s.SpeedMPS = &v
		}
		if heading := finite(f[8]); heading != nil && *heading >= 0 && *heading < 360 {
			s.Heading = heading
		}
		report.ValidFixes++
		pendingBreak = false
		report.Samples = append(report.Samples, s)
	}
	return report, nil
}

// Sentence is useful for synthetic protocol fixtures, never for fabricating device observations.
func Sentence(body string) string {
	var sum byte
	for i := range body {
		sum ^= body[i]
	}
	return fmt.Sprintf("$%s*%02X", body, sum)
}
