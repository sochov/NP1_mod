package modem

import (
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const qcmapConfigPath = "/etc/data/mobileap_cfg.xml"
const maximumAPNConfig = 64 * 1024

// APNProfileConfig is saved configuration, not proof of the active data call.
// The fixed QCMAP schema on NP1 contains profile indexes, not an APN name.
type APNProfileConfig struct {
	ObservedAt       time.Time `json:"observedAt"`
	Source           string    `json:"source"`
	IPv4ProfileIndex *int      `json:"ipv4ProfileIndex"`
	IPv6ProfileIndex *int      `json:"ipv6ProfileIndex"`
	Current          bool      `json:"current"`
}
type APNObservation struct {
	ObservedAt   time.Time `json:"observedAt"`
	Source       string    `json:"source"`
	APN          *string   `json:"apn"`
	WANIPv4      *string   `json:"wanIPv4"`
	ProfileType  *string   `json:"profileType"`
	ProfileIndex *int      `json:"profileIndex"`
	Current      bool      `json:"current"`
}

func ReadQCMAPProfileConfig() (APNProfileConfig, error) {
	root, e := os.OpenRoot("/etc/data")
	if e != nil {
		return APNProfileConfig{}, e
	}
	defer root.Close()
	st, e := root.Lstat("mobileap_cfg.xml")
	if e != nil {
		return APNProfileConfig{}, e
	}
	if !st.Mode().IsRegular() || st.Size() > maximumAPNConfig {
		return APNProfileConfig{}, errors.New("invalid QCMAP config file")
	}
	stream, e := root.Open("mobileap_cfg.xml")
	if e != nil {
		return APNProfileConfig{}, e
	}
	defer stream.Close()
	opened, e := stream.Stat()
	if e != nil || !opened.Mode().IsRegular() || !os.SameFile(st, opened) || opened.Size() > maximumAPNConfig {
		return APNProfileConfig{}, errors.New("QCMAP config changed while opening")
	}
	return ParseQCMAPProfileConfig(stream)
}

// ParseQCMAPProfileConfig deliberately ignores every other configuration field,
// including usernames, passwords, DDNS and SIM/device identifiers.
func ParseQCMAPProfileConfig(reader io.Reader) (APNProfileConfig, error) {
	out := APNProfileConfig{Source: "qcmap-saved-profile-config", Current: false}
	raw, e := io.ReadAll(io.LimitReader(reader, maximumAPNConfig+1))
	if e != nil {
		return out, e
	}
	if len(raw) > maximumAPNConfig {
		return out, errors.New("QCMAP config exceeds limit")
	}
	decoder := xml.NewDecoder(strings.NewReader(string(raw)))
	path := []string{}
	seen := map[string]bool{}
	rootSeen := false
	for {
		token, e := decoder.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return APNProfileConfig{}, errors.New("invalid QCMAP config XML")
		}
		switch value := token.(type) {
		case xml.CharData:
			if len(path) == 0 && strings.TrimSpace(string(value)) != "" {
				return APNProfileConfig{}, errors.New("data outside QCMAP XML root")
			}
		case xml.Directive:
			return APNProfileConfig{}, errors.New("QCMAP XML directives are unsupported")
		case xml.StartElement:
			if len(path) == 0 {
				if rootSeen || value.Name.Local != "system" || value.Name.Space != "" {
					return APNProfileConfig{}, errors.New("invalid QCMAP XML root")
				}
				rootSeen = true
			}
			if value.Name.Space != "" {
				return APNProfileConfig{}, errors.New("unsupported QCMAP XML namespace")
			}
			path = append(path, value.Name.Local)
			if len(path) > 32 {
				return APNProfileConfig{}, errors.New("QCMAP XML nesting exceeds limit")
			}
			joined := strings.Join(path, "/")
			if joined != "system/MobileAPCfg/MobileAPWanCfg/V4_UMTS_PROFILE_INDEX" && joined != "system/MobileAPCfg/MobileAPWanCfg/V6_UMTS_PROFILE_INDEX" {
				continue
			}
			if seen[joined] {
				return APNProfileConfig{}, errors.New("duplicate QCMAP profile index")
			}
			seen[joined] = true
			text, e := readQCMAPIndexText(decoder, value)
			if e != nil {
				return APNProfileConfig{}, errors.New("invalid QCMAP profile index")
			}
			path = path[:len(path)-1]
			index, e := strconv.Atoi(strings.TrimSpace(text))
			if e != nil || index < 0 || index > 255 {
				return APNProfileConfig{}, errors.New("QCMAP profile index out of range")
			}
			if value.Name.Local == "V4_UMTS_PROFILE_INDEX" {
				out.IPv4ProfileIndex = &index
			} else {
				out.IPv6ProfileIndex = &index
			}
		case xml.EndElement:
			if len(path) == 0 {
				return APNProfileConfig{}, errors.New("invalid QCMAP XML path")
			}
			path = path[:len(path)-1]
		}
	}
	if len(seen) == 0 {
		return APNProfileConfig{}, errors.New("QCMAP profile indexes missing")
	}
	out.ObservedAt = time.Now().UTC()
	return out, nil
}

