package nasbackup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testInput() ConfigInput {
	return ConfigInput{Host: "nas.example.test", Share: "recordings", Directory: "private/ncc", Username: "fixture-user", Password: "fixture-secret"}
}
func TestConfigPrivateRoundtripAndRedaction(t *testing.T) {
	input := testInput()
	cfg, err := NewConfig(input)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(privateTempDir(t), "private", "config.json")
	if err = SaveConfig(filename, cfg); err != nil {
		t.Fatal(err)
	}
	read, err := LoadConfig(filename)
	if err != nil || read != cfg {
		t.Fatal("private config roundtrip", err)
	}
	if runtime.GOOS == "linux" {
		info, _ := os.Stat(filename)
		if info.Mode().Perm() != 0600 {
			t.Fatal("config not 0600")
		}
	}
	for _, v := range []any{input, cfg, &SMBRemote{config: cfg}} {
		raw, _ := json.Marshal(v)
		text := fmt.Sprintf("%v %+v %#v", v, v, v) + string(raw)
		for _, secret := range []string{input.Host, input.Share, input.Directory, input.Username, input.Password} {
			if strings.Contains(text, secret) {
				t.Fatal("private NAS configuration escaped redaction")
			}
		}
	}
	if !cfg.View().UsernameConfigured || !cfg.View().PasswordConfigured {
		t.Fatal("credential configuration missing")
	}
}
func TestConfigScopeValidation(t *testing.T) {
	for _, change := range []func(*ConfigInput){func(v *ConfigInput) { v.Host = "smb://host" }, func(v *ConfigInput) { v.Share = "../other" }, func(v *ConfigInput) { v.Directory = "../outside" }, func(v *ConfigInput) { v.Directory = "/absolute" }, func(v *ConfigInput) { v.Directory = `\\host\share` }, func(v *ConfigInput) { v.Directory = "private/../outside" }, func(v *ConfigInput) { v.Directory = "private/ads:stream" }, func(v *ConfigInput) { v.Password = "" }} {
		input := testInput()
		change(&input)
		if _, err := NewConfig(input); err == nil {
			t.Fatal("unsafe configuration accepted")
		}
	}
	for _, name := range []string{"../file.MP4", "arbitrary.part", `ncc-abc.part\..\elsewhere`, "FILEM_261112_120000_0000.MP4"} {
		if generatedName.MatchString(name) {
			t.Fatal("arbitrary remote path accepted")
		}
	}
}

func TestSMBShareRootIsExplicitAndKeepsGeneratedNamesScoped(t *testing.T) {
	for _, dir := range []string{"", "."} {
		input := testInput()
		input.Directory = dir
		cfg, err := NewConfig(input)
		if err != nil || cfg.Target().Directory != "." {
			t.Fatal("share root rejected", err)
		}
		if strings.Contains(cfg.TargetKey(), input.Password) {
			t.Fatal("target key leaked credential")
		}
	}
	for _, dir := range []string{"..", "./..", "/", "\\", "one/../../"} {
		input := testInput()
		input.Directory = dir
		if _, err := NewConfig(input); err == nil {
			t.Fatal("root scope expanded", dir)
		}
	}
}
func TestConfigRejectsSymlinkAndMalformedPrivateJSON(t *testing.T) {
	dir := privateTempDir(t)
	filename := filepath.Join(dir, "config.json")
	if err := os.WriteFile(filename, []byte(`{"host":"private","unexpected":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(filename); err == nil {
		t.Fatal("malformed config accepted")
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(filename, link); err != nil {
		t.Skip("host symlink unavailable")
	}
	if _, err := LoadConfig(link); err == nil {
		t.Fatal("symlink credential file accepted")
	}
}
