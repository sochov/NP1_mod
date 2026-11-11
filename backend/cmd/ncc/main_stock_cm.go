package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"time"

	"github.com/np1-control-center/ncc/internal/modem"
)

// This mode reads process metadata only. It never opens an AT/QMI FD and does
// not authorize a background observer to share the stock owner's device.
func runStockCMOwnershipReadOnly() error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	proof, err := modem.InspectStockCMOwnership(ctx)
	return writeStockCMOwnershipReadOnly(os.Stdout, proof, err)
}

func writeStockCMOwnershipReadOnly(out io.Writer, proof modem.StockCMOwnershipProof, inspectionError error) error {
	code := ""
	if inspectionError != nil {
		code = inspectionError.Error()
		if !regexp.MustCompile(`^STOCK_CM_[A-Z0-9_]{1,128}$`).MatchString(code) {
			code = "STOCK_CM_INSPECTION_FAILED"
		}
	}
	if err := json.NewEncoder(out).Encode(struct {
		modem.StockCMOwnershipProof
		Code string `json:"code,omitempty"`
	}{proof, code}); err != nil {
		return err
	}
	if inspectionError != nil {
		return errors.New(code)
	}
	return nil
}
