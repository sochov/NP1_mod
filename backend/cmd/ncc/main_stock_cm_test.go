package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/np1-control-center/ncc/internal/modem"
)

func TestStockCMReadOnlyModeEmitsTypedSummaryAndSanitizesErrors(t *testing.T) {
	for _, message := range []string{"STOCK_CM_QMI_FD_NOT_OBSERVED", "private executable/cmdline credential"} {
		var out bytes.Buffer
		err := writeStockCMOwnershipReadOnly(&out, modem.StockCMOwnershipProof{Status: "unknown", OwnerVerified: true}, errors.New(message))
		var proof map[string]any
		if err == nil || json.Unmarshal(out.Bytes(), &proof) != nil || proof["status"] != "unknown" || proof["ownerVerified"] != true {
			t.Fatal("typed diagnostic missing")
		}
		if strings.Contains(out.String(), "credential") || strings.Contains(err.Error(), "credential") {
			t.Fatal("private process error escaped")
		}
	}
	var out bytes.Buffer
	if err := writeStockCMOwnershipReadOnly(&out, modem.StockCMOwnershipProof{Status: "no-cm", OwnerVerified: true}, nil); err != nil || strings.Contains(out.String(), `"code"`) {
		t.Fatal("absence changed into a failed or verified CM proof")
	}
}
