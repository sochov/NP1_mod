package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/np1-control-center/ncc/internal/adapters"
	"github.com/np1-control-center/ncc/internal/modem"
)

// runGobiNetProfilesProbe is an explicit one-shot read and private backup. It
// emits only typed counts/status, never raw TLVs, APN or credentials.
func runGobiNetProfilesProbe() error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	capture, e := modem.ReadGobiNetProfiles(ctx)
	if e != nil {
		return e
	}
	summary, e := capture.Save()
	if e != nil {
		return e
	}
	readback, e := modem.LoadProfileRecovery(summary.BackupFile)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(capture.Summary(), readback.Summary()) {
		return errors.New("saved profile recovery read-back mismatch")
	}
	summary.BackupFile = ""
	if e = json.NewEncoder(os.Stdout).Encode(struct {
		modem.ProfileRecoverySummary
		ReadBackVerified bool `json:"readBackVerified"`
	}{summary, true}); e != nil {
		return e
	}
	for _, profile := range summary.Profiles {
		if profile.Status != "available" && !profile.AbsenceConfirmed {
			return errors.New("saved profile read is partial")
		}
	}
	return nil
}

// This checks our own validated state file only; it issues no modem query.
func runProfilesObserverCheck() error {
	until := time.Now().Add(25 * time.Second)
	for time.Now().Before(until) {
		state, e := modem.ReadWorkerState(modem.WorkerStatePath, time.Now().UTC())
		if e == nil && state.Status == "available" && state.Signal != nil && state.Signal.Queries["signal"] == "available" && (state.Signal.RSRP != nil || state.Signal.RSSI != nil) && time.Since(state.Signal.ObservedAt) < 10*time.Second {
			fmt.Fprintln(os.Stdout, "Measured modem observer recovered")
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("modem observer did not recover a fresh measured signal")
}
func runGobiNetProfileInventoryProbe() error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	inventory, e := modem.ReadGobiNetProfileInventory(ctx)
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(inventory)
}
func runAPNEditorSeed() error {
	seeded, e := adapters.SeedAPNEditorFromRecovery()
	if e != nil {
		return e
	}
	view, e := adapters.APNSettings()
	if e != nil {
		return e
	}
	if len(view.Profiles) != 2 {
		return errors.New("APN editor profile count invalid")
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		Seeded             bool `json:"seeded"`
		Profile1Configured bool `json:"profile1Configured"`
		Profile2Enabled    bool `json:"profile2Enabled"`
		UsernameConfigured bool `json:"usernameConfigured"`
		PasswordConfigured bool `json:"passwordConfigured"`
		Current            bool `json:"current"`
	}{seeded, view.Profiles[0].APN != "", view.Profiles[1].Enabled, view.Profiles[0].UsernameConfigured, view.Profiles[0].PasswordConfigured, false})
}
