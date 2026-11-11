package modem

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestQCMAPProfileConfigIsNotCurrentAPNAndNeverExportsCredentials(t *testing.T) {
	xml := `<?xml version="1.0"?><system><MobileAPCfg><MobileAPWanCfg><V4_UMTS_PROFILE_INDEX>1</V4_UMTS_PROFILE_INDEX><V6_UMTS_PROFILE_INDEX>2</V6_UMTS_PROFILE_INDEX><Username>secret-user</Username><Password>secret-password</Password><APN>configured.example</APN></MobileAPWanCfg><DDNSPassword>secret-ddns</DDNSPassword></MobileAPCfg></system>`
	out, e := ParseQCMAPProfileConfig(strings.NewReader(xml))
	if e != nil || out.Current || out.IPv4ProfileIndex == nil || *out.IPv4ProfileIndex != 1 || *out.IPv6ProfileIndex != 2 {
		t.Fatal(out, e)
	}
	raw, _ := json.Marshal(out)
	if bytes.Contains(raw, []byte("secret")) || bytes.Contains(raw, []byte("configured.example")) {
		t.Fatal("unrequested data escaped", string(raw))
	}
}
func TestQCMAPConfigSizePathDuplicateAndDirective(t *testing.T) {
	for _, raw := range []string{`<system><V4_UMTS_PROFILE_INDEX>1</V4_UMTS_PROFILE_INDEX></system>`, `<system><MobileAPCfg><MobileAPWanCfg><V4_UMTS_PROFILE_INDEX>256</V4_UMTS_PROFILE_INDEX></MobileAPWanCfg></MobileAPCfg></system>`, `<system><MobileAPCfg><MobileAPWanCfg><V4_UMTS_PROFILE_INDEX>1</V4_UMTS_PROFILE_INDEX><V4_UMTS_PROFILE_INDEX>2</V4_UMTS_PROFILE_INDEX></MobileAPWanCfg></MobileAPCfg></system>`, `<!DOCTYPE system><system/>`, strings.Repeat("x", maximumAPNConfig+1)} {
		if _, e := ParseQCMAPProfileConfig(strings.NewReader(raw)); e == nil {
			t.Fatal("unsafe config accepted")
		}
	}
}
func TestQCMAPRejectsMultipleRootsNestedValuesAndNamespaceSpoofing(t *testing.T) {
	good := `<system><MobileAPCfg><MobileAPWanCfg><V4_UMTS_PROFILE_INDEX>1</V4_UMTS_PROFILE_INDEX></MobileAPWanCfg></MobileAPCfg></system>`
	for _, raw := range []string{
		good + `<another/>`,
		good + good,
		`trailing` + good,
		good + `trailing`,
		strings.Replace(good, `>1</V4`, `><nested>1</nested></V4`, 1),
		strings.Replace(good, `<system>`, `<system xmlns="urn:unrelated">`, 1),
		strings.Replace(good, `<V4_UMTS_PROFILE_INDEX>`, `<V4_UMTS_PROFILE_INDEX xmlns="urn:unrelated">`, 1),
	} {
		if _, e := ParseQCMAPProfileConfig(strings.NewReader(raw)); e == nil {
			t.Fatal("ambiguous XML accepted")
		}
	}
}
func TestAPNRequestIsOnlyCurrentSettingsAndNoCredentialMask(t *testing.T) {
	got := apnCurrentRequest()
	want := []byte{0, 4, 0, 0x2d, 0, 7, 0, 0x10, 4, 0, 9, 1, 0, 0}
	if !bytes.Equal(got, want) {
		t.Fatal(got)
	}
}
func TestAPNCurrentTypedWhitelistAndIPv4Endian(t *testing.T) {
	frame := statusFrame(4, 0x2d, map[byte][]byte{0x14: []byte("test.example"), 0x1e: {9, 113, 0, 203}, 0x1f: {0, 1}, 0x1b: []byte("secret-user"), 0x1c: []byte("secret-password")})
	out, e := ParseAPNCurrentSettings(frame)
	if e != nil || !out.Current || out.APN == nil || *out.APN != "test.example" || out.WANIPv4 == nil || *out.WANIPv4 != "203.0.113.9" || out.ProfileType == nil || *out.ProfileType != "3gpp" || *out.ProfileIndex != 1 {
		t.Fatal(out, e)
	}
	raw, _ := json.Marshal(out)
	if bytes.Contains(raw, []byte("secret")) {
		t.Fatal("credential TLV escaped")
	}
}
func TestAPNMissingUnknownAndUnsafeValues(t *testing.T) {
	out, e := ParseAPNCurrentSettings(statusFrame(4, 0x2d, map[byte][]byte{0x1e: {0, 0, 0, 0}, 0x1f: {9, 1}}))
	if e != nil || out.APN != nil || out.WANIPv4 != nil || out.ProfileType != nil {
		t.Fatal(out, e)
	}
	for _, fields := range []map[byte][]byte{{0x14: []byte("user:password@apn")}, {0x14: []byte("bad\nname")}, {0x1e: {1, 2, 3}}, {0x1f: {0}}} {
		if _, e = ParseAPNCurrentSettings(statusFrame(4, 0x2d, fields)); e == nil {
			t.Fatal("invalid APN settings accepted")
		}
	}
	frame := statusFrame(4, 0x2d, nil)
	frame[5]++
	if _, e = ParseAPNCurrentSettings(frame); e == nil {
		t.Fatal("invalid outer size accepted")
	}
}
func FuzzAPNCurrentSettingsBounds(f *testing.F) {
	f.Add(statusFrame(4, 0x2d, map[byte][]byte{0x14: []byte("test.example")}))
	f.Fuzz(func(t *testing.T, raw []byte) { _, _ = ParseAPNCurrentSettings(raw) })
}

func TestNativeAPNOverrideDropsCredentialsAndIsOnlyConfigured(t *testing.T) {
	out, e := ParseNativeAPNOverride(strings.NewReader("one.example,secret-one,private-one,3\r\ntwo.example,secret-two,private-two,0\r\n"))
	if e != nil || out.Current || len(out.Profiles) != 2 || out.Profiles[0].APN != "one.example" || out.Profiles[1].Slot != 2 {
		t.Fatal(out, e)
	}
	raw, _ := json.Marshal(out)
	if bytes.Contains(raw, []byte("secret")) || bytes.Contains(raw, []byte("private")) {
		t.Fatal("credentials escaped", string(raw))
	}
}
func TestNativeAPNOverrideStrictRowsAndStockBounds(t *testing.T) {
	for _, text := range []string{"one.example,,,0\ntwo.example,,,0", "one.example,,,0\r\n", "one.example,,,4\r\ntwo.example,,,0", "one.example,,,0,extra\r\ntwo.example,,,0", "\ufeffone.example,,,0\r\ntwo.example,,,0", strings.Repeat("x", 4097)} {
		if _, e := ParseNativeAPNOverride(strings.NewReader(text)); e == nil {
			t.Fatal("unsafe override accepted")
		}
	}
}
