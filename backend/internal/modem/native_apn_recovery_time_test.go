package modem

import (
	"testing"
	"time"
)

func TestNativeAPNRestartRecoveryCannotUseAnotherJobsOldSnapshot(t *testing.T) {
	now := time.Now().UTC()
	started := now.Add(-time.Minute)
	if e := validateNativeAPNRecoveryTime(started.Add(time.Second), started, now); e != nil {
		t.Fatal(e)
	}
	if e := validateNativeAPNRecoveryTime(started.Add(-time.Nanosecond), started, now); e == nil || e.Error() != "APN_NETWORK_BACKUP_WRONG_JOB" {
		t.Fatal(e)
	}
	for _, observed := range []time.Time{{}, now.Add(-2 * time.Hour), now.Add(2 * time.Minute)} {
		if e := validateNativeAPNRecoveryTime(observed, time.Time{}, now); e == nil {
			t.Fatal("stale/future backup accepted")
		}
	}
	if e := validateNativeAPNRecoveryTime(now.Add(-time.Minute), time.Time{}, now); e != nil {
		t.Fatal("explicit conditional recovery rejected", e)
	}
}
