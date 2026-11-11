package adapters

import (
	"context"
	"errors"
	"testing"
)

func TestStockStartupNeedsNotificationProofBeforeSuccess(t *testing.T) {
	for _, failed := range []bool{false, true} {
		f := newStockServiceFixture()
		f.state, f.pid = "inactive", 0
		calls := 0
		d := f.io()
		d.completeStart = func(_ context.Context, def stockServiceDefinition, current StockService) error {
			calls++
			if def.id != "opening" || current.State != "active" || current.MainPID != 43 {
				t.Fatal("notification preceded process startup")
			}
			if failed {
				return errors.New("no startup notification")
			}
			return nil
		}
		result, err := controlStockService(context.Background(), "opening", stockServiceRequest{"start", f.invocation, f.state}, d)
		if calls != 1 || (err != nil) != failed || (result.Status == "applied") == failed {
			t.Fatal("startup proof bypassed", calls, err, result.Status)
		}
	}
}

func TestStockStartupProofCannotTransferToAnotherInvocation(t *testing.T) {
	f := newStockServiceFixture()
	f.state, f.pid = "inactive", 0
	d := f.io()
	d.completeStart = func(context.Context, stockServiceDefinition, StockService) error {
		f.pid++
		f.invocation = "cccccccccccccccccccccccccccccccc"
		return nil
	}
	if _, err := controlStockService(context.Background(), "opening", stockServiceRequest{"start", f.invocation, f.state}, d); err == nil {
		t.Fatal("changed invocation inherited startup proof")
	}
}

func TestStockStartupStopNeverNotifies(t *testing.T) {
	f := newStockServiceFixture()
	d := f.io()
	d.completeStart = func(context.Context, stockServiceDefinition, StockService) error {
		t.Fatal("stop sent start notification")
		return nil
	}
	if _, err := controlStockService(context.Background(), "opening", stockServiceRequest{"stop", f.invocation, f.state}, d); err != nil {
		t.Fatal(err)
	}
}

func TestStockStartupOutputRequiresExactCleanupAndReadyProof(t *testing.T) {
	valid := "start-barrier=notified; owner=matched; threads=multiple; cleanup=matched\n"
	if !stockStartOutputReady(valid) {
		t.Fatal("valid startup proof rejected")
	}
	for _, value := range []string{"", "start-barrier=notified", valid + "extra", "start-barrier=notified; owner=matched; threads=single; cleanup=matched"} {
		if stockStartOutputReady(value) {
			t.Fatal("incomplete startup proof accepted")
		}
	}
	var out stockStartOutput
	if n, err := out.Write(make([]byte, 16385)); n != 16385 || err != nil || !out.overflow {
		t.Fatal("diagnostic overflow could abort shared mutex owner")
	}
}
