// Package otaguard adds layers around the stock OTA presence flag (package otapolicy):
//
//  1. It keeps the flag in place (otapolicy.Enforce) while the owner's choice is "stopped".
//  2. It rejects outgoing IPv4 traffic of the stock Update account, the only process that
//     runs as that user, in an NCC-owned iptables chain. Loopback stays allowed; D-Bus and
//     other Unix sockets are not affected. Nothing else on the device is filtered.
//  3. It reports what would precede an update: staged packages in the Update work area, a
//     non-empty download list, the SD-card update trigger file and a changed Update binary.
//
// It never stops, masks or signals stock services, never deletes stock files, and never
// interrupts an update that is already running.
package otaguard

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const Chain = "NCC_OTA_V1"

// Paths and commands are fields so that tests can run against fixtures.
type Guard struct {
	Passwd, ProcRoot, UpdateDir, SDTrigger, IPv6Routes string
	// Owned marks the chain as created by NCC; only an owned chain is removed.
	Owned, StatusPath string
	Account           string
	Run               func(ctx context.Context, name string, args ...string) ([]byte, error)
	Enforce           func() (FlagState, error)
	Firmware          func() bool
	Now               func() time.Time
}

// FlagState is the part of the OTA policy the guard needs.
type FlagState struct {
	Disabled, Managed, Protected bool
	Heals                        int
	LastHeal                     *time.Time
}

type Network struct {
	// Wanted: the owner has not re-enabled OTA. Active: the chain and its jump are in place exactly as planned.
	Wanted   bool   `json:"wanted"`
	Active   bool   `json:"active"`
	UID      int    `json:"uid"`
	Packets  uint64 `json:"rejectedPackets"`
	Error    string `json:"error,omitempty"`
	IPv6Exit bool   `json:"ipv6Exit"`
}
type Staging struct {
	WorkFiles     int    `json:"workFiles"`
	WorkBytes     int64  `json:"workBytes"`
	OfferedItems  int    `json:"offeredItems"`
	ListReadable  bool   `json:"listReadable"`
	SDTrigger     bool   `json:"sdTrigger"`
	FirmwareKnown bool   `json:"firmwareKnown"`
	UpdateRunning bool   `json:"updateRunning"`
	UpdateUID     int    `json:"updateUID"`
	Error         string `json:"error,omitempty"`
}
type Report struct {
	CheckedAt time.Time  `json:"checkedAt"`
	Flag      FlagReport `json:"flag"`
	Network   Network    `json:"network"`
	Staging   Staging    `json:"staging"`
	// Alerts are stable codes; the UI words them.
	Alerts []string `json:"alerts"`
}
type FlagReport struct {
	Present   bool       `json:"present"`
	Managed   bool       `json:"managed"`
	Protected bool       `json:"protected"`
	Heals     int        `json:"heals"`
	LastHeal  *time.Time `json:"lastHeal,omitempty"`
	Error     string     `json:"error,omitempty"`
}

func Device(enforce func() (FlagState, error), firmware func() bool) Guard {
	return Guard{
		Passwd: "/etc/passwd", ProcRoot: "/proc", UpdateDir: "/pio_data/SYSTEM/Update", SDTrigger: "/mnt/sdcard/update.Auth",
		IPv6Routes: "/proc/net/ipv6_route", Owned: "/overlay/owner-apps/np1-control/private/ota-policy/network-owned",
		StatusPath: "/run/np1-control/ota-guard.json", Account: "Update",
		Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		},
		Enforce: enforce, Firmware: firmware, Now: time.Now,
	}
}

