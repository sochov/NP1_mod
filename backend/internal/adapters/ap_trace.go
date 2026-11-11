package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Private startup timing records distinguish process identity races from external
// interface removal. No SSID, MAC, IP, PSK or raw process arguments are recorded.
func (r *apRuntime) traceStart(stage string) {
	_, e := os.Stat("/sys/class/net/" + apInterface)
	value := struct {
		At                 time.Time `json:"at"`
		Stage              string    `json:"stage"`
		InterfacePresent   bool      `json:"interfacePresent"`
		InterfaceOwned     bool      `json:"interfaceOwned"`
		HostProcessMatches bool      `json:"hostProcessMatches"`
	}{time.Now().UTC(), stage, e == nil, apOwnsInterface(r.ownership), apMatchesProcess(r.ownership.Hostapd)}
	f, e := os.OpenFile(filepath.Join(apPrivate(r.directory), "startup.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		return
	}
	defer f.Close()
	if st, e := f.Stat(); e == nil && st.Size() < 32768 {
		_ = json.NewEncoder(f).Encode(value)
	}
}
