package modem

import (
	"context"
	"errors"
	"testing"
)

func TestStockCMRoutingNeverFallsBackAfterUnknownOrFailedProof(t *testing.T) {
	want := errors.New("STOCK_CM_GET_PROVENANCE_CHANGED")
	for _, tc := range []struct {
		present                             bool
		detectError, cmError, errorExpected error
		wantCM, wantExclusive               int
	}{
		{false, nil, nil, nil, 0, 1}, {true, nil, nil, nil, 1, 0}, {true, nil, want, want, 1, 0}, {false, want, nil, want, 0, 0}, {true, want, nil, want, 0, 0},
	} {
		cmCalls, exclusiveCalls := 0, 0
		_, err := readByStockCMOwnership(context.Background(), func(context.Context) (bool, error) { return tc.present, tc.detectError }, func(context.Context) (int, error) { cmCalls++; return 1, tc.cmError }, func(context.Context) (int, error) { exclusiveCalls++; return 2, nil })
		if !errors.Is(err, tc.errorExpected) || cmCalls != tc.wantCM || exclusiveCalls != tc.wantExclusive {
			t.Fatal("unsafe fallback or wrong route", err, cmCalls, exclusiveCalls)
		}
	}
}