// AccountUID reads the numeric id of the stock Update account. Root, duplicates and
// malformed entries are refused so that a rule can never match every process.
func (g Guard) AccountUID() (int, error) {
	raw, e := os.ReadFile(g.Passwd)
	if e != nil {
		return 0, e
	}
	found := -1
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) < 7 || fields[0] != g.Account {
			continue
		}
		uid, e := strconv.Atoi(fields[2])
		if e != nil || found >= 0 {
			return 0, errors.New("OTA_GUARD_ACCOUNT_AMBIGUOUS")
		}
		found = uid
	}
	if found <= 0 {
		return 0, errors.New("OTA_GUARD_ACCOUNT_UNSAFE")
	}
	return found, nil
}

// Rules is the whole chain body; the guard always rebuilds it from this list.
func Rules(uid int) [][]string {
	owner := strconv.Itoa(uid)
	return [][]string{
		{"-A", Chain, "-o", "lo", "-j", "RETURN"},
		{"-A", Chain, "-p", "tcp", "-m", "owner", "--uid-owner", owner, "-m", "comment", "--comment", "ncc-ota-guard", "-j", "REJECT", "--reject-with", "tcp-reset"},
		{"-A", Chain, "-m", "owner", "--uid-owner", owner, "-m", "comment", "--comment", "ncc-ota-guard", "-j", "REJECT", "--reject-with", "icmp-port-unreachable"},
	}
}
func (g Guard) iptables(ctx context.Context, args ...string) ([]byte, error) {
	return g.Run(ctx, "iptables", append([]string{"-w", "5"}, args...)...)
}
func (g Guard) chainExists(ctx context.Context) bool {
	_, e := g.iptables(ctx, "-S", Chain)
	return e == nil
}

// inPlace checks the live chain against the plan with iptables -C, so that the way a
// particular iptables version prints matches does not matter: exactly the planned rules,
// loopback first, and a jump from OUTPUT.
func (g Guard) inPlace(ctx context.Context, uid int) bool {
	raw, e := g.iptables(ctx, "-S", Chain)
	if e != nil {
		return false
	}
	var rules []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "-A ") {
			rules = append(rules, line)
		}
	}
	planned := Rules(uid)
	if len(rules) != len(planned) || !strings.Contains(rules[0], "-o lo") {
		return false
	}
	for _, rule := range planned {
		if _, e = g.iptables(ctx, append([]string{"-C"}, rule[1:]...)...); e != nil {
			return false
		}
	}
	_, e = g.iptables(ctx, "-C", "OUTPUT", "-j", Chain)
	return e == nil
}

// Block installs or repairs the chain. A chain of the same name that NCC did not create is refused.
func (g Guard) Block(ctx context.Context, uid int) error {
	if g.inPlace(ctx, uid) {
		return nil
	}
	if g.chainExists(ctx) {
		if _, e := os.Stat(g.Owned); e != nil {
			return errors.New("OTA_GUARD_CHAIN_UNMANAGED")
		}
	} else {
		if e := os.WriteFile(g.Owned, []byte("NCC_OTA_V1\n"), 0600); e != nil {
			return e
		}
		if _, e := g.iptables(ctx, "-N", Chain); e != nil {
			return errors.New("OTA_GUARD_CHAIN_CREATE_FAILED")
		}
	}
	if _, e := g.iptables(ctx, "-F", Chain); e != nil {
		return errors.New("OTA_GUARD_CHAIN_FLUSH_FAILED")
	}
	for _, rule := range Rules(uid) {
		if _, e := g.iptables(ctx, rule...); e != nil {
			return errors.New("OTA_GUARD_RULE_FAILED")
		}
	}
	if _, e := g.iptables(ctx, "-C", "OUTPUT", "-j", Chain); e != nil {
		if _, e = g.iptables(ctx, "-I", "OUTPUT", "1", "-j", Chain); e != nil {
			return errors.New("OTA_GUARD_JUMP_FAILED")
		}
	}
	if !g.inPlace(ctx, uid) {
		return errors.New("OTA_GUARD_READBACK_FAILED")
	}
	return nil
}

