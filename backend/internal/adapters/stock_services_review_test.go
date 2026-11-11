package adapters

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

const reviewOldInvocation = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const reviewNewInvocation = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func reviewServiceShow(unit, state, invocation, pid string) string {
	return fmt.Sprintf("Id=%s\nLoadState=loaded\nActiveState=%s\nSubState=running\nMainPID=%s\nInvocationID=%s\nUnitFileState=static\nCanStart=yes\nCanStop=yes\nRefuseManualStart=no\nRefuseManualStop=no\nJob=\n", unit, state, pid, invocation)
}

type serviceReviewClock struct {
	now, started time.Time
	issued       bool
	commands     int
	phase        string
	phases       []string
	after        func(time.Duration, string) (string, error)
}

func (f *serviceReviewClock) io(initial string) stockServiceIO {
	return stockServiceIO{now: func() time.Time { return f.now }, sleep: func(ctx context.Context, delay time.Duration) error { f.now = f.now.Add(delay); return ctx.Err() }, run: func(_ context.Context, args ...string) (string, error) {
		if args[0] == "show" {
			unit := args[len(args)-1]
			if !f.issued {
				pid := "41"
				if initial == "inactive" {
					pid = "0"
				}
				return reviewServiceShow(unit, initial, reviewOldInvocation, pid), nil
			}
			if f.phase == "stop" {
				return reviewServiceShow(unit, "inactive", reviewOldInvocation, "0"), nil
			}
			return f.after(f.now.Sub(f.started), unit)
		}
		action := ""
		for _, arg := range args {
			if arg == "start" || arg == "stop" || arg == "restart" {
				action = arg
				break
			}
		}
		if action == "" {
			return "", nil
		}
		f.issued = true
		f.started = f.now
		f.commands++
		f.phase = action
		f.phases = append(f.phases, action)
		return "", nil
	}}
}
func TestStockServiceReviewEarlyExitNeverConfirmsStartOrRestart(t *testing.T) {
	for _, action := range []string{"start", "restart"} {
		t.Run(action, func(t *testing.T) {
			initial := "inactive"
			if action == "restart" {
				initial = "active"
			}
			f := serviceReviewClock{now: time.Now(), after: func(elapsed time.Duration, unit string) (string, error) {
				if elapsed >= 1500*time.Millisecond {
					return reviewServiceShow(unit, "failed", reviewNewInvocation, "0"), nil
				}
				return reviewServiceShow(unit, "active", reviewNewInvocation, "100"), nil
			}}
			result, e := controlStockService(context.Background(), "opening", stockServiceRequest{action, reviewOldInvocation, initial}, f.io(initial))
			if e == nil || result.Status == "applied" {
				t.Fatal("early process exit reported success")
			}
			p, ok := e.(*stockServiceProblem)
			if !ok || p.status != 503 {
				t.Fatal("early exit did not remain a read-back failure", e)
			}
			expectedCommands := 1
			if action == "restart" {
				expectedCommands = 2
			}
			if f.commands != expectedCommands {
				t.Fatal("early exit caused another command or forced fallback")
			}
		})
	}
}
func TestStockServiceReviewMinimumStableWindow(t *testing.T) {
	for _, action := range []string{"start", "stop", "restart"} {
		t.Run(action, func(t *testing.T) {
			initial := "active"
			if action == "start" {
				initial = "inactive"
			}
			f := serviceReviewClock{now: time.Now(), after: func(_ time.Duration, unit string) (string, error) {
				if action == "stop" {
					return reviewServiceShow(unit, "inactive", reviewOldInvocation, "0"), nil
				}
				return reviewServiceShow(unit, "active", reviewNewInvocation, "100"), nil
			}}
			result, e := controlStockService(context.Background(), "opening", stockServiceRequest{action, reviewOldInvocation, initial}, f.io(initial))
			if e != nil || result.Status != "applied" {
				t.Fatal(e)
			}
			minimum := 3 * time.Second
			if action == "stop" {
				minimum = time.Second
			}
			if f.now.Sub(f.started) < minimum {
				t.Fatalf("confirmed after %v, minimum %v", f.now.Sub(f.started), minimum)
			}
			if action == "restart" && strings.Join(f.phases, ",") != "stop,start" {
				t.Fatalf("restart must stop/wait before start: %v", f.phases)
			}
		})
	}
}
func TestStockServiceReviewUnreadableIntervalResetsStability(t *testing.T) {
	f := serviceReviewClock{now: time.Now(), after: func(elapsed time.Duration, unit string) (string, error) {
		if elapsed == time.Second {
			return "", errors.New("show unavailable")
		}
		return reviewServiceShow(unit, "active", reviewNewInvocation, "100"), nil
	}}
	result, e := controlStockService(context.Background(), "opening", stockServiceRequest{"start", reviewOldInvocation, "inactive"}, f.io("inactive"))
	if e != nil || result.Status != "applied" {
		t.Fatal(e)
	}
	if f.now.Sub(f.started) < 4250*time.Millisecond {
		t.Fatal("unobserved interval counted as continuous stable runtime", f.now.Sub(f.started))
	}
}
func TestStockServiceReviewPartialMetadataIsNotControllable(t *testing.T) {
	for _, mode := range []string{"missing-pid", "malformed-pid", "overflow-pid", "missing-invocation", "malformed-invocation"} {
		t.Run(mode, func(t *testing.T) {
			raw := reviewServiceShow("app-@Opening.service", "active", reviewOldInvocation, "41")
			switch mode {
			case "missing-pid":
				raw = strings.ReplaceAll(raw, "MainPID=41\n", "")
			case "malformed-pid":
				raw = strings.ReplaceAll(raw, "MainPID=41", "MainPID=not-a-number")
			case "overflow-pid":
				raw = strings.ReplaceAll(raw, "MainPID=41", "MainPID=18446744073709551616")
			case "missing-invocation":
				raw = strings.ReplaceAll(raw, "InvocationID="+reviewOldInvocation, "InvocationID=")
			case "malformed-invocation":
				raw = strings.ReplaceAll(raw, reviewOldInvocation, "not-a-valid-invocation")
			}
			d := stockServiceIO{run: func(context.Context, ...string) (string, error) { return raw, nil }}
			def, _ := stockServiceDefinitionFor("opening")
			list, e := readStockServices(context.Background(), []stockServiceDefinition{def}, d)
			if e != nil {
				return
			}
			if len(list) != 1 || list[0].CanStart || list[0].CanStop || list[0].CanRestart {
				t.Fatal("partial metadata enabled controls")
			}
		})
	}
}
func TestStockServiceReviewNeverStartedInactiveCanStart(t *testing.T) {
	raw := reviewServiceShow("app-@Opening.service", "inactive", "", "0")
	d := stockServiceIO{run: func(context.Context, ...string) (string, error) { return raw, nil }}
	def, _ := stockServiceDefinitionFor("opening")
	list, e := readStockServices(context.Background(), []stockServiceDefinition{def}, d)
	if e != nil || len(list) != 1 || !list[0].CanStart {
		t.Fatal("valid never-started unit was rejected", e)
	}
}
