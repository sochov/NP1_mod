package adapters

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/np1-control-center/ncc/internal/config"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

type apSharingProof struct {
	Filter map[string][][]string `json:"filter"`
	NAT    map[string][][]string `json:"nat"`
	Routes []string              `json:"routes"`
}
type apSharingOwner struct {
	Version         int             `json:"version"`
	BootID          string          `json:"bootId"`
	Token           string          `json:"token"`
	ForwardOriginal string          `json:"forwardOriginal"`
	ForwardChanged  bool            `json:"forwardChanged"`
	Cleaning        bool            `json:"cleaning,omitempty"`
	Current         *apSharingProof `json:"current,omitempty"`
	Pending         *apSharingProof `json:"pending,omitempty"`
}
type apSharingEffects struct {
	run          func(context.Context, string, ...string) (string, error)
	restore      func(context.Context, string) error
	readForward  func() (string, error)
	writeForward func(string) error
	load         func() (*apSharingOwner, error)
	save         func(*apSharingOwner) error
	remove       func() error
	bootID       func() string
}
type apSharingFirewall struct {
	mu    sync.Mutex
	d     apSharingEffects
	owner *apSharingOwner
}
type apSharingSnapshot struct {
	filter, nat string
	routes      []string
	rule        string
	forward     string
}

