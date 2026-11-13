package otaguard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeIPTables keeps chains as ordered rule lists, enough to exercise the guard's commands.
type fakeIPTables struct {
	chains map[string][]string
	calls  []string
	fail   string
}

func newFake() *fakeIPTables { return &fakeIPTables{chains: map[string][]string{"OUTPUT": {}}} }
func (f *fakeIPTables) run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name != "iptables" || len(args) < 3 || args[0] != "-w" {
		return nil, errors.New("unexpected command")
	}
	args = args[2:]
	f.calls = append(f.calls, strings.Join(args, " "))
	if f.fail != "" && strings.HasPrefix(strings.Join(args, " "), f.fail) {
		return nil, errors.New("injected")
	}
	chain := ""
	if len(args) > 1 {
		chain = args[1]
	}
	rule := strings.Join(args[2:], " ")
	switch args[0] {
	case "-N":
		if _, ok := f.chains[chain]; ok {
			return nil, errors.New("exists")
		}
		f.chains[chain] = []string{}
	case "-X":
		delete(f.chains, chain)
	case "-F":
		f.chains[chain] = []string{}
	case "-A":
		f.chains[chain] = append(f.chains[chain], rule)
	case "-I":
		f.chains[chain] = append([]string{strings.Join(args[3:], " ")}, f.chains[chain]...)
	case "-D", "-C":
		rules, ok := f.chains[chain]
		if !ok {
			return nil, errors.New("no chain")
		}
		for i, r := range rules {
			if r == rule {
				if args[0] == "-D" {
					f.chains[chain] = append(rules[:i:i], rules[i+1:]...)
				}
				return nil, nil
			}
		}
		return nil, errors.New("no rule")
	case "-S":
		rules, ok := f.chains[chain]
		if !ok {
			return nil, errors.New("no chain")
		}
		out := "-N " + chain + "\n"
		for _, r := range rules {
			// iptables prints the comment in quotes.
			out += "-A " + chain + " " + strings.ReplaceAll(r, "--comment ncc-ota-guard", `--comment "ncc-ota-guard"`) + "\n"
		}
		return []byte(out), nil
	case "-L":
		return []byte("Chain NCC_OTA_V1 (1 references)\n    pkts      bytes target     prot opt in     out     source               destination\n       0        0 RETURN     all  --  *      lo      0.0.0.0/0            0.0.0.0/0\n       7      420 REJECT     tcp  --  *      *       0.0.0.0/0            0.0.0.0/0            owner UID match 12109\n       2      120 REJECT     all  --  *      *       0.0.0.0/0            0.0.0.0/0            owner UID match 12109\n"), nil
	}
	return nil, nil
}

