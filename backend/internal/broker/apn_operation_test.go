package broker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/adapters"
	"github.com/np1-control-center/ncc/internal/modem"
)

func successfulAPNChild() apnChildResult {
	yes, revision := true, 8
	return apnChildResult{Version: 1, Revision: revision, AppliedRevision: &revision, Result: modem.NativeAPNApplyResult{Status: "applied", SavedFieldsMatched: &yes, Profile2StillAbsent: &yes, ManagementSettled: true}}
}

func TestAPNChildRequiresMatchingRevisionAndMeasuredConfiguration(t *testing.T) {
	base := successfulAPNChild()
	raw, _ := json.Marshal(base)
	if _, e := parseAPNChildResult(raw, 8); e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		name   string
		change func(*apnChildResult)
	}{
		{"wrong revision", func(v *apnChildResult) { v.Revision = 9 }},
		{"wrong applied revision", func(v *apnChildResult) { x := 9; v.AppliedRevision = &x }},
		{"saved unknown", func(v *apnChildResult) { v.Result.SavedFieldsMatched = nil }},
		{"p2 unknown", func(v *apnChildResult) { v.Result.Profile2StillAbsent = nil }},
		{"unsettled management", func(v *apnChildResult) { v.Result.ManagementSettled = false }},
		{"recovery required", func(v *apnChildResult) { v.Result.RecoveryRequired = true }},
		{"mere acceptance", func(v *apnChildResult) { v.Result.Status = "accepted" }},
		{"contradictory error", func(v *apnChildResult) { v.Code = "APN_APPLY_FAILED" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := successfulAPNChild()
			tc.change(&v)
			raw, _ := json.Marshal(v)
			if _, e := parseAPNChildResult(raw, 8); e == nil {
				t.Fatal("unverified child accepted")
			}
		})
	}
	for _, raw := range []string{string(raw) + " {}", `{"version":1,"revision":8,"credential":"private-value"}`, strings.Repeat("x", apnHelperOutputLimit+1)} {
		if _, e := parseAPNChildResult([]byte(raw), 8); e == nil || strings.Contains(e.Error(), "private-value") {
			t.Fatal("unbounded/invalid result or leaked detail")
		}
	}
	failed := apnChildResult{Version: 1, Revision: 8, Code: "REVISION_CONFLICT"}
	raw, _ = json.Marshal(failed)
	if _, e := parseAPNChildResult(raw, 8); e == nil || e.Error() != "REVISION_CONFLICT" {
		t.Fatal(e)
	}
}

func TestAPNJobNilErrorDoesNotInventVerifiedSuccess(t *testing.T) {
	j := newAPNJobs(func(context.Context, int) (adapters.APNSettingsView, modem.NativeAPNApplyResult, error) {
		return adapters.APNSettingsView{}, modem.NativeAPNApplyResult{Status: "applied"}, nil
	}, func(apnApplyJob) error { return nil })
	defer j.close()
	job, e := j.start(1)
	if e != nil {
		t.Fatal(e)
	}
	j.wg.Wait()
	final, _ := j.get(job.ID)
	if final.Status != "failed" || final.Verified || final.Code != "APN_RESULT_UNVERIFIED" {
		t.Fatal(final)
	}
}

