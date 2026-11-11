package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// A small kernel model retains unrelated stock rules, routes and policies. Tests
// exercise effects and read-back, rather than accepting command success alone.
type sharingKernel struct {
	filter, nat   map[string][][]string
	routes        [][]string
	rule, forward string
	saved         *apSharingOwner
	writes        int
	failRestore   bool
}

func newSharingKernel() *sharingKernel {
	return &sharingKernel{filter: map[string][][]string{"FORWARD": {{"-j", "STOCK"}}, "INPUT": {{"-j", "STOCK"}}, "STOCK": {{"-j", "RETURN"}}}, nat: map[string][][]string{"POSTROUTING": {{"-j", "STOCK_NAT"}}, "STOCK_NAT": {{"-j", "RETURN"}}}, forward: "0"}
}
func (k *sharingKernel) deps() apSharingEffects {
	return apSharingEffects{
		bootID: func() string { return "test-boot" }, readForward: func() (string, error) { return k.forward, nil }, writeForward: func(v string) error { k.forward = v; k.writes++; return nil },
		load: func() (*apSharingOwner, error) { return k.saved, nil }, save: func(v *apSharingOwner) error { b, _ := json.Marshal(v); return json.Unmarshal(b, &k.saved) }, remove: func() error { k.saved = nil; return nil },
		restore: func(_ context.Context, raw string) error {
			if k.failRestore {
				return errors.New("fixture failure")
			}
			var table map[string][][]string
			for _, line := range strings.Split(raw, "\n") {
				f := strings.Fields(line)
				if len(f) == 0 {
					continue
				}
				switch {
				case f[0] == "*filter":
					table = k.filter
				case f[0] == "*nat":
					table = k.nat
				case strings.HasPrefix(f[0], ":"):
					name := strings.TrimPrefix(f[0], ":")
					if _, ok := table[name]; !ok {
						table[name] = [][]string{}
					}
				case f[0] == "-F":
					table[f[1]] = [][]string{}
				case f[0] == "-A":
					table[f[1]] = append(table[f[1]], f[2:])
				}
			}
			k.writes++
			return nil
		},
		run: func(_ context.Context, name string, args ...string) (string, error) {
			if name == "iptables" {
				table := k.filter
				if args[3] == "nat" {
					table = k.nat
				}
				action := args[4]
				if action == "-S" {
					var out []string
					for chain, rules := range table {
						out = append(out, "-N "+chain)
						for _, r := range rules {
							out = append(out, "-A "+chain+" "+strings.Join(r, " "))
						}
					}
					return strings.Join(out, "\n"), nil
				}
				chain := args[5]
				k.writes++
				switch action {
				case "-I":
					table[chain] = append([][]string{args[7:]}, table[chain]...)
				case "-D":
					wanted := sharingArguments(args[6:])
					for i, r := range table[chain] {
						if sharingArguments(r) == wanted {
							table[chain] = append(table[chain][:i], table[chain][i+1:]...)
							return "", nil
						}
					}
					return "", errors.New("missing rule")
				case "-F":
					table[chain] = [][]string{}
				case "-X":
					delete(table, chain)
				}
				return "", nil
			}
			if name == "ip" && args[1] == "rule" {
				switch args[2] {
				case "show":
					return k.rule, nil
				case "add":
					k.rule = sharingExpectedRule()
				case "del":
					k.rule = ""
				}
				k.writes++
				return "", nil
			}
			if name == "ip" && args[1] == "route" {
				switch args[2] {
				case "show":
					out := []string{"default via 10.7.0.1 dev stock0 proto dhcp"}
					for _, r := range k.routes {
						out = append(out, strings.Join(r, " ")+" table "+apSharingTable)
					}
					return strings.Join(out, "\n"), nil
				case "flush":
					k.routes = nil
				case "add":
					k.routes = append(k.routes, append([]string{}, args[5:]...))
				}
				k.writes++
				return "", nil
			}
			return "", errors.New("unexpected command")
		},
	}
}
func sharingFixtureUpstream(t *testing.T) APSharingUpstream {
	t.Helper()
	u, e := newAPSharingUpstream("wifi", "wlan0", "10.7.0.2", 24, "10.7.0.1", []string{"9.9.9.9"}, "fixture", time.Now())
	if e != nil {
		t.Fatal(e)
	}
	return u
}
func TestAPSharingFirewallLifecycle(t *testing.T) {
	k := newSharingKernel()
	f := &apSharingFirewall{d: k.deps()}
	u := sharingFixtureUpstream(t)
	ctx := context.Background()
	if e := f.apply(ctx, &u); e != nil {
		t.Fatal(e)
	}
	if k.forward != "1" || k.saved.Current == nil {
		t.Fatal("missing acknowledged installation")
	}
	before := k.writes
	if e := f.apply(ctx, &u); e != nil {
		t.Fatal(e)
	}
	if k.writes != before {
		t.Fatal("unchanged polling rewrote kernel state")
	}
	if e := f.apply(ctx, nil); e != nil {
		t.Fatal(e)
	}
	if len(k.filter[apSharingForwardChain]) != 2 || k.filter[apSharingForwardChain][0][1] != "DROP" || len(k.routes) != 1 {
		t.Fatal("WAN loss must retain AP guard and blackhole policy")
	}
	if e := f.apply(ctx, &u); e != nil {
		t.Fatal(e)
	}
	if e := f.cleanup(ctx); e != nil {
		t.Fatal(e)
	}
	want := newSharingKernel()
	if !reflect.DeepEqual(k.filter, want.filter) || !reflect.DeepEqual(k.nat, want.nat) || k.rule != "" || len(k.routes) != 0 || k.forward != "0" || k.saved != nil {
		t.Fatal("cleanup changed stock state or leaked owned state")
	}
}
func TestAPSharingFirewallCollisionDoesNotMutate(t *testing.T) {
	k := newSharingKernel()
	k.filter[apSharingForwardChain] = [][]string{{"-j", "ACCEPT"}}
	f := &apSharingFirewall{d: k.deps()}
	if e := f.apply(context.Background(), nil); e == nil {
		t.Fatal("accepted unknown existing chain")
	}
	if k.writes != 0 {
		t.Fatal("mutated colliding resources")
	}
}
func TestAPSharingFirewallOwnershipChangeRefusesCleanup(t *testing.T) {
	k := newSharingKernel()
	f := &apSharingFirewall{d: k.deps()}
	if e := f.apply(context.Background(), nil); e != nil {
		t.Fatal(e)
	}
	k.filter[apSharingForwardChain] = append(k.filter[apSharingForwardChain], []string{"-j", "ACCEPT"})
	before := k.writes
	if e := f.cleanup(context.Background()); e == nil {
		t.Fatal("removed externally changed chain")
	}
	if k.writes != before {
		t.Fatal("mutated after ownership mismatch")
	}
}
func TestAPSharingFirewallRecoveryAndOriginalForwarding(t *testing.T) {
	k := newSharingKernel()
	k.forward = "1"
	f := &apSharingFirewall{d: k.deps()}
	u := sharingFixtureUpstream(t)
	if e := f.apply(context.Background(), &u); e != nil {
		t.Fatal(e)
	}
	recovered := &apSharingFirewall{d: k.deps()}
	if e := recovered.cleanup(context.Background()); e != nil {
		t.Fatal(e)
	}
	if k.forward != "1" {
		t.Fatal("restored unrelated forwarding to zero")
	}
}

func TestAPSharingIPTablesCanonicalMatchOrder(t *testing.T) {
	expected := []string{"-i", "nccap0", "-s", "192.168.226.0/24", "-d", "192.168.226.1", "-p", "udp", "--dport", "53", "-j", "ACCEPT"}
	actual := []string{"-s", "192.168.226.0/24", "-d", "192.168.226.1/32", "-i", "nccap0", "-p", "udp", "-m", "udp", "--dport", "53", "-j", "ACCEPT"}
	if sharingArguments(expected) != sharingArguments(actual) {
		t.Fatal("iptables canonical option order caused false ownership mismatch")
	}
	actual = append(actual, "-m", "comment")
	if sharingArguments(expected) == sharingArguments(actual) {
		t.Fatal("additional match was silently ignored")
	}
}
