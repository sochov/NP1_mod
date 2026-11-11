package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"regexp"
	"time"

	"github.com/np1-control-center/ncc/internal/modem"
)

func stockCMProbeError(err error) error {
	if err == nil {
		return nil
	}
	if regexp.MustCompile(`^STOCK_CM_[A-Z0-9_]{1,128}$`).MatchString(err.Error()) {
		return err
	}
	return errors.New("STOCK_CM_GET_FAILED")
}

func runStockCMNASReadOnly() error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	value, err := modem.ReadStockCMNAS(ctx)
	if err != nil {
		return stockCMProbeError(err)
	}
	if err = json.NewEncoder(os.Stdout).Encode(struct {
		MetadataVerified bool                `json:"metadataVerified"`
		NAS              modem.GobiNetSignal `json:"nas"`
	}{true, value}); err != nil {
		return err
	}
	for _, name := range []string{"signal", "servingSystem", "rfBand"} {
		if value.Queries[name] != "available" {
			return errors.New("STOCK_CM_NAS_READBACK_PARTIAL")
		}
	}
	return nil
}

func runStockCMProfilesReadOnly() error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	capture, inventory, err := modem.ReadStockCMProfiles(ctx)
	if err != nil {
		return stockCMProbeError(err)
	}
	summary, err := capture.Save()
	if err != nil {
		return errors.New("STOCK_CM_PROFILE_PRIVATE_SAVE_FAILED")
	}
	readback, err := modem.LoadProfileRecovery(summary.BackupFile)
	if err != nil || !reflect.DeepEqual(capture.Summary(), readback.Summary()) {
		return errors.New("STOCK_CM_PROFILE_PRIVATE_READBACK_FAILED")
	}
	summary.BackupFile = ""
	if err = json.NewEncoder(os.Stdout).Encode(struct {
		MetadataVerified    bool                         `json:"metadataVerified"`
		PrivateSaveVerified bool                         `json:"privateSaveVerified"`
		Profiles            modem.ProfileRecoverySummary `json:"profiles"`
		Inventory           modem.ProfileInventory       `json:"inventory"`
	}{true, true, summary, inventory}); err != nil {
		return err
	}
	for _, profile := range summary.Profiles {
		if profile.Status != "available" && !profile.AbsenceConfirmed {
			return errors.New("STOCK_CM_PROFILE_READBACK_PARTIAL")
		}
	}
	return nil
}
