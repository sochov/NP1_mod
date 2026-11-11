package modem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const ProfileRecoveryDirectory = "/overlay/owner-apps/np1-control/private/apn/recovery"

// ProfileRecovery intentionally has no exported raw or credential fields.
// Its JSON, String and GoString forms contain only the public summary.
type ProfileRecovery struct {
	observedAt time.Time
	profiles   [2]savedAPNProfile
}
type ProfileRecoverySummary struct {
	ObservedAt           time.Time              `json:"observedAt"`
	Source               string                 `json:"source"`
	Current              bool                   `json:"current"`
	Complete             bool                   `json:"complete"`
	NativeFieldsComplete bool                   `json:"nativeFieldsComplete"`
	Profiles             []SavedAPNProfileState `json:"profiles"`
	BackupFile           string                 `json:"backupFile,omitempty"`
}
type SavedAPNProfileState struct {
	Index                int      `json:"index"`
	Status               string   `json:"status"`
	FieldCount           int      `json:"fieldCount"`
	MissingFields        []string `json:"missingFields"`
	UnknownPDP           bool     `json:"unknownPDP"`
	NativeFieldsComplete bool     `json:"nativeFieldsComplete"`
	ProtocolError        *uint16  `json:"protocolError,omitempty"`
	ExtendedError        *uint16  `json:"extendedError,omitempty"`
	AbsenceConfirmed     bool     `json:"absenceConfirmed"`
}
type savedAPNProfile struct {
	index          int
	status         string
	raw            []byte
	fieldCount     int
	pdp            *byte
	apn            *string
	username       *string
	password       *string
	authentication *byte
	protocolError  *uint16
	extendedError  *uint16
}

func (r ProfileRecovery) MarshalJSON() ([]byte, error) { return json.Marshal(r.Summary()) }
func (r ProfileRecovery) String() string               { raw, _ := r.MarshalJSON(); return string(raw) }
func (r ProfileRecovery) GoString() string             { return r.String() }
func (r ProfileRecovery) Summary() ProfileRecoverySummary {
	out := ProfileRecoverySummary{ObservedAt: r.observedAt, Source: "gobinet-wds-saved-profiles", Current: false, Complete: true, NativeFieldsComplete: true, Profiles: []SavedAPNProfileState{}}
	for i, profile := range r.profiles {
		state := SavedAPNProfileState{Index: i + 1, Status: profile.status, FieldCount: profile.fieldCount, MissingFields: []string{}, ProtocolError: profile.protocolError, ExtendedError: profile.extendedError, AbsenceConfirmed: profile.absenceConfirmed()}
		if state.Status == "" {
			state.Status = "not-requested"
		}
		for _, field := range []struct {
			name    string
			present bool
		}{{"pdp", profile.pdp != nil}, {"apn", profile.apn != nil}, {"username", profile.username != nil}, {"password", profile.password != nil}, {"authentication", profile.authentication != nil}} {
			if !field.present {
				state.MissingFields = append(state.MissingFields, field.name)
			}
		}
		state.UnknownPDP = profile.pdp != nil && *profile.pdp > 4
		state.NativeFieldsComplete = profile.nativeFieldsComplete()
		if profile.status != "available" || len(state.MissingFields) > 0 || state.UnknownPDP {
			out.Complete = false
		}
		if !state.NativeFieldsComplete {
			out.NativeFieldsComplete = false
		}
		out.Profiles = append(out.Profiles, state)
	}
	return out
}
func (p savedAPNProfile) absenceConfirmed() bool {
	// Only explicit 3GPP no-context/no-valid-flag/profile-not-found results
	// confirm an undefined saved context. Generic invalid index/access failures
	// do not prove absence and never enable restoration.
	return p.status == "failed" && p.protocolError != nil && *p.protocolError == 81 && p.extendedError != nil && (*p.extendedError == 1003 || *p.extendedError == 1004 || *p.extendedError == 10)
}
func parseFailedSavedProfile(frame []byte, index int) (savedAPNProfile, error) {
	if index != 1 && index != 2 {
		return savedAPNProfile{}, errors.New("profile index is not allowlisted")
	}
	values, e := parseQMIResponse(frame, uint16(index+4), 0x002b)
	var protocol *qmiProtocolError
	if !errors.As(e, &protocol) {
		return savedAPNProfile{}, errors.New("profile failure frame is not a validated protocol error")
	}
	code := protocol.Code
	return savedAPNProfile{index: index, status: "failed", raw: append([]byte(nil), frame...), fieldCount: len(values), protocolError: &code, extendedError: protocol.Extended}, nil
}
func safeNativeCredential(value string) bool {
	if len(value) > 49 || strings.HasPrefix(value, "-") {
		return false
	}
	for _, c := range []byte(value) {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-@:+%=", rune(c))) {
			return false
		}
	}
	return true
}
func (p savedAPNProfile) nativeFieldsComplete() bool {
	return p.status == "available" && p.pdp != nil && *p.pdp == 0 && p.apn != nil && len(*p.apn) <= 49 && validAPN(*p.apn) && p.username != nil && safeNativeCredential(*p.username) && p.password != nil && safeNativeCredential(*p.password) && p.authentication != nil && *p.authentication <= 3
}
func profileRequest(index int) ([]byte, error) {
	if index != 1 && index != 2 {
		return nil, errors.New("profile index is not allowlisted")
	}
	return []byte{0, byte(index + 4), 0, 0x2b, 0, 5, 0, 1, 2, 0, 0, byte(index)}, nil
}

