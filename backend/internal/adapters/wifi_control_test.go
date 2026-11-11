package adapters

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeWiFi struct {
	mu      sync.Mutex
	current int
	next    int
	fields  map[int]map[string]string
	calls   []string
	fail    string
}

func newFakeWiFi() *fakeWiFi {
	return &fakeWiFi{current: 4, next: 12, fields: map[int]map[string]string{4: {"ssid": "stock-lan"}, 7: {"ssid": "other-lan"}}}
}
func (f *fakeWiFi) Request(ctx context.Context, command string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	f.calls = append(f.calls, command)
	if command == f.fail {
		return "FAIL", nil
	}
	p := strings.SplitN(command, " ", 4)
	switch p[0] {
	case "STATUS":
		if f.current < 0 {
			return "wpa_state=DISCONNECTED", nil
		}
		return fmt.Sprintf("wpa_state=COMPLETED\nid=%d\nssid=%s\nip_address=192.0.2.5", f.current, f.fields[f.current]["ssid"]), nil
	case "LIST_NETWORKS":
		s := "network id / ssid / bssid / flags\n"
		for id, values := range f.fields {
			flag := ""
			if id == f.current {
				flag = "[CURRENT]"
			}
			s += fmt.Sprintf("%d\t%s\tany\t%s\n", id, values["ssid"], flag)
		}
		return s, nil
	case "SCAN":
		return "OK", nil
	case "SCAN_RESULTS":
		return "bssid / frequency / signal level / flags / ssid\n00:11:22:33:44:55\t2412\t-61\t[WPA2-PSK-CCMP][ESS]\tNearby\n", nil
	case "ADD_NETWORK":
		id := f.next
		f.next++
		f.fields[id] = map[string]string{}
		return strconv.Itoa(id), nil
	case "SET_NETWORK":
		id, _ := strconv.Atoi(p[1])
		if f.fields[id] == nil {
			return "FAIL", nil
		}
		value := p[3]
		if p[2] == "ssid" {
			data, _ := hex.DecodeString(value)
			value = string(data)
		}
		f.fields[id][p[2]] = value
		return "OK", nil
	case "GET_NETWORK":
		id, _ := strconv.Atoi(p[1])
		value := f.fields[id][p[2]]
		if value == "" {
			return "FAIL", nil
		}
		return value, nil
	case "SELECT_NETWORK":
		id, _ := strconv.Atoi(p[1])
		if f.fields[id] == nil {
			return "FAIL", nil
		}
		f.current = id
		return "OK", nil
	case "REMOVE_NETWORK":
		id, _ := strconv.Atoi(p[1])
		if f.fields[id] == nil {
			return "FAIL", nil
		}
		delete(f.fields, id)
		return "OK", nil
	case "DISCONNECT":
		f.current = -1
		return "OK", nil
	}
	return "FAIL", nil
}
func (f *fakeWiFi) history() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}
func wifiTestController(t *testing.T) (*WiFiController, *fakeWiFi) {
	t.Helper()
	f := newFakeWiFi()
	c := NewWiFiController(t.TempDir(), f)
	t.Cleanup(func() {
		if c.timer != nil {
			c.timer.Stop()
		}
	})
	return c, f
}

