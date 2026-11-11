package modem

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type nasServing struct {
	Registered, PSAttached *bool
	Carrier, PLMN, RAT     string
}

func boolValue(value bool) *bool { return &value }
func safeCarrier(raw []byte) string {
	if !utf8.Valid(raw) {
		return ""
	}
	value := string(raw)
	for _, r := range value {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return strings.TrimSpace(value)
}
func parseServingResponse(frame []byte, transaction uint16) (nasServing, error) {
	out := nasServing{}
	values, e := parseNASResponse(frame, transaction, 0x0024)
	if e != nil {
		return out, e
	}
	if value, ok := values[1]; ok {
		if len(value) < 5 || len(value) != 5+int(value[4]) {
			return out, errors.New("invalid NAS serving RAT array")
		}
		switch value[0] {
		case 0, 2, 3:
			out.Registered = boolValue(false)
		case 1:
			out.Registered = boolValue(true)
		}
		switch value[2] {
		case 1:
			out.PSAttached = boolValue(true)
		case 2:
			out.PSAttached = boolValue(false)
		}
		rats := []string{}
		seen := map[string]bool{}
		for _, rat := range value[5:] {
			name := map[byte]string{1: "CDMA", 2: "EVDO", 4: "GSM", 5: "UMTS", 8: "LTE", 9: "TD-SCDMA", 12: "5G NR"}[rat]
			if name != "" && !seen[name] {
				rats = append(rats, name)
				seen[name] = true
			}
		}
		out.RAT = strings.Join(rats, "/")
	}
	mcc, mnc := -1, -1
	if value, ok := values[0x12]; ok {
		if len(value) < 5 || len(value) != 5+int(value[4]) {
			return nasServing{}, errors.New("invalid current PLMN name length")
		}
		mcc, mnc = int(binary.LittleEndian.Uint16(value)), int(binary.LittleEndian.Uint16(value[2:]))
		if mcc < 1 || mcc > 999 || mnc > 999 {
			return nasServing{}, errors.New("invalid current PLMN codes")
		}
		out.Carrier = safeCarrier(value[5:])
	}
	digits := 0
	if value, ok := values[0x27]; ok {
		if len(value) != 5 || value[4] > 1 {
			return nasServing{}, errors.New("invalid PLMN digit status")
		}
		otherMCC, otherMNC := int(binary.LittleEndian.Uint16(value)), int(binary.LittleEndian.Uint16(value[2:]))
		if otherMCC < 1 || otherMCC > 999 || otherMNC > 999 {
			return nasServing{}, errors.New("invalid digit status PLMN")
		}
		if mcc >= 0 && (mcc != otherMCC || mnc != otherMNC) {
			return nasServing{}, errors.New("PLMN digit status refers to another network")
		}
		mcc, mnc = otherMCC, otherMNC
		digits = 2
		if value[4] == 1 {
			digits = 3
		}
		if digits == 2 && mnc > 99 {
			return nasServing{}, errors.New("two-digit MNC out of range")
		}
	}
	if mcc >= 0 {
		if digits == 0 && mnc >= 100 {
			digits = 3
		}
		if digits > 0 {
			out.PLMN = fmt.Sprintf("%03d%0*d", mcc, digits, mnc)
		}
	}
	return out, nil
}

// QmiNasActiveBand is a sparse enum, not the LTE band number. Only explicitly
// verified LTE values are mapped. A UMTS or unknown RAT never enters this map.
var lteBands = map[uint16]string{120: "B1", 121: "B2", 122: "B3", 123: "B4", 124: "B5", 125: "B6", 126: "B7", 127: "B8", 128: "B9", 129: "B10", 130: "B11", 131: "B12", 132: "B13", 133: "B14", 134: "B17", 143: "B18", 144: "B19", 145: "B20", 146: "B21", 152: "B23", 147: "B24", 148: "B25", 153: "B26", 164: "B27", 158: "B28", 159: "B29", 160: "B30", 165: "B31", 154: "B32", 135: "B33", 136: "B34", 137: "B35", 138: "B36", 139: "B37", 140: "B38", 141: "B39", 142: "B40", 149: "B41", 150: "B42", 151: "B43"}

func parseBandResponse(frame []byte, transaction uint16) (string, []string, error) {
	values, e := parseNASResponse(frame, transaction, 0x0031)
	if e != nil {
		return "", nil, e
	}
	known := map[string]bool{}
	unknown := false
	parse := func(value []byte, width int, record bool) error {
		if len(value) < 1 || len(value) != 1+int(value[0])*width {
			return errors.New("invalid RF band list bounds")
		}
		for pos := 1; pos < len(value); pos += width {
			if value[pos] != 8 || !record {
				continue
			}
			band := lteBands[binary.LittleEndian.Uint16(value[pos+1:pos+3])]
			if band == "" {
				unknown = true
			} else {
				known[band] = true
			}
		}
		return nil
	}
	extended, hasExtended := values[0x11]
	if regular, ok := values[1]; ok {
		if e = parse(regular, 5, !hasExtended); e != nil {
			return "", nil, e
		}
	}
	if hasExtended {
		if e = parse(extended, 7, true); e != nil {
			return "", nil, e
		}
	}
	bands := []string{}
	for band := range known {
		bands = append(bands, band)
	}
	sort.Strings(bands)
	if len(bands) == 1 && !unknown {
		return bands[0], bands, nil
	}
	return "", bands, nil
}
