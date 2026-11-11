package modem

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type WorkerMode string

const (
	SignalOnly WorkerMode = "signal"
	WithStatus WorkerMode = "status"
)
const WorkerStatePath = "/run/np1-control-modem/state.json"
const workerExecutable = "/run/np1-control-modem/ncc"
const workerInterval = 15 * time.Second
const workerHelperTimeout = 12 * time.Second
const WorkerFreshness = 35 * time.Second
const maximumWorkerState = 16 * 1024

// WorkerState is the entire published interface. It contains no raw modem
// output, process inventory, device descriptors, credentials or arbitrary maps.
type WorkerState struct {
	Version     int            `json:"version"`
	Mode        WorkerMode     `json:"mode"`
	Status      string         `json:"status"`
	UpdatedAt   time.Time      `json:"updatedAt"`
	AttemptedAt time.Time      `json:"attemptedAt"`
	ErrorCode   string         `json:"errorCode,omitempty"`
	Signal      *GobiNetSignal `json:"signal,omitempty"`
}

// RunWorker runs serial, bounded helpers from its own fixed runtime executable.
// The caller must cancel ctx on stop. No later helper is started until Wait on
// the previous child has completed, including after cancellation or timeout.
func RunWorker(ctx context.Context, mode WorkerMode) error {
	if mode != SignalOnly && mode != WithStatus {
		return errors.New("invalid modem worker mode")
	}
	executable, e := os.Executable()
	if e != nil {
		return e
	}
	executable, e = filepath.EvalSymlinks(executable)
	if e != nil || executable != workerExecutable {
		return errors.New("modem worker requires its fixed runtime executable")
	}
	probe := func(ctx context.Context) (GobiNetSignal, error) { return runProbeHelper(ctx, executable, mode) }
	return runWorker(ctx, mode, workerInterval, probe, func(state WorkerState) error { return publishWorkerState(WorkerStatePath, state) })
}