func TestWiFiNewConnectionConfirmKeepsSecretPrivate(t *testing.T) {
	c, f := wifiTestController(t)
	ctx := context.Background()
	secret := "secret \"quoted\\ password"
	transaction, e := c.ConnectNew(ctx, WiFiNewConnection{"My \"LAN", "wpa2", secret})
	if e != nil {
		t.Fatal(e)
	}
	view, e := c.List(ctx)
	if e != nil || view.Transaction == nil || !view.Transaction.CanConfirm || len(view.Saved) != 0 {
		t.Fatalf("pending view %#v %v", view, e)
	}
	if _, e = c.Confirm(ctx, transaction.ID); e != nil {
		t.Fatal(e)
	}
	view, e = c.List(ctx)
	if e != nil || len(view.Saved) != 1 || !view.Saved[0].PasswordConfigured || view.Transaction.State != "confirmed" {
		t.Fatalf("confirmed view %#v %v", view, e)
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), "password\"") {
		t.Fatal("public response contains a password")
	}
	private, e := os.ReadFile(filepath.Join(c.directory, "profiles.json"))
	if e != nil || !strings.Contains(string(private), "password") {
		t.Fatal("private profile not stored")
	}
	for _, command := range f.history() {
		if command == "SAVE_CONFIG" || strings.HasPrefix(command, "REMOVE_NETWORK 4") {
			t.Fatal("modified stock persistence/network")
		}
	}
	found := false
	for _, command := range f.history() {
		if strings.HasSuffix(command, " psk \""+secret+"\"") {
			found = true
		}
	}
	if !found {
		t.Fatal("PSK did not preserve exact bytes inside stock quotes")
	}
}
func TestWiFiRollbackRestoresOldBeforeRemovingOnlyOwnedNetwork(t *testing.T) {
	c, f := wifiTestController(t)
	transaction, e := c.ConnectNew(context.Background(), WiFiNewConnection{"Temporary", "wpa2", "12345678"})
	if e != nil {
		t.Fatal(e)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	result, e := c.Rollback(cancelled, transaction.ID)
	if e != nil || result.State != "rolled-back" {
		t.Fatalf("rollback %#v %v", result, e)
	}
	history := f.history()
	restore, remove := -1, -1
	for i, command := range history {
		if command == "SELECT_NETWORK 4" {
			restore = i
		}
		if command == "REMOVE_NETWORK 12" {
			remove = i
		}
	}
	if restore < 0 || remove <= restore {
		t.Fatal("old connection must be restored before own object removal")
	}
	if _, e = os.Stat(filepath.Join(c.directory, "profiles.json")); !os.IsNotExist(e) {
		t.Fatal("unconfirmed credentials were persisted as saved profile")
	}
}
func TestWiFiTimeoutAndRestartRollBackWithoutCaller(t *testing.T) {
	c, f := wifiTestController(t)
	c.timeout = 25 * time.Millisecond
	if _, e := c.ConnectSaved(context.Background(), 7); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		done := c.pending == nil
		c.mu.Unlock()
		if done {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	c.mu.Lock()
	if c.pending != nil || c.last == nil || c.last.Reason != "confirmation-timeout" {
		t.Fatal("independent timer did not roll back")
	}
	c.mu.Unlock()
	c.timeout = time.Minute
	transaction, e := c.ConnectNew(context.Background(), WiFiNewConnection{"Restarted", "open", ""})
	if e != nil {
		t.Fatal(e)
	}
	c.timer.Stop()
	restarted := NewWiFiController(c.directory, f)
	if e = restarted.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	view, e := restarted.List(context.Background())
	if e != nil || view.Status.NetworkID == nil || *view.Status.NetworkID != 4 || view.Transaction.ID != transaction.ID || view.Transaction.Reason != "service-restarted" {
		t.Fatalf("restart recovery %#v %v", view, e)
	}
}
func TestWiFiRejectsInjectionConflictsAndForeignOwnership(t *testing.T) {
	c, f := wifiTestController(t)
	for _, change := range []WiFiNewConnection{{"bad\nssid", "open", ""}, {"ok", "wpa2", "bad\npass"}, {"ok", "enterprise", "12345678"}, {"ok", "open", "unexpected"}} {
		if _, e := c.ConnectNew(context.Background(), change); e == nil {
			t.Fatal("invalid request accepted")
		}
	}
	if len(f.history()) != 0 {
		t.Fatal("invalid input reached stock control")
	}
	transaction, e := c.ConnectNew(context.Background(), WiFiNewConnection{"Owned", "open", ""})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.ConnectSaved(context.Background(), 7); e == nil {
		t.Fatal("parallel pending transaction accepted")
	}
	f.mu.Lock()
	f.fields[12]["id_str"] = "\"foreign\""
	f.mu.Unlock()
	if _, e = c.Confirm(context.Background(), transaction.ID); e == nil {
		t.Fatal("foreign ID was confirmed")
	}
	if _, e = c.Rollback(context.Background(), transaction.ID); e == nil {
		t.Fatal("foreign ID was removed")
	}
	for _, command := range f.history() {
		if command == "REMOVE_NETWORK 12" {
			t.Fatal("foreign object removed")
		}
	}
}
func TestWiFiFailedSelectRollsBackAndDoesNotPersist(t *testing.T) {
	c, f := wifiTestController(t)
	f.fail = "SELECT_NETWORK 12"
	if _, e := c.ConnectNew(context.Background(), WiFiNewConnection{"Fail", "open", ""}); e == nil {
		t.Fatal("failed select accepted")
	}
	history := f.history()
	count := 0
	for _, command := range history {
		if command == "REMOVE_NETWORK 12" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("owned object removal count %d", count)
	}
	if c.pending != nil {
		t.Fatal("rollback journal left active")
	}
}
func TestWiFiBoundedParsersAndCachedScan(t *testing.T) {
	c, _ := wifiTestController(t)
	scan, e := c.Scan(context.Background())
	if e != nil || len(scan.Results) != 1 || scan.Results[0].Security != "wpa2" || !scan.ResultsAreCached {
		t.Fatalf("scan %#v %v", scan, e)
	}
	connection, e := parseWiFiStatus("wpa_state=COMPLETED\nid=4\nssid=Test\\x20LAN")
	if e != nil || connection.SSID != "Test LAN" {
		t.Fatal("escaped SSID decode")
	}
	for _, raw := range []string{"network id\tssid\tbssid\tflags\n4\tOK\tany\t\n4\tduplicate\tany\t", "wrong header", "network id\tssid\tbssid\tflags\n4;bad\tOK\tany\t"} {
		if _, e := parseWiFiNetworks(raw); e == nil {
			t.Fatal("malformed network list accepted")
		}
	}
}
