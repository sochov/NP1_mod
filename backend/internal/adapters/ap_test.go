package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPConfigurationValidationAndSecretRedaction(t *testing.T) {
	for _, test := range []struct {
		ssid, password string
		enabled, valid bool
	}{{"Simple LAN", "12345678", true, true}, {"管理用", "long enough password", true, true}, {"x", "", false, true}, {"x", "", true, false}, {"x\ninterface=wlan0", "12345678", true, false}, {strings.Repeat("a", 33), "12345678", true, false}, {"x", "1234567", true, false}, {"x", "password\ninterface=wlan0", true, false}, {"x", "日本語password", true, false}} {
		if e := apValidate(test.ssid, test.password, test.enabled); (e == nil) != test.valid {
			t.Fatalf("validation mismatch (enabled=%v valid=%v)", test.enabled, test.valid)
		}
	}
	c := apConfiguration{Revision: 2, Enabled: true, SSID: "Example", Passphrase: "secret-test-passphrase"}
	b, e := json.Marshal(apPublic(c))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), c.Passphrase) || strings.Contains(string(b), `"passphrase"`) {
		t.Fatal("AP public settings leaked credential")
	}
	if !strings.Contains(string(b), `"passphraseConfigured":true`) {
		t.Fatal("credential presence not exposed safely")
	}
}
func TestAPHostapdConfigurationPreventsCredentialInjection(t *testing.T) {
	c := apConfiguration{SSID: "quote=\"example\\SSID", Passphrase: "quote \" and slash \\ password"}
	value := apHostapdConfig(c, 6, "/run/test-private")
	if strings.Contains(value, c.SSID) || strings.Contains(value, c.Passphrase) || strings.Contains(value, "wpa_passphrase=") {
		t.Fatal("plaintext credential used in generated AP config")
	}
	for _, line := range []string{"interface=nccap0\n", "channel=6\n", "ssid2=", "wpa_psk=", "ctrl_interface=/run/test-private/private/ctrl\n", "logger_syslog=0\n"} {
		if !strings.Contains(value, line) {
			t.Fatalf("missing fixed AP configuration directive %q", line)
		}
	}
	for _, line := range strings.Split(value, "\n") {
		if strings.HasPrefix(line, "wpa_psk=") && len(strings.TrimPrefix(line, "wpa_psk=")) != 64 {
			t.Fatal("invalid WPA2 PSK derivation")
		}
	}
}
func TestAPDHCPIsLANOnlyAndExplicitlyScoped(t *testing.T) {
	value := apDHCPConfig("/run/example")
	for _, line := range []string{"port=0\n", "interface=nccap0\n", "bind-interfaces\n", "dhcp-option=3\n", "dhcp-option=6\n", "no-resolv\n", "no-hosts\n", "log-facility=-\n", "dhcp-range=192.168.226.20,192.168.226.99,255.255.255.0,10m\n"} {
		if !strings.Contains(value, line) {
			t.Fatalf("DHCP missing safety directive %q", line)
		}
	}
	for _, forbidden := range []string{"interface=wlan0", "interface=wlan1", "interface=bridge0", "conf-dir", "dhcp-script", "server=", "enable-ra", "/etc/dnsmasq"} {
		if strings.Contains(value, forbidden) {
			t.Fatal("unscoped DHCP configuration")
		}
	}
}
func TestAPSubnetOverlapRejectsRoutesAndAddresses(t *testing.T) {
	for _, value := range []string{"192.168.226.0/24 dev wlan0", "192.168.0.0/16 via 10.0.0.1", "local 192.168.226.1 dev bridge0 table local", "1: bridge0 inet 192.168.226.88/28 brd 192.168.226.95 scope global"} {
		if !apOverlap(value) {
			t.Fatal("overlapping management subnet accepted")
		}
	}
	for _, value := range []string{"default via 192.168.77.1 dev wlan0\n192.168.77.0/24 dev wlan0", "100.64.0.0/10 dev tailscale0", "192.168.225.0/24 dev bridge0\n192.168.227.0/24 dev bridge1", "default dev tailscale0 table 52"} {
		if apOverlap(value) {
			t.Fatal("unrelated route treated as overlap")
		}
	}
}
func TestAPCountRequiresActualEnabledMatchingChannel(t *testing.T) {
	count, e := apHostapdStatus("state=ENABLED\nchannel=6\nnum_sta[0]=0\n", 6)
	if e != nil || count == nil || *count != 0 {
		t.Fatal("measured zero must be known", count, e)
	}
	count, e = apHostapdStatus("state=ENABLED\nchannel=6\n", 6)
	if e != nil || count != nil {
		t.Fatal("missing count must stay unknown")
	}
	for _, value := range []string{"state=DISABLED\nchannel=6\nnum_sta[0]=0", "state=ENABLED\nchannel=11\nnum_sta[0]=2", "state=ENABLED\nchannel=6\nnum_sta[0]=-1"} {
		if _, e := apHostapdStatus(value, 6); e == nil {
			t.Fatal("unconfirmed AP accepted")
		}
	}
}
func TestAPDHCPSocketOwnerParsing(t *testing.T) {
	value := "  sl local_address rem_address st tx_queue tr retrnsmt uid timeout inode\n 1: 00000000:0043 00000000:0000 07 00000000:00000000 00:00000000 00000000 0 0 1234 2\n 2: 00000000:0035 00000000:0000 07 00000000:00000000 00:00000000 00000000 0 0 9999 2\n"
	inodes := apSocketInodes(value, 67)
	if len(inodes) != 1 || !inodes["1234"] || inodes["9999"] {
		t.Fatal("DHCP socket owner matching failed")
	}
}
func TestAPLoadRejectsPublicOrSymlinkConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	c, e := apLoadConfig(path)
	if e != nil || c.Enabled || c.Revision != 0 || c.Passphrase != "" {
		t.Fatal("missing config must remain disabled")
	}
	if e = os.WriteFile(path, []byte(`{"revision":1,"enabled":false,"ssid":"test","passphrase":""}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = apLoadConfig(path); e != nil {
		t.Fatal(e)
	}
	if os.PathSeparator != '\\' {
		if e = os.Chmod(path, 0644); e != nil {
			t.Fatal(e)
		}
		if _, e = apLoadConfig(path); e == nil {
			t.Fatal("world-readable AP secret config accepted")
		}
	}
	link := filepath.Join(dir, "link.json")
	if e = os.Symlink(path, link); e == nil {
		if _, e = apLoadConfig(link); e == nil {
			t.Fatal("symlink AP config accepted")
		}
	}
}