func newAPSharingFirewall(directory string) *apSharingFirewall {
	path := filepath.Join(apPrivate(directory), "sharing-owner.json")
	return &apSharingFirewall{d: apSharingEffects{run: apRun, restore: func(ctx context.Context, text string) error {
		ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "iptables-restore", "--wait=2", "--noflush")
		cmd.Stdin = strings.NewReader(text)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if cmd.Run() != nil {
			return errors.New("AP_SHARING_FIREWALL_RESTORE_FAILED")
		}
		return nil
	}, readForward: func() (string, error) {
		raw, e := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
		return strings.TrimSpace(string(raw)), e
	}, writeForward: func(value string) error {
		return os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte(value+"\n"), 0644)
	}, load: func() (*apSharingOwner, error) {
		st, e := os.Lstat(path)
		if os.IsNotExist(e) {
			return nil, nil
		}
		if e != nil || !st.Mode().IsRegular() || st.Size() > 65536 || st.Mode().Perm()&0077 != 0 {
			return nil, errors.New("AP_SHARING_OWNER_INVALID")
		}
		raw, e := os.ReadFile(path)
		var value apSharingOwner
		if e != nil || len(raw) > 65536 || json.Unmarshal(raw, &value) != nil {
			return nil, errors.New("AP_SHARING_OWNER_INVALID")
		}
		return &value, nil
	}, save: func(value *apSharingOwner) error { return config.Atomic(path, value) }, remove: func() error {
		e := os.Remove(path)
		if os.IsNotExist(e) {
			return nil
		}
		return e
	}, bootID: func() string { return read("/proc/sys/kernel/random/boot_id") }}}
}
func sharingArguments(args []string) string {
	// iptables -S reorders source/destination/interface matches and inserts
	// protocol match modules. Compare the known fixed option/value pairs by
	// meaning, while retaining duplicates (an extra match must still differ).
	protocol := ""
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-p" {
			protocol = strings.Trim(args[i+1], "\"")
		}
	}
	pairs := []string{}
	for i := 0; i < len(args); i += 2 {
		if i+1 >= len(args) {
			return "invalid:" + strings.Join(args, " ")
		}
		key, value := args[i], strings.Trim(args[i+1], "\"")
		if key == "-m" && value == protocol && (protocol == "tcp" || protocol == "udp") {
			continue
		}
		if (key == "-s" || key == "-d") && strings.HasSuffix(value, "/32") {
			value = strings.TrimSuffix(value, "/32")
		}
		if key == "--ctstate" {
			states := strings.Split(value, ",")
			sort.Strings(states)
			value = strings.Join(states, ",")
		}
		pairs = append(pairs, key+"="+value)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, " ")
}
func sharingChain(raw, chain string) (bool, []string) {
	exists := false
	rules := []string{}
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 && (parts[0] == "-N" || parts[0] == "-P") && parts[1] == chain {
			exists = true
		}
		if len(parts) >= 2 && parts[0] == "-A" && parts[1] == chain {
			exists = true
			rules = append(rules, sharingArguments(parts[2:]))
		}
	}
	return exists, rules
}
func sharingRuleKeys(args [][]string) []string {
	out := []string{}
	for _, value := range args {
		out = append(out, sharingArguments(value))
	}
	return out
}
func sharingRouteKey(fields []string) (string, error) {
	if len(fields) == 0 {
		return "", errors.New("AP_SHARING_ROUTE_INVALID")
	}
	kind := "unicast"
	if fields[0] == "blackhole" {
		kind = "blackhole"
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return "", errors.New("AP_SHARING_ROUTE_INVALID")
	}
	destination := fields[0]
	values := map[string]string{"proto": "", "scope": "global", "metric": "0", "via": "", "dev": "", "src": ""}
	for i := 1; i < len(fields); i++ {
		key := fields[i]
		if key == "linkdown" || key == "onlink" {
			continue
		}
		if i+1 >= len(fields) {
			return "", errors.New("AP_SHARING_ROUTE_INVALID")
		}
		if key == "table" {
			if fields[i+1] != apSharingTable {
				return "", errors.New("AP_SHARING_ROUTE_INVALID")
			}
			i++
			continue
		}
		if _, ok := values[key]; !ok {
			return "", errors.New("AP_SHARING_ROUTE_INVALID")
		}
		values[key] = fields[i+1]
		i++
	}
	if values["proto"] != apSharingProtocol {
		return "", errors.New("AP_SHARING_ROUTE_NOT_OWNED")
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s", kind, destination, values["via"], values["dev"], values["src"], values["metric"], values["proto"], values["scope"]), nil
}
func (p apSharingPlan) proof() apSharingProof {
	out := apSharingProof{Filter: p.filter, NAT: p.nat, Routes: []string{}}
	for _, route := range p.routes {
		key, _ := sharingRouteKey(route)
		out.Routes = append(out.Routes, key)
	}
	sort.Strings(out.Routes)
	return out
}
func (f *apSharingFirewall) snapshot(ctx context.Context) (apSharingSnapshot, error) {
	s := apSharingSnapshot{}
	var e error
	s.filter, e = f.d.run(ctx, "iptables", "-w", "2", "-t", "filter", "-S")
	if e != nil {
		return s, e
	}
	s.nat, e = f.d.run(ctx, "iptables", "-w", "2", "-t", "nat", "-S")
	if e != nil {
		return s, e
	}
	raw, e := f.d.run(ctx, "ip", "-4", "route", "show", "table", "all")
	if e != nil {
		return s, e
	}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		ownedTable := false
		for i := 0; i < len(fields)-1; i++ {
			ownedTable = ownedTable || fields[i] == "table" && fields[i+1] == apSharingTable
		}
		if ownedTable {
			key, e := sharingRouteKey(fields)
			if e != nil {
				return s, e
			}
			s.routes = append(s.routes, key)
		}
	}
	sort.Strings(s.routes)
	raw, e = f.d.run(ctx, "ip", "-4", "rule", "show")
	if e != nil {
		return s, e
	}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == apSharingPriority+":" {
			if s.rule != "" {
				return s, errors.New("AP_SHARING_POLICY_CONFLICT")
			}
			s.rule = strings.Join(fields, " ")
		}
	}
	s.forward, e = f.d.readForward()
	if e != nil || (s.forward != "0" && s.forward != "1") {
		return s, errors.New("AP_SHARING_FORWARD_STATE_UNAVAILABLE")
	}
	return s, nil
}
func sharingExpectedRule() string {
	return apSharingPriority + ": from " + apSharingSubnet + " iif " + apInterface + " lookup " + apSharingTable
}
func sharingProofChain(proof *apSharingProof, table, chain string) []string {
	if proof == nil {
		return nil
	}
	if table == "filter" {
		return sharingRuleKeys(proof.Filter[chain])
	}
	return sharingRuleKeys(proof.NAT[chain])
}
func (f *apSharingFirewall) validate(s apSharingSnapshot) error {
	if f.owner == nil {
		return errors.New("AP_SHARING_OWNER_INVALID")
	}
	guard, _ := apBuildSharingPlan(f.owner.Token, nil)
	guardProof := guard.proof()
	for _, entry := range []struct{ table, chain, raw string }{{"filter", apSharingForwardChain, s.filter}, {"filter", apSharingDNSChain, s.filter}, {"nat", apSharingNATChain, s.nat}} {
		exists, rules := sharingChain(entry.raw, entry.chain)
		if !exists {
			continue
		}
		// Cleanup journals its intent before flushing an owned chain. A crash
		// between -F and -X may therefore leave that same chain empty.
		if f.owner.Cleaning && len(rules) == 0 {
			continue
		}
		valid := false
		for _, proof := range []*apSharingProof{f.owner.Current, f.owner.Pending, &guardProof} {
			valid = valid || reflect.DeepEqual(rules, sharingProofChain(proof, entry.table, entry.chain))
		}
		if !valid {
			return errors.New("AP_SHARING_FIREWALL_OWNERSHIP_CHANGED")
		}
	}
	allowed := map[string]bool{}
	for _, proof := range []*apSharingProof{f.owner.Current, f.owner.Pending, &guardProof} {
		if proof != nil {
			for _, route := range proof.Routes {
				allowed[route] = true
			}
		}
	}
	for _, route := range s.routes {
		if !allowed[route] {
			return errors.New("AP_SHARING_ROUTE_OWNERSHIP_CHANGED")
		}
	}
	if s.rule != "" && s.rule != sharingExpectedRule() {
		return errors.New("AP_SHARING_POLICY_CONFLICT")
	}
	return nil
}
func (f *apSharingFirewall) loadOwner() error {
	if f.owner != nil {
		return nil
	}
	owner, e := f.d.load()
	if e != nil {
		return e
	}
	if owner != nil {
		if owner.Version != 1 || owner.BootID == "" || owner.BootID != f.d.bootID() || (owner.ForwardOriginal != "0" && owner.ForwardOriginal != "1") {
			return errors.New("AP_SHARING_OWNER_INVALID")
		}
		if _, e = apBuildSharingPlan(owner.Token, nil); e != nil {
			return e
		}
		f.owner = owner
	}
	return nil
}
func (f *apSharingFirewall) apply(ctx context.Context, u *APSharingUpstream) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := f.loadOwner(); e != nil {
		return e
	}
	snapshot, e := f.snapshot(ctx)
	if e != nil {
		return e
	}
	if f.owner == nil {
		for _, entry := range []struct{ chain, raw string }{{apSharingForwardChain, snapshot.filter}, {apSharingDNSChain, snapshot.filter}, {apSharingNATChain, snapshot.nat}} {
			if exists, _ := sharingChain(entry.raw, entry.chain); exists {
				return errors.New("AP_SHARING_FIREWALL_CONFLICT")
			}
		}
		if len(snapshot.routes) > 0 || snapshot.rule != "" {
			return errors.New("AP_SHARING_POLICY_CONFLICT")
		}
		var bytes [16]byte
		if _, e = rand.Read(bytes[:]); e != nil {
			return errors.New("AP_SHARING_OWNER_UNAVAILABLE")
		}
		f.owner = &apSharingOwner{Version: 1, BootID: f.d.bootID(), Token: hex.EncodeToString(bytes[:]), ForwardOriginal: snapshot.forward}
		if f.owner.BootID == "" {
			return errors.New("AP_SHARING_OWNER_UNAVAILABLE")
		}
	}
	if e = f.validate(snapshot); e != nil {
		return e
	}
	if f.owner.Cleaning {
		return errors.New("AP_SHARING_CLEANUP_REQUIRED")
	}
	plan, e := apBuildSharingPlan(f.owner.Token, u)
	if e != nil {
		return e
	}
	proof := plan.proof()
	if f.owner.Current != nil && reflect.DeepEqual(*f.owner.Current, proof) && sharingSnapshotComplete(snapshot, proof, f.owner.Token) {
		return nil
	}
	f.owner.Pending = &proof
	if e = f.d.save(f.owner); e != nil {
		return e
	}
	guard, _ := apBuildSharingPlan(f.owner.Token, nil)
	if e = f.d.restore(ctx, guard.restoreText()); e != nil {
		return e
	}
	for _, jump := range apSharingJumps(f.owner.Token) {
		raw := snapshot.filter
		if jump[0] == "nat" {
			raw = snapshot.nat
		}
		if !sharingHasJump(raw, jump) {
			args := append([]string{"-w", "2", "-t", jump[0], "-I", jump[1], "1"}, jump[2:]...)
			if _, e = f.d.run(ctx, "iptables", args...); e != nil {
				return e
			}
		}
	}
	if snapshot.rule == "" {
		if _, e = f.d.run(ctx, "ip", "-4", "rule", "add", "priority", apSharingPriority, "from", apSharingSubnet, "iif", apInterface, "lookup", apSharingTable); e != nil {
			return e
		}
	}
	if _, e = f.d.run(ctx, "ip", "-4", "route", "flush", "table", apSharingTable); e != nil {
		return e
	}
	for _, route := range plan.routes {
		args := append([]string{"-4", "route", "add", "table", apSharingTable}, route...)
		if _, e = f.d.run(ctx, "ip", args...); e != nil {
			return e
		}
	}
	if snapshot.forward != "1" {
		f.owner.ForwardChanged = f.owner.ForwardOriginal == "0"
		if e = f.d.save(f.owner); e != nil {
			return e
		}
		if e = f.d.writeForward("1"); e != nil {
			return errors.New("AP_SHARING_FORWARD_ENABLE_FAILED")
		}
	}
	if e = f.d.restore(ctx, plan.restoreText()); e != nil {
		return e
	}
	after, e := f.snapshot(ctx)
	if e != nil {
		return e
	}
	if e = f.validate(after); e != nil {
		return e
	}
	if !sharingSnapshotComplete(after, proof, f.owner.Token) {
		return errors.New("AP_SHARING_READBACK_FAILED")
	}
	f.owner.Current = &proof
	f.owner.Pending = nil
	return f.d.save(f.owner)
}
func sharingHasJump(raw string, jump []string) bool {
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "-A" && fields[1] == jump[1] && sharingArguments(fields[2:]) == sharingArguments(jump[2:]) {
			return true
		}
	}
	return false
}
func sharingSnapshotComplete(s apSharingSnapshot, p apSharingProof, token string) bool {
	if s.forward != "1" || s.rule != sharingExpectedRule() || !reflect.DeepEqual(s.routes, p.Routes) {
		return false
	}
	for chain, args := range p.Filter {
		exists, rules := sharingChain(s.filter, chain)
		if !exists || !reflect.DeepEqual(rules, sharingRuleKeys(args)) {
			return false
		}
	}
	for chain, args := range p.NAT {
		exists, rules := sharingChain(s.nat, chain)
		if !exists || !reflect.DeepEqual(rules, sharingRuleKeys(args)) {
			return false
		}
	}
	for _, jump := range apSharingJumps(token) {
		raw := s.filter
		if jump[0] == "nat" {
			raw = s.nat
		}
		if !sharingHasJump(raw, jump) {
			return false
		}
	}
	return true
}

