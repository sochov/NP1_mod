package modem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func workerSignal(mode WorkerMode) GobiNetSignal {
	v := -85.0
	out := GobiNetSignal{ObservedAt: time.Now().UTC(), Source: "gobinet-nas-independent-cid", DriverVersion: GobiNetDriverVersion, RSRP: &v, Queries: map[string]string{"signal": "available", "servingSystem": "not-requested", "rfBand": "not-requested"}}
	if mode == WithStatus {
		out.Queries["servingSystem"] = "available"
		out.Queries["rfBand"] = "available"
		out.Registered = boolValue(true)
		out.PSAttached = boolValue(true)
		out.PLMN = "00101"
		out.Carrier = "TEST"
		out.RAT = "LTE"
		out.Band = "B19"
		out.Bands = []string{"B19"}
	}
	return out
}
func TestMain(m *testing.M) {
	if kind := os.Getenv("NCC_MODEM_TEST_HELPER"); kind != "" && len(os.Args) >= 3 && os.Args[1] == "--mode" {
		switch kind {
		case "hang":
			time.Sleep(time.Hour)
		case "oversize":
			fmt.Print(strings.Repeat("x", maximumWorkerState+1))
		case "unknown":
			fmt.Print(`{"unexpected":true}`)
		default:
			mode := SignalOnly
			if os.Args[2] == "gobinet-status-probe" {
				mode = WithStatus
			}
			_ = json.NewEncoder(os.Stdout).Encode(workerSignal(mode))
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestWorkerHelperExplicitModeAndBoundedOutput(t *testing.T) {
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	for _, mode := range []WorkerMode{SignalOnly, WithStatus} {
		t.Setenv("NCC_MODEM_TEST_HELPER", "valid")
		out, e := runProbeHelper(context.Background(), executable, mode)
		if e != nil || out.Queries["servingSystem"] == "available" != (mode == WithStatus) {
			t.Fatal(out, e)
		}
	}
	for _, kind := range []string{"oversize", "unknown"} {
		t.Setenv("NCC_MODEM_TEST_HELPER", kind)
		if _, e = runProbeHelper(context.Background(), executable, SignalOnly); e == nil {
			t.Fatal("unsafe output accepted", kind)
		}
	}
	t.Setenv("NCC_MODEM_TEST_HELPER", "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, e = runProbeHelper(ctx, executable, SignalOnly); e == nil || time.Since(started) > 3*time.Second {
		t.Fatal("helper timeout failed", e)
	}
}
func TestWorkerSerialStopWaitAndStateTransitions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls, active, maximum atomic.Int32
	second := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	states := make(chan WorkerState, 10)
	probe := func(ctx context.Context) (GobiNetSignal, error) {
		current := active.Add(1)
		defer active.Add(-1)
		if current > maximum.Load() {
			maximum.Store(current)
		}
		if calls.Add(1) == 1 {
			return workerSignal(SignalOnly), nil
		}
		close(second)
		<-ctx.Done()
		<-release
		return GobiNetSignal{}, ctx.Err()
	}
	go func() {
		done <- runWorker(ctx, SignalOnly, 5*time.Millisecond, probe, func(state WorkerState) error { states <- state; return nil })
	}()
	<-second
	cancel()
	select {
	case e := <-done:
		t.Fatal("worker returned before child Wait", e)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
	close(states)
	got := []string{}
	for state := range states {
		got = append(got, state.Status)
	}
	if strings.Join(got, ",") != "unknown,available,stale,stopped" || maximum.Load() != 1 || calls.Load() != 2 {
		t.Fatal(got, maximum.Load(), calls.Load())
	}
}
func TestWorkerStartupWriteFailureNeverStartsHelper(t *testing.T) {
	called := false
	e := runWorker(context.Background(), SignalOnly, time.Second, func(context.Context) (GobiNetSignal, error) { called = true; return GobiNetSignal{}, nil }, func(WorkerState) error { return errors.New("synthetic write failure") })
	if e == nil || called {
		t.Fatal(e, called)
	}
}
func TestWorkerStateAtomicRoundTripAndStrictSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime", "state.json")
	now := time.Now().UTC()
	signal := workerSignal(WithStatus)
	state := WorkerState{Version: 1, Mode: WithStatus, Status: "available", AttemptedAt: now, UpdatedAt: now, Signal: &signal}
	if e := publishWorkerState(path, state); e != nil {
		t.Fatal(e)
	}
	out, e := ReadWorkerState(path, now)
	if runtime.GOOS == "linux" && os.Geteuid() != 0 {
		if e == nil {
			t.Fatal("unprivileged state owner trusted")
		}
		return
	}
	if e != nil || out.Signal.PLMN != "00101" {
		t.Fatal(out, e)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatal("temporary file leaked", entries)
	}
	raw, _ := json.Marshal(state)
	raw = append(raw[:len(raw)-1], []byte(`,"unexpected":true}`)...)
	os.WriteFile(path, raw, 0640)
	if _, e = ReadWorkerState(path, now); e == nil {
		t.Fatal("unknown schema field accepted")
	}
	os.WriteFile(path, []byte(strings.Repeat("x", maximumWorkerState+1)), 0640)
	if _, e = ReadWorkerState(path, now); e == nil {
		t.Fatal("oversized state accepted")
	}
}
func TestWorkerProvenanceAndModeValidation(t *testing.T) {
	now := time.Now().UTC()
	signal := workerSignal(WithStatus)
	if e := validateWorkerSignal(signal, SignalOnly, now, now); e == nil {
		t.Fatal("status leaked into signal mode")
	}
	signal = workerSignal(SignalOnly)
	signal.ObservedAt = now.Add(-time.Minute)
	if e := validateWorkerSignal(signal, SignalOnly, now, now); e == nil {
		t.Fatal("old helper data accepted")
	}
	signal = workerSignal(WithStatus)
	signal.Queries["servingSystem"] = "failed"
	if e := validateWorkerSignal(signal, WithStatus, now, now); e == nil {
		t.Fatal("failed query values accepted")
	}
	signal = workerSignal(WithStatus)
	signal.RAT = "credential/raw-output"
	if e := validateWorkerSignal(signal, WithStatus, now, now); e == nil {
		t.Fatal("arbitrary text accepted")
	}
}