// Release removes only an owned chain and its jumps.
func (g Guard) Release(ctx context.Context) error {
	if _, e := os.Stat(g.Owned); e != nil {
		return nil
	}
	for i := 0; i < 8; i++ {
		if _, e := g.iptables(ctx, "-C", "OUTPUT", "-j", Chain); e != nil {
			break
		}
		if _, e := g.iptables(ctx, "-D", "OUTPUT", "-j", Chain); e != nil {
			return errors.New("OTA_GUARD_JUMP_REMOVE_FAILED")
		}
	}
	if g.chainExists(ctx) {
		if _, e := g.iptables(ctx, "-F", Chain); e != nil {
			return errors.New("OTA_GUARD_CHAIN_FLUSH_FAILED")
		}
		if _, e := g.iptables(ctx, "-X", Chain); e != nil {
			return errors.New("OTA_GUARD_CHAIN_REMOVE_FAILED")
		}
	}
	return os.Remove(g.Owned)
}

var counterLine = regexp.MustCompile(`^\s*(\d+)\s+\d+\s+REJECT\b`)

func (g Guard) rejected(ctx context.Context) uint64 {
	raw, e := g.iptables(ctx, "-L", Chain, "-v", "-x", "-n")
	if e != nil {
		return 0
	}
	var total uint64
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		if m := counterLine.FindStringSubmatch(scanner.Text()); m != nil {
			n, _ := strconv.ParseUint(m[1], 10, 64)
			total += n
		}
	}
	return total
}

// ipv6Exit reports an IPv6 default route; the device has no ip6tables, so IPv6 is not filtered.
func (g Guard) ipv6Exit() bool {
	raw, e := os.ReadFile(g.IPv6Routes)
	if e != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		// destination ::/0 via a non-loopback device
		if len(f) >= 10 && f[0] == strings.Repeat("0", 32) && f[1] == "00" && f[9] != "lo" {
			return true
		}
	}
	return false
}