func runWorker(ctx context.Context, mode WorkerMode, interval time.Duration, probe func(context.Context) (GobiNetSignal, error), publish func(WorkerState) error) error {
	state := WorkerState{Version: 1, Mode: mode, Status: "unknown", UpdatedAt: time.Now().UTC(), ErrorCode: "starting"}
	if e := publish(state); e != nil {
		return e
	}
	defer func() {
		state.Status = "stopped"
		state.ErrorCode = "stopped"
		state.UpdatedAt = time.Now().UTC()
		_ = publish(state)
	}()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	sample := func() error {
		if e := ctx.Err(); e != nil {
			return e
		}
		state.AttemptedAt = time.Now().UTC()
		bounded, cancel := context.WithTimeout(ctx, workerHelperTimeout)
		signal, e := probe(bounded)
		timedOut := bounded.Err() != nil
		cancel()
		finished := time.Now().UTC()
		state.UpdatedAt = finished
		if e == nil {
			e = validateWorkerSignal(signal, mode, state.AttemptedAt, finished)
		}
		if e != nil {
			state.Status = "unknown"
			state.ErrorCode = "helper-failed"
			if timedOut {
				state.ErrorCode = "helper-timeout"
			}
			if state.Signal != nil {
				state.Status = "stale"
			}
		} else {
			state.Signal = &signal
			state.Status = "available"
			state.ErrorCode = ""
		}
		return publish(state)
	}
	for {
		if e := sample(); e != nil {
			if ctx.Err() != nil {
				return nil
			}
			return e
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

type cappedOutput struct {
	bytes.Buffer
	limit int
}

func (w *cappedOutput) Write(p []byte) (int, error) {
	if len(p) > w.limit-w.Len() {
		return 0, errors.New("modem helper output exceeds limit")
	}
	return w.Buffer.Write(p)
}
func runProbeHelper(ctx context.Context, executable string, mode WorkerMode) (GobiNetSignal, error) {
	result := GobiNetSignal{}
	helperMode := "gobinet-probe"
	if mode == WithStatus {
		helperMode = "gobinet-status-probe"
	} else if mode != SignalOnly {
		return result, errors.New("invalid modem worker mode")
	}
	cmd := exec.CommandContext(ctx, executable, "--mode", helperMode)
	output := &cappedOutput{limit: maximumWorkerState}
	diagnostic := &cappedOutput{limit: 2048}
	cmd.Stdout = output
	cmd.Stderr = diagnostic
	if e := cmd.Run(); e != nil {
		if executable == workerExecutable {
			_ = os.WriteFile("/run/np1-control-modem/last-error.txt", diagnostic.Bytes(), 0600)
		}
		return result, e
	}
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(&result); e != nil {
		return GobiNetSignal{}, e
	}
	var trailing any
	if e := decoder.Decode(&trailing); e != io.EOF {
		return GobiNetSignal{}, errors.New("unexpected trailing modem output")
	}
	return result, nil
}

func validateWorkerSignal(signal GobiNetSignal, mode WorkerMode, started, finished time.Time) error {
	if (signal.Source != "gobinet-nas-independent-cid" && signal.Source != "gobinet-nas-independent-cid-stock-cm-readonly") || signal.DriverVersion != GobiNetDriverVersion || signal.ObservedAt.IsZero() || signal.ObservedAt.Before(started.Add(-2*time.Second)) || signal.ObservedAt.After(finished.Add(2*time.Second)) {
		return errors.New("invalid modem sample provenance or time")
	}
	for _, field := range []struct {
		value            *float64
		minimum, maximum float64
	}{{signal.RSSI, -125, -30}, {signal.RSRQ, -40, -1}, {signal.RSRP, -160, -30}, {signal.SNR, -30, 50}} {
		if field.value != nil && (*field.value != *field.value || *field.value < field.minimum || *field.value > field.maximum) {
			return errors.New("invalid modem signal value")
		}
	}
	if len(signal.Carrier) > 255 || safeCarrier([]byte(signal.Carrier)) != signal.Carrier || len(signal.RAT) > 64 || len(signal.Bands) > 16 {
		return errors.New("invalid modem status text")
	}
	if signal.RAT != "" {
		for _, rat := range strings.Split(signal.RAT, "/") {
			if !map[string]bool{"CDMA": true, "EVDO": true, "GSM": true, "UMTS": true, "LTE": true, "TD-SCDMA": true, "5G NR": true}[rat] {
				return errors.New("unverified RAT name")
			}
		}
	}
	if signal.PLMN != "" {
		if len(signal.PLMN) != 5 && len(signal.PLMN) != 6 {
			return errors.New("invalid PLMN width")
		}
		for _, v := range signal.PLMN {
			if v < '0' || v > '9' {
				return errors.New("invalid PLMN digits")
			}
		}
	}
	for _, band := range append(append([]string(nil), signal.Bands...), signal.Band) {
		if band == "" {
			continue
		}
		valid := false
		for _, known := range lteBands {
			if band == known {
				valid = true
				break
			}
		}
		if !valid {
			return errors.New("unverified LTE band")
		}
	}
	for name, status := range signal.Queries {
		if name != "signal" && name != "servingSystem" && name != "rfBand" {
			return errors.New("unexpected modem query")
		}
		if status != "available" && status != "failed" && status != "timeout" && status != "not-requested" {
			return errors.New("unexpected modem query status")
		}
	}
	if mode == SignalOnly && (signal.Registered != nil || signal.PSAttached != nil || signal.Carrier != "" || signal.PLMN != "" || signal.Band != "" || len(signal.Bands) > 0 || signal.RAT != "" || signal.Queries["servingSystem"] != "not-requested" || signal.Queries["rfBand"] != "not-requested") {
		return errors.New("status query data reached signal-only worker")
	}
	if len(signal.Queries) != 3 {
		return errors.New("missing modem query outcomes")
	}
	available := false
	for _, status := range signal.Queries {
		if status == "available" {
			available = true
		}
		if mode == WithStatus && status == "not-requested" {
			return errors.New("missing opt-in query outcome")
		}
	}
	if !available {
		return errors.New("no successful modem query")
	}
	if signal.Queries["signal"] != "available" && (signal.RSSI != nil || signal.RSRQ != nil || signal.RSRP != nil || signal.SNR != nil) {
		return errors.New("unverified signal values")
	}
	if signal.Queries["servingSystem"] != "available" && (signal.Registered != nil || signal.PSAttached != nil || signal.Carrier != "" || signal.PLMN != "" || signal.RAT != "") {
		return errors.New("unverified registration values")
	}
	if signal.Queries["rfBand"] != "available" && (signal.Band != "" || len(signal.Bands) > 0) {
		return errors.New("unverified band values")
	}
	return nil
}

func publishWorkerState(path string, state WorkerState) error {
	raw, e := json.Marshal(state)
	if e != nil {
		return e
	}
	if len(raw) > maximumWorkerState {
		return errors.New("modem worker state exceeds limit")
	}
	dir := filepath.Dir(path)
	if e = os.MkdirAll(dir, 0750); e != nil {
		return e
	}
	file, e := os.CreateTemp(dir, ".modem-state-*")
	if e != nil {
		return e
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if e = file.Chmod(0640); e == nil {
		_, e = file.Write(append(raw, '\n'))
	}
	if e == nil {
		e = file.Sync()
	}
	closed := file.Close()
	if e != nil {
		return e
	}
	if closed != nil {
		return closed
	}
	if e = os.Rename(temporary, path); e != nil {
		return e
	}
	directory, e := os.Open(dir)
	if e == nil {
		_ = directory.Sync()
		directory.Close()
	}
	return nil
}

// ReadWorkerState validates the fixed schema and provenance. Callers decide
// available/stale from both sample time and worker update time; UpdatedAt alone
// cannot make old values appear fresh after repeated helper failures.
func ReadWorkerState(path string, now time.Time) (WorkerState, error) {
	state := WorkerState{}
	st, e := os.Lstat(path)
	if e != nil {
		return state, e
	}
	if !st.Mode().IsRegular() || st.Size() > maximumWorkerState || !trustedWorkerFile(st) {
		return state, errors.New("untrusted modem state file")
	}
	file, e := os.Open(path)
	if e != nil {
		return state, e
	}
	defer file.Close()
	opened, e := file.Stat()
	if e != nil || !os.SameFile(st, opened) {
		return state, errors.New("modem state changed while opening")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maximumWorkerState+1))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&state); e != nil {
		return WorkerState{}, e
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return WorkerState{}, errors.New("unexpected trailing modem state")
	}
	if state.Version != 1 || state.Mode != SignalOnly && state.Mode != WithStatus || state.UpdatedAt.IsZero() || state.UpdatedAt.After(now.Add(5*time.Second)) || state.AttemptedAt.After(state.UpdatedAt) {
		return WorkerState{}, errors.New("invalid modem worker metadata")
	}
	if state.Status != "available" && state.Status != "unknown" && state.Status != "stale" && state.Status != "stopped" {
		return WorkerState{}, errors.New("invalid modem worker status")
	}
	if !map[string]bool{"": true, "starting": true, "helper-failed": true, "helper-timeout": true, "stopped": true}[state.ErrorCode] {
		return WorkerState{}, errors.New("invalid modem worker error code")
	}
	if state.Status == "available" && (state.ErrorCode != "" || state.AttemptedAt.IsZero()) {
		return WorkerState{}, errors.New("contradictory modem worker success")
	}
	if state.Status == "unknown" && state.Signal != nil {
		return WorkerState{}, errors.New("contradictory modem worker values")
	}
	if state.Signal != nil {
		if e = validateWorkerSignal(*state.Signal, state.Mode, state.Signal.ObservedAt, state.Signal.ObservedAt); e != nil {
			return WorkerState{}, e
		}
		if state.Signal.ObservedAt.After(state.UpdatedAt.Add(2 * time.Second)) {
			return WorkerState{}, errors.New("modem sample newer than worker")
		}
	} else if state.Status == "available" || state.Status == "stale" {
		return WorkerState{}, errors.New("modem values missing")
	}
	return state, nil
}