// cleanup is called after owned DHCP/hostapd have stopped. It removes only
// marker-verified resources; any unexpected rule/route makes it refuse mutation.
func (f *apSharingFirewall) cleanup(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := f.loadOwner(); e != nil {
		return e
	}
	if f.owner == nil {
		return nil
	}
	snapshot, e := f.snapshot(ctx)
	if e != nil {
		return e
	}
	if e = f.validate(snapshot); e != nil {
		return e
	}
	f.owner.Cleaning = true
	if e = f.d.save(f.owner); e != nil {
		return e
	}
	for _, jump := range apSharingJumps(f.owner.Token) {
		raw := snapshot.filter
		if jump[0] == "nat" {
			raw = snapshot.nat
		}
		if sharingHasJump(raw, jump) {
			args := append([]string{"-w", "2", "-t", jump[0], "-D", jump[1]}, jump[2:]...)
			if _, e = f.d.run(ctx, "iptables", args...); e != nil {
				return e
			}
		}
	}
	for _, entry := range []struct{ table, chain, raw string }{{"filter", apSharingForwardChain, snapshot.filter}, {"filter", apSharingDNSChain, snapshot.filter}, {"nat", apSharingNATChain, snapshot.nat}} {
		if exists, _ := sharingChain(entry.raw, entry.chain); exists {
			for _, action := range []string{"-F", "-X"} {
				if _, e = f.d.run(ctx, "iptables", "-w", "2", "-t", entry.table, action, entry.chain); e != nil {
					return e
				}
			}
		}
	}
	if snapshot.rule != "" {
		if _, e = f.d.run(ctx, "ip", "-4", "rule", "del", "priority", apSharingPriority, "from", apSharingSubnet, "iif", apInterface, "lookup", apSharingTable); e != nil {
			return e
		}
	}
	if len(snapshot.routes) > 0 {
		if _, e = f.d.run(ctx, "ip", "-4", "route", "flush", "table", apSharingTable); e != nil {
			return e
		}
	}
	if f.owner.ForwardChanged && snapshot.forward == "1" && f.owner.ForwardOriginal == "0" {
		if e = f.d.writeForward("0"); e != nil {
			return errors.New("AP_SHARING_FORWARD_RESTORE_FAILED")
		}
	}
	if e = f.d.remove(); e != nil {
		return e
	}
	f.owner = nil
	return nil
}