// staging inspects without modifying. Directory walks are bounded.
func (g Guard) staging() Staging {
	var s Staging
	work := filepath.Join(g.UpdateDir, "work")
	seen := 0
	walkErr := filepath.WalkDir(work, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		seen++
		if seen > 4096 {
			return fs.SkipAll
		}
		if d.Type().IsRegular() {
			if info, e := d.Info(); e == nil {
				s.WorkFiles++
				s.WorkBytes += info.Size()
			}
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, fs.ErrNotExist) {
		s.Error = "OTA_GUARD_WORK_UNREADABLE"
	}
	if raw, e := os.ReadFile(filepath.Join(g.UpdateDir, "downloadlist.xml")); e == nil && len(raw) <= 1<<20 {
		s.ListReadable = true
		s.OfferedItems = offered(raw)
	}
	if _, e := os.Lstat(g.SDTrigger); e == nil {
		s.SDTrigger = true
	}
	s.FirmwareKnown = g.Firmware == nil || g.Firmware()
	s.UpdateRunning, s.UpdateUID = g.updateProcess()
	return s
}

var itemTag = regexp.MustCompile(`<item[\s>/]`)

// offered counts <item> entries inside <items>; the cached list is empty when no update is offered.
func offered(raw []byte) int {
	text := string(raw)
	start, end := strings.Index(text, "<items"), strings.Index(text, "</items>")
	if start < 0 || end < start {
		return 0
	}
	return len(itemTag.FindAllString(text[start:end], -1))
}
func (g Guard) updateProcess() (bool, int) {
	entries, e := os.ReadDir(g.ProcRoot)
	if e != nil {
		return false, 0
	}
	for _, entry := range entries {
		if _, e := strconv.Atoi(entry.Name()); e != nil {
			continue
		}
		comm, e := os.ReadFile(filepath.Join(g.ProcRoot, entry.Name(), "comm"))
		if e != nil || strings.TrimSpace(string(comm)) != "Update" {
			continue
		}
		status, e := os.ReadFile(filepath.Join(g.ProcRoot, entry.Name(), "status"))
		if e != nil {
			continue
		}
		for _, line := range strings.Split(string(status), "\n") {
			if f := strings.Fields(line); len(f) >= 2 && f[0] == "Uid:" {
				uid, _ := strconv.Atoi(f[1])
				return true, uid
			}
		}
	}
	return false, 0
}

// Check runs one round: keep the flag, sync the network layer to the owner's choice, inspect.
func (g Guard) Check(ctx context.Context) Report {
	r := Report{CheckedAt: g.Now().UTC(), Alerts: []string{}}
	flag, flagErr := g.Enforce()
	r.Flag = FlagReport{Present: flag.Disabled, Managed: flag.Managed, Protected: flag.Protected, Heals: flag.Heals, LastHeal: flag.LastHeal}
	if flagErr != nil {
		r.Flag.Error = "OTA_GUARD_FLAG_UNAVAILABLE"
	}
	// Only an explicit, managed re-enable lifts the network layer. Unknown or unreadable
	// state keeps it, so a firmware change cannot silently open the path.
	r.Network.Wanted = !(flagErr == nil && flag.Managed && !flag.Disabled)
	r.Network.IPv6Exit = g.ipv6Exit()
	uid, e := g.AccountUID()
	if e != nil {
		r.Network.Error = e.Error()
	} else {
		r.Network.UID = uid
		if r.Network.Wanted {
			if e = g.Block(ctx, uid); e != nil {
				r.Network.Error = e.Error()
			}
			r.Network.Active = e == nil && g.inPlace(ctx, uid)
			r.Network.Packets = g.rejected(ctx)
		} else if e = g.Release(ctx); e != nil {
			r.Network.Error = e.Error()
		}
	}
	r.Staging = g.staging()
	if r.Staging.UpdateRunning && uid > 0 && r.Staging.UpdateUID != uid {
		r.Alerts = append(r.Alerts, "update-account-changed")
	}
	for _, alert := range []struct {
		on   bool
		code string
	}{
		{r.Network.Wanted && !r.Flag.Present, "flag-missing"},
		{r.Network.Wanted && r.Flag.Present && !r.Flag.Protected, "flag-unprotected"},
		{r.Network.Wanted && !r.Network.Active, "network-inactive"},
		{r.Network.Wanted && r.Network.IPv6Exit, "ipv6-unfiltered"},
		{r.Staging.WorkFiles > 0, "staged-package"},
		{r.Staging.OfferedItems > 0, "update-offered"},
		{r.Staging.SDTrigger, "sd-trigger"},
		{!r.Staging.FirmwareKnown, "firmware-changed"},
	} {
		if alert.on {
			r.Alerts = append(r.Alerts, alert.code)
		}
	}
	return r
}

// Save writes the report atomically and readable by the root broker.
func (g Guard) Save(r Report) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	dir := filepath.Dir(g.StatusPath)
	f, e := os.CreateTemp(dir, ".ota-guard-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(b); e == nil {
		e = f.Chmod(0644)
	}
	if closeErr := f.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	return os.Rename(name, g.StatusPath)
}

// Load reads a report; reports older than maxAge are treated as missing.
func Load(path string, now time.Time, maxAge time.Duration) (Report, bool) {
	raw, e := os.ReadFile(path)
	if e != nil || len(raw) > 65536 {
		return Report{}, false
	}
	var r Report
	if json.Unmarshal(raw, &r) != nil || r.CheckedAt.IsZero() || now.Sub(r.CheckedAt) > maxAge || r.CheckedAt.After(now.Add(time.Minute)) {
		return Report{}, false
	}
	return r, true
}

// Loop checks every interval until ctx ends.
func (g Guard) Loop(ctx context.Context, interval time.Duration) {
	for {
		round, cancel := context.WithTimeout(ctx, 20*time.Second)
		_ = g.Save(g.Check(round))
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}
