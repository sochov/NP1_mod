package modem

import (
	"errors"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type APNOverrideProfile struct {
	Slot           int    `json:"slot"`
	APN            string `json:"apn"`
	Authentication int    `json:"authentication"`
}
type APNOverride struct {
	ObservedAt time.Time            `json:"observedAt"`
	Source     string               `json:"source"`
	Current    bool                 `json:"current"`
	Profiles   []APNOverrideProfile `json:"profiles"`
}

// ParseNativeAPNOverride reads the stock SD apnInfo.csv input format. It never
// writes or activates it, and does not retain the username/password columns.
// Stock consumes two CRLF-separated rows, each APN,user,password,auth(0..3).
func ParseNativeAPNOverride(reader io.Reader) (APNOverride, error) {
	raw, e := io.ReadAll(io.LimitReader(reader, 4097))
	if e != nil {
		return APNOverride{}, e
	}
	if len(raw) > 4096 || !utf8.Valid(raw) {
		return APNOverride{}, errors.New("invalid APN override size or encoding")
	}
	text := string(raw)
	if strings.HasPrefix(text, "\ufeff") || strings.ContainsRune(text, '\x00') {
		return APNOverride{}, errors.New("unsupported APN override encoding")
	}
	text = strings.TrimSuffix(text, "\r\n")
	lines := strings.Split(text, "\r\n")
	if len(lines) != 2 {
		return APNOverride{}, errors.New("APN override requires two CRLF-separated rows")
	}
	out := APNOverride{Source: "stock-sd-apn-override", Current: false, Profiles: []APNOverrideProfile{}}
	for index, line := range lines {
		fields := strings.Split(line, ",")
		if len(fields) != 4 || !validAPN(fields[0]) || len(fields[0]) > 49 {
			return APNOverride{}, errors.New("invalid stock APN override row")
		}
		for _, value := range fields[1:3] {
			if len(value) > 49 || strings.ContainsAny(value, "\r\n\"\x00") {
				return APNOverride{}, errors.New("unsupported APN credential field")
			}
		}
		auth, e := strconv.Atoi(fields[3])
		if e != nil || len(fields[3]) != 1 || auth < 0 || auth > 3 {
			return APNOverride{}, errors.New("invalid APN authentication type")
		}
		out.Profiles = append(out.Profiles, APNOverrideProfile{Slot: index + 1, APN: fields[0], Authentication: auth})
	}
	out.ObservedAt = time.Now().UTC()
	return out, nil
}