func fixture(t *testing.T) (Guard, *fakeIPTables) {
	t.Helper()
	base := t.TempDir()
	write := func(path, text string) {
		os.MkdirAll(filepath.Dir(path), 0700)
		if e := os.WriteFile(path, []byte(text), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write(filepath.Join(base, "passwd"), "root:x:0:0::/root:/bin/sh\nUpdate:x:12109:12109::/home/apluser:/bin/false\n")
	write(filepath.Join(base, "proc", "961", "comm"), "Update\n")
	write(filepath.Join(base, "proc", "961", "status"), "Name:\tUpdate\nUid:\t12109\t12109\t12109\t12109\n")
	write(filepath.Join(base, "Update", "downloadlist.xml"), "<?xml version='1.0'?>\n<results><resultCode>0</resultCode><result><items>\n</items></result></results>\n")
	os.MkdirAll(filepath.Join(base, "Update", "work"), 0700)
	write(filepath.Join(base, "ipv6_route"), "fd7a115ca1e000000000000000000000 30 00000000000000000000000000000000 00 00000000000000000000000000000000 00000100 00000000 00000000 00000001 tailscale0\n")
	f := newFake()
	g := Guard{
		Passwd: filepath.Join(base, "passwd"), ProcRoot: filepath.Join(base, "proc"), UpdateDir: filepath.Join(base, "Update"),
		SDTrigger: filepath.Join(base, "sdcard", "update.Auth"), IPv6Routes: filepath.Join(base, "ipv6_route"),
		Owned: filepath.Join(base, "network-owned"), StatusPath: filepath.Join(base, "ota-guard.json"), Account: "Update",
		Run:      f.run,
		Enforce:  func() (FlagState, error) { return FlagState{Disabled: true, Managed: true, Protected: true}, nil },
		Firmware: func() bool { return true },
		Now:      func() time.Time { return time.Date(2026, 11, 13, 5, 0, 0, 0, time.UTC) },
	}
	return g, f
}

func TestStoppedPolicyBlocksOnlyTheUpdateAccount(t *testing.T) {
	g, f := fixture(t)
	r := g.Check(context.Background())
	if !r.Network.Wanted || !r.Network.Active || r.Network.UID != 12109 || r.Network.Packets != 9 || r.Network.Error != "" {
		t.Fatalf("network layer: %+v", r.Network)
	}
	if got := f.chains["OUTPUT"]; len(got) != 1 || got[0] != "-j "+Chain {
		t.Fatalf("OUTPUT jump: %v", got)
	}
	rules := f.chains[Chain]
	if len(rules) != 3 || rules[0] != "-o lo -j RETURN" {
		t.Fatalf("chain: %v", rules)
	}
	for _, rule := range rules[1:] {
		if !strings.Contains(rule, "--uid-owner 12109") || !strings.Contains(rule, "-j REJECT") {
			t.Fatalf("every reject must be limited to the Update account: %q", rule)
		}
	}
	if len(r.Alerts) != 0 {
		t.Fatalf("a quiet device raises no alerts: %v", r.Alerts)
	}
	// A second round finds the chain in place and changes nothing.
	f.calls = nil
	g.Check(context.Background())
	for _, call := range f.calls {
		if strings.HasPrefix(call, "-A") || strings.HasPrefix(call, "-I") || strings.HasPrefix(call, "-F") || strings.HasPrefix(call, "-N") {
			t.Fatalf("an intact chain was rewritten: %v", f.calls)
		}
	}
}
func TestTamperedChainIsRebuiltAndExplicitEnableLiftsIt(t *testing.T) {
	g, f := fixture(t)
	g.Check(context.Background())
	f.chains[Chain] = f.chains[Chain][:1]
	f.chains["OUTPUT"] = nil
	if r := g.Check(context.Background()); !r.Network.Active {
		t.Fatalf("tampered chain not repaired: %+v", r.Network)
	}
	g.Enforce = func() (FlagState, error) { return FlagState{Disabled: false, Managed: true}, nil }
	r := g.Check(context.Background())
	if r.Network.Wanted || r.Network.Active {
		t.Fatalf("owner's re-enable must lift the block: %+v", r.Network)
	}
	if _, ok := f.chains[Chain]; ok || len(f.chains["OUTPUT"]) != 0 {
		t.Fatalf("chain left after release: %v", f.chains)
	}
	if _, e := os.Stat(g.Owned); !os.IsNotExist(e) {
		t.Fatal("ownership marker left behind")
	}
}
func TestUnknownStateKeepsTheBlock(t *testing.T) {
	g, _ := fixture(t)
	g.Enforce = func() (FlagState, error) { return FlagState{}, errors.New("unsupported Update firmware") }
	g.Firmware = func() bool { return false }
	r := g.Check(context.Background())
	if !r.Network.Wanted || !r.Network.Active {
		t.Fatalf("an unreadable policy must keep blocking: %+v", r.Network)
	}
	for _, want := range []string{"flag-missing", "firmware-changed"} {
		if !contains(r.Alerts, want) {
			t.Fatalf("missing alert %s: %v", want, r.Alerts)
		}
	}
}
func TestForeignChainAndUnsafeAccountAreRefused(t *testing.T) {
	g, f := fixture(t)
	f.chains[Chain] = []string{"-j ACCEPT"}
	r := g.Check(context.Background())
	if r.Network.Active || r.Network.Error != "OTA_GUARD_CHAIN_UNMANAGED" || f.chains[Chain][0] != "-j ACCEPT" {
		t.Fatalf("a chain NCC did not create must be left alone: %+v %v", r.Network, f.chains)
	}
	for _, passwd := range []string{"Update:x:0:0::/:/bin/false\n", "Update:x:12109:1::/:/bin/false\nUpdate:x:12110:1::/:/bin/false\n", "other:x:5:5::/:/bin/false\n"} {
		g2, f2 := fixture(t)
		os.WriteFile(g2.Passwd, []byte(passwd), 0600)
		r = g2.Check(context.Background())
		if r.Network.Active || r.Network.Error == "" || len(f2.chains[Chain]) != 0 {
			t.Fatalf("unsafe account %q produced rules: %+v", passwd, r.Network)
		}
	}
}
func TestStagingSignalsAreReported(t *testing.T) {
	g, _ := fixture(t)
	os.WriteFile(filepath.Join(g.UpdateDir, "work", "update_package"), make([]byte, 2048), 0600)
	os.WriteFile(filepath.Join(g.UpdateDir, "downloadlist.xml"), []byte("<results><result><items><item><version>2</version></item></items></result></results>"), 0600)
	os.MkdirAll(filepath.Dir(g.SDTrigger), 0700)
	os.WriteFile(g.SDTrigger, []byte("x"), 0600)
	os.WriteFile(g.IPv6Routes, []byte("00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000400 00000000 00000000 00000003 wlan0\n"), 0600)
	os.WriteFile(filepath.Join(g.ProcRoot, "961", "status"), []byte("Uid:\t0\t0\t0\t0\n"), 0600)
	r := g.Check(context.Background())
	if r.Staging.WorkFiles != 1 || r.Staging.WorkBytes != 2048 || r.Staging.OfferedItems != 1 || !r.Staging.SDTrigger || !r.Network.IPv6Exit {
		t.Fatalf("staging: %+v %+v", r.Staging, r.Network)
	}
	for _, want := range []string{"staged-package", "update-offered", "sd-trigger", "ipv6-unfiltered", "update-account-changed"} {
		if !contains(r.Alerts, want) {
			t.Fatalf("missing alert %s: %v", want, r.Alerts)
		}
	}
	// Inspection only: nothing was removed.
	if _, e := os.Stat(filepath.Join(g.UpdateDir, "work", "update_package")); e != nil {
		t.Fatal("staged package was touched")
	}
}
func TestReportRoundTripAndStaleness(t *testing.T) {
	g, _ := fixture(t)
	r := g.Check(context.Background())
	if e := g.Save(r); e != nil {
		t.Fatal(e)
	}
	if got, ok := Load(g.StatusPath, r.CheckedAt.Add(time.Minute), 2*time.Minute); !ok || !got.Network.Active {
		t.Fatalf("load: %+v %v", got, ok)
	}
	if _, ok := Load(g.StatusPath, r.CheckedAt.Add(10*time.Minute), 2*time.Minute); ok {
		t.Fatal("a stale report must not be shown as current")
	}
}
func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