func readQCMAPIndexText(decoder *xml.Decoder, start xml.StartElement) (string, error) {
	var text strings.Builder
	for {
		token, e := decoder.Token()
		if e != nil {
			return "", e
		}
		switch value := token.(type) {
		case xml.CharData:
			text.Write(value)
		case xml.Comment:
		case xml.EndElement:
			if value.Name != start.Name {
				return "", errors.New("unexpected profile index closing element")
			}
			return text.String(), nil
		default:
			return "", errors.New("profile index must contain only text")
		}
	}
}

const currentSettingsMask uint32 = 1<<0 | 1<<3 | 1<<8 // Profile ID/APN/IP, never credentials.
func apnCurrentRequest() []byte {
	return []byte{0, 4, 0, 0x2d, 0, 7, 0, 0x10, 4, 0, byte(currentSettingsMask & 0xff), byte(currentSettingsMask >> 8), 0, 0}
}
func validAPN(value string) bool {
	if len(value) < 1 || len(value) > 100 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// ParseAPNCurrentSettings validates the WDS service payload and emits only the
// three requested settings. Unexpected credential TLVs are never retained.
func ParseAPNCurrentSettings(frame []byte) (APNObservation, error) {
	out := APNObservation{Source: "gobinet-wds-current-settings"}
	if len(frame) < 7 || len(frame) > maximumQMIFrame || int(binary.LittleEndian.Uint16(frame[5:7])) != len(frame)-7 {
		return out, errors.New("invalid WDS frame size")
	}
	if frame[0] != 2 || binary.LittleEndian.Uint16(frame[1:3]) != 4 || binary.LittleEndian.Uint16(frame[3:5]) != 0x002d {
		return out, errUnrelatedQMI
	}
	success := false
	seen := map[byte]bool{}
	for pos := 7; pos < len(frame); {
		if len(frame)-pos < 3 {
			return APNObservation{}, errors.New("truncated WDS TLV")
		}
		kind := frame[pos]
		size := int(binary.LittleEndian.Uint16(frame[pos+1 : pos+3]))
		pos += 3
		if size > len(frame)-pos || seen[kind] {
			return APNObservation{}, errors.New("invalid WDS TLV bounds or duplicate")
		}
		seen[kind] = true
		value := frame[pos : pos+size]
		pos += size
		switch kind {
		case 2:
			if size != 4 {
				return APNObservation{}, errors.New("invalid WDS operation result")
			}
			result, code := binary.LittleEndian.Uint16(value), binary.LittleEndian.Uint16(value[2:])
			if result != 0 || code != 0 {
				return APNObservation{}, fmt.Errorf("WDS Get Current Settings failed: result=%d code=%d", result, code)
			}
			success = true
		case 0x14:
			if size == 0 {
				continue
			}
			name := string(value)
			if !validAPN(name) {
				return APNObservation{}, errors.New("invalid APN name")
			}
			out.APN = &name
		case 0x1e:
			if size != 4 {
				return APNObservation{}, errors.New("invalid WDS IPv4 length")
			}
			v := binary.LittleEndian.Uint32(value)
			ip := net.IPv4(byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
			if !ip.IsUnspecified() && !ip.IsLoopback() && !ip.IsMulticast() && !ip.IsLinkLocalUnicast() && v != 0xffffffff {
				address := ip.String()
				out.WANIPv4 = &address
			}
		case 0x1f:
			if size != 2 {
				return APNObservation{}, errors.New("invalid WDS profile length")
			}
			family := map[byte]string{0: "3gpp", 1: "3gpp2"}[value[0]]
			if family != "" {
				index := int(value[1])
				out.ProfileType = &family
				out.ProfileIndex = &index
			}
		}
	}
	if !success {
		return APNObservation{}, errors.New("WDS operation result missing")
	}
	out.Current = true
	out.ObservedAt = time.Now().UTC()
	return out, nil
}
