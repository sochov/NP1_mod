package broker

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/adapters"
	"github.com/np1-control-center/ncc/internal/modem"
)

func TestAPNJobIsAsyncExclusiveAndRevisionBound(t *testing.T) {
	started := make(chan int, 1)
	release := make(chan struct{})
	var mu sync.Mutex
	var saved []apnApplyJob
	j := newAPNJobs(func(ctx context.Context, revision int) (adapters.APNSettingsView, modem.NativeAPNApplyResult, error) {
		started <- revision
		select {
		case <-release:
		case <-ctx.Done():
			return adapters.APNSettingsView{}, modem.NativeAPNApplyResult{}, ctx.Err()
		}
		return adapters.APNSettingsView{AppliedRevision: &revision}, modem.NativeAPNApplyResult{Status: "applied"}, nil
	}, func(job apnApplyJob) error { mu.Lock(); defer mu.Unlock(); saved = append(saved, job); return nil })
	t.Cleanup(j.close)
	job, e := j.start(4)
	if e != nil {
		t.Fatal(e)
	}
	if !apnJobID.MatchString(job.ID) || job.Verified {
		t.Fatal(job)
	}
	select {
	case revision := <-started:
		if revision != 4 {
			t.Fatal(revision)
		}
	case <-time.After(time.Second):
		t.Fatal("operation not started")
	}
	if _, e = j.start(5); e == nil || e.Error() != "APN_APPLY_IN_PROGRESS" {
		t.Fatalf("parallel apply: %v", e)
	}
	close(release)
	j.wg.Wait()
	final, ok := j.get(job.ID)
	if !ok || final.Status != "succeeded" || !final.Verified || final.AppliedRevision == nil || *final.AppliedRevision != 4 {
		t.Fatalf("%+v", final)
	}
	if _, ok = j.get(strings.Repeat("a", 48)); ok {
		t.Fatal("foreign job visible")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(saved) != 3 {
		t.Fatalf("wanted queued/running/final writes, got %d", len(saved))
	}
}
func TestAPNJobErrorsAreBoundedAndDoNotClaimSuccess(t *testing.T) {
	j := newAPNJobs(func(context.Context, int) (adapters.APNSettingsView, modem.NativeAPNApplyResult, error) {
		return adapters.APNSettingsView{}, modem.NativeAPNApplyResult{}, errors.New("private detail must not be returned")
	}, func(apnApplyJob) error { return nil })
	defer j.close()
	job, e := j.start(2)
	if e != nil {
		t.Fatal(e)
	}
	j.wg.Wait()
	final, _ := j.get(job.ID)
	if final.Verified || final.Code != "APN_APPLY_FAILED" || final.Status != "failed" {
		t.Fatal(final)
	}
}
func TestAPNJobSaveFailureNeverStartsNativeOperation(t *testing.T) {
	called := false
	j := newAPNJobs(func(context.Context, int) (adapters.APNSettingsView, modem.NativeAPNApplyResult, error) {
		called = true
		return adapters.APNSettingsView{}, modem.NativeAPNApplyResult{}, nil
	}, func(apnApplyJob) error { return errors.New("disk") })
	defer j.close()
	if _, e := j.start(2); e == nil {
		t.Fatal("missing save error")
	}
	if called {
		t.Fatal("write attempted without job evidence")
	}
}
