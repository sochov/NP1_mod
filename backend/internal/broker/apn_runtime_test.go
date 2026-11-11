package broker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAPNPersistedJobIsBoundedAndNeverCreatesOrAcceptsAmbiguousState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "job.json")
	now := time.Now().UTC()
	if _, e := readAPNPersistedJob(path, now); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	write := func(v any) {
		t.Helper()
		raw, _ := json.Marshal(v)
		if e := os.WriteFile(path, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	job := apnApplyJob{ID: strings.Repeat("a", 48), Revision: 2, Status: "running", StartedAt: now}
	write(job)
	if _, e := readAPNPersistedJob(path, now); e != nil {
		t.Fatal(e)
	}
	for _, modify := range []func(*apnApplyJob){
		func(j *apnApplyJob) { j.ID = "arbitrary" }, func(j *apnApplyJob) { j.Status = "accepted" },
		func(j *apnApplyJob) { j.StartedAt = now.Add(2 * time.Minute) }, func(j *apnApplyJob) { j.Status = "succeeded"; j.Verified = true },
	} {
		bad := job
		modify(&bad)
		write(bad)
		if _, e := readAPNPersistedJob(path, now); e == nil {
			t.Fatal("invalid persisted job accepted")
		}
	}
	for _, raw := range []string{strings.Repeat("x", 32769), `{} {}`, `{"unknown":"private value"}`} {
		os.WriteFile(path, []byte(raw), 0600)
		if _, e := readAPNPersistedJob(path, now); e == nil || strings.Contains(e.Error(), "private value") {
			t.Fatal("invalid job or leaked detail")
		}
	}
	write(job)
	link := filepath.Join(filepath.Dir(path), "link.json")
	if os.Symlink(path, link) == nil {
		if _, e := readAPNPersistedJob(link, now); e == nil {
			t.Fatal("symlink job accepted")
		}
	}
}