func TestAPNObserverResumeWaitsForNewMeasurementAndKeepsMarkerOnFailure(t *testing.T) {
	for _, scenario := range []string{"success", "stop failure", "start failure", "signal failure", "cancelled operation"} {
		t.Run(scenario, func(t *testing.T) {
			var order []string
			removed, called := false, false
			now := time.Now().UTC()
			observer := apnObserver{
				active: func(context.Context) (bool, error) { return true, nil },
				mark:   func(time.Time) error { order = append(order, "mark"); return nil },
				remove: func() error { removed = true; order = append(order, "remove"); return nil },
				now:    func() time.Time { return now },
				service: func(ctx context.Context, action string) error {
					order = append(order, action)
					if ctx.Err() != nil {
						t.Error("cleanup inherited cancelled context")
					}
					if scenario == action+" failure" {
						return errors.New("service")
					}
					return nil
				},
				fresh: func(ctx context.Context, since time.Time) error {
					order = append(order, "measured")
					if !since.Equal(now) {
						t.Error("incorrect freshness anchor")
					}
					if ctx.Err() != nil {
						t.Error("cancelled freshness check")
					}
					if scenario == "signal failure" {
						return errors.New("no signal")
					}
					return nil
				},
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			e := withAPNObserverPaused(ctx, observer, func(context.Context) error {
				called = true
				order = append(order, "native")
				if scenario == "cancelled operation" {
					cancel()
					return context.Canceled
				}
				return nil
			})
			if scenario == "success" {
				if e != nil || !removed || !reflect.DeepEqual(order, []string{"mark", "stop", "native", "start", "measured", "remove"}) {
					t.Fatalf("%v %v", order, e)
				}
			} else if e == nil {
				t.Fatal("failed operation accepted")
			}
			if scenario == "stop failure" && called {
				t.Fatal("native called despite failed pause")
			}
			if (scenario == "start failure" || scenario == "signal failure") && removed {
				t.Fatal("marker cleared before actual observer recovery")
			}
			if scenario == "stop failure" && !removed {
				t.Fatal("observer was not restored after failed stop")
			}
		})
	}
}

func TestAPNObserverDoesNotTreatActiveOrOldSampleAsRecovered(t *testing.T) {
	now := time.Now().UTC()
	value := -80.0
	state := modem.WorkerState{Status: "available", Signal: &modem.GobiNetSignal{ObservedAt: now, RSRP: &value, Queries: map[string]string{"signal": "available"}}}
	if !measuredAPNObserverSince(state, now.Add(-time.Second), now) {
		t.Fatal("new measurement rejected")
	}
	if measuredAPNObserverSince(state, now.Add(time.Millisecond), now) {
		t.Fatal("pre-start measurement accepted")
	}
	state.Status = "stale"
	if measuredAPNObserverSince(state, now.Add(-time.Second), now) {
		t.Fatal("stale accepted")
	}
	state.Status = "available"
	state.Signal.Queries["signal"] = "failed"
	if measuredAPNObserverSince(state, now.Add(-time.Second), now) {
		t.Fatal("status-only accepted")
	}
	state.Signal.Queries["signal"] = "available"
	state.Signal.ObservedAt = now.Add(-11 * time.Second)
	if measuredAPNObserverSince(state, now.Add(-20*time.Second), now) {
		t.Fatal("old signal accepted")
	}
}

func TestAPNProcessHelper(t *testing.T) {
	if os.Getenv("NCC_APN_PROCESS_TEST") != "1" {
		return
	}
	time.Sleep(time.Minute)
	os.Exit(0)
}

func TestAPNProcessTimeoutAndCancellationReapChildBeforeReturning(t *testing.T) {
	for _, cancelParent := range []bool{false, true} {
		t.Run(map[bool]string{false: "timeout", true: "shutdown"}[cancelParent], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var child *exec.Cmd
			start := time.Now()
			_, timed, e := executeAPNProcess(ctx, 200*time.Millisecond, func(bounded context.Context) *exec.Cmd {
				child = exec.CommandContext(bounded, os.Args[0], "-test.run=^TestAPNProcessHelper$")
				child.Env = append(os.Environ(), "NCC_APN_PROCESS_TEST=1")
				if cancelParent {
					time.AfterFunc(100*time.Millisecond, cancel)
				}
				return child
			})
			if e == nil || !timed || time.Since(start) > 5*time.Second || child.ProcessState == nil {
				t.Fatalf("child not bounded/reaped: %v", e)
			}
			// Windows marks a waited handle released (EINVAL), while Unix
			// reports ErrProcessDone. Both follow a completed cmd.Run/Wait.
			if child.Process.Kill() == nil {
				t.Fatal("child still live after operation returned")
			}
		})
	}
}
