package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAPChannelContinuityThroughSTAAbsence(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		decision, e := apChooseChannel(11, 0, 6, fallback)
		if e != nil || decision.Channel != 11 || decision.Source != "retained" {
			t.Fatalf("fallback=%v: %#v %v", fallback, decision, e)
		}
	}
	decision, e := apChooseChannel(11, 3, 11, true)
	if e != nil || decision.Channel != 3 || decision.Source != "sta" {
		t.Fatal("valid completed channel was not selected")
	}
}
func TestAPStandaloneDefaultsOnWithoutOverridingExplicitFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	value, e := apLoadConfig(path)
	if e != nil || !value.StandaloneFallback {
		t.Fatal("new installation must keep LAN available without STA")
	}
	for _, test := range []struct {
		raw  string
		want bool
	}{{`{"ssid":"Example","enabled":false}`, true}, {`{"ssid":"Example","enabled":false,"standaloneFallback":false}`, false}} {
		if e = os.WriteFile(path, []byte(test.raw), 0600); e != nil {
			t.Fatal(e)
		}
		value, e = apLoadConfig(path)
		if e != nil {
			t.Fatal(e)
		}
		if value.StandaloneFallback != test.want {
			t.Fatal("legacy default or explicit operator setting changed")
		}
	}
}
func TestAPStandaloneStartupUsesSameBootHistoryThenChannel6(t *testing.T) {
	for _, test := range []struct {
		last   int
		want   int
		source string
	}{{0, 6, "standalone"}, {14, 6, "standalone"}, {11, 11, "last-verified"}} {
		decision, e := apChooseChannel(0, 0, test.last, true)
		if e != nil || decision.Channel != test.want || decision.Source != test.source {
			t.Fatalf("%#v %v", decision, e)
		}
	}
	if _, e := apChooseChannel(0, 0, 11, false); e == nil {
		t.Fatal("operator override ignored")
	}
}
func TestAPChannelHistoryDoesNotCrossBootOrInvalidDomain(t *testing.T) {
	directory := t.TempDir()
	if e := os.Mkdir(filepath.Join(directory, "private"), 0700); e != nil {
		t.Fatal(e)
	}
	for _, test := range []struct {
		boot    string
		channel int
		want    int
	}{{"same", 11, 11}, {"old", 11, 0}, {"same", 149, 0}} {
		raw, _ := json.Marshal(apLastChannel{test.channel, test.boot, time.Now()})
		if e := os.WriteFile(filepath.Join(directory, "private", "last-channel.json"), raw, 0600); e != nil {
			t.Fatal(e)
		}
		if got := apReadLastChannel(directory, "same"); got != test.want {
			t.Fatalf("unexpected last channel %d", got)
		}
	}
}