// Only fixed profile 1/2 responses are accepted. Values remain private. Empty
// credentials are distinct from absent TLVs, and unknown PDP/auth stay raw-only.
func parseSavedAPNProfile(frame []byte, index int) (savedAPNProfile, error) {
	if index != 1 && index != 2 {
		return savedAPNProfile{}, errors.New("profile index is not allowlisted")
	}
	values, e := parseQMIResponse(frame, uint16(index+4), 0x002b)
	if e != nil {
		return savedAPNProfile{}, e
	}
	out := savedAPNProfile{index: index, status: "available", fieldCount: len(values)}
	for _, kind := range []byte{0x11, 0x1d} {
		if value, ok := values[kind]; ok {
			if len(value) != 1 {
				return savedAPNProfile{}, errors.New("invalid profile enum length")
			}
			v := value[0]
			if kind == 0x11 {
				out.pdp = &v
			} else {
				out.authentication = &v
			}
		}
	}
	for _, kind := range []byte{0x14, 0x1b, 0x1c} {
		if raw, ok := values[kind]; ok {
			// A valid QMI string need not be usable by the stock AT path. Keep
			// unsupported values in the private raw backup without normalizing.
			if len(raw) > 1024 || !utf8.Valid(raw) || strings.ContainsRune(string(raw), '\x00') {
				return savedAPNProfile{}, errors.New("invalid profile string")
			}
			value := string(raw)
			switch kind {
			case 0x14:
				out.apn = &value
			case 0x1b:
				out.username = &value
			case 0x1c:
				out.password = &value
			}
		}
	}
	out.raw = append([]byte(nil), frame...)
	return out, nil
}
func collectProfiles(ctx context.Context, exchange func(context.Context, uint16, uint16) ([]byte, error)) (ProfileRecovery, error) {
	out := ProfileRecovery{observedAt: time.Now().UTC()}
	success := false
	for index := 1; index <= 2; index++ {
		entry := savedAPNProfile{index: index, status: "failed"}
		if e := ctx.Err(); e != nil {
			entry.status = "timeout"
			out.profiles[index-1] = entry
			continue
		}
		frame, e := exchange(ctx, uint16(index+4), 0x002b)
		if e == nil {
			entry, e = parseSavedAPNProfile(frame, index)
		}
		if e != nil {
			entry = savedAPNProfile{index: index, status: "failed"}
			if failure, parseError := parseFailedSavedProfile(frame, index); parseError == nil {
				entry = failure
			}
			if errors.Is(e, context.DeadlineExceeded) || errors.Is(e, context.Canceled) {
				entry.status = "timeout"
			}
		} else {
			success = true
		}
		out.profiles[index-1] = entry
	}
	if !success {
		return out, errors.New("saved profile queries unavailable")
	}
	return out, nil
}

var _ fmt.Stringer = ProfileRecovery{}
var _ fmt.GoStringer = ProfileRecovery{}
