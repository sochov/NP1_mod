package adapters

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/np1-control-center/ncc/internal/modem"
)

func TestAPNApplyRevisionBlocksOtherStoresButKeepsPublicRead(t *testing.T) {
	directory := t.TempDir()
	first, second := NewAPNStore(directory), NewAPNStore(directory)
	if _, e := first.Save(0, apnChanges()); e != nil {
		t.Fatal(e)
	}
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	released, received := false, false
	go func() {
		_, e := first.WithApplyRevision(1, func(modem.NativeAPNProfile1Input) error { close(started); <-release; return nil })
		finished <- e
	}()
	<-started
	defer func() {
		if !released {
			close(release)
		}
		if !received {
			<-finished
		}
	}()
	view, e := second.Read()
	if e != nil || !view.ApplyInProgress || view.BusyReason != "APPLY_IN_PROGRESS" || view.Revision != 1 {
		t.Fatal(view, e)
	}
	if _, e = second.Save(1, apnChanges()); !errors.Is(e, errAPNApplyBusy) {
		t.Fatal("parallel save was not blocked", e)
	}
	if _, e = second.MarkApplied(1); !errors.Is(e, errAPNApplyBusy) {
		t.Fatal("parallel applied marker was not blocked", e)
	}
	if _, e = second.ForApply(1); !errors.Is(e, errAPNApplyBusy) {
		t.Fatal("parallel private read did not report busy", e)
	}
	if _, e = second.seedFromRecovery(func(func(modem.ConfiguredAPNProfile1, string, string) error) error {
		return errors.New("must not execute")
	}); !errors.Is(e, errAPNApplyBusy) {
		t.Fatal("seed was not blocked", e)
	}
	if _, e = first.Save(1, apnChanges()); !errors.Is(e, errAPNApplyBusy) {
		t.Fatal("same-process save blocked instead of reporting busy", e)
	}
	close(release)
	released = true
	e = <-finished
	received = true
	if e != nil {
		t.Fatal(e)
	}
	view, e = second.Read()
	if e != nil || view.ApplyInProgress || view.BusyReason != "" || view.AppliedRevision == nil || *view.AppliedRevision != 1 {
		t.Fatal(view, e)
	}
}
func TestAPNApplyRevisionFailureAndExternalCASConflict(t *testing.T) {
	store := NewAPNStore(t.TempDir())
	if _, e := store.Save(0, apnChanges()); e != nil {
		t.Fatal(e)
	}
	if _, e := store.WithApplyRevision(1, func(modem.NativeAPNProfile1Input) error { return errors.New("APN_NATIVE_READBACK_INCOMPLETE") }); e == nil {
		t.Fatal("failed callback committed")
	}
	view, _ := store.Read()
	if view.AppliedRevision != nil {
		t.Fatal("failure marked applied")
	}
	called := false
	if _, e := store.WithApplyRevision(0, func(modem.NativeAPNProfile1Input) error { called = true; return nil }); e == nil || called {
		t.Fatal("stale revision invoked apply")
	}
	_, e := store.WithApplyRevision(1, func(modem.NativeAPNProfile1Input) error {
		// Simulate an out-of-band writer ignoring the lock; own Save cannot do so.
		value, e := store.load()
		if e != nil {
			return e
		}
		value.Revision = 2
		return store.write(value)
	})
	if e == nil || e.Error() != "REVISION_CONFLICT" {
		t.Fatal("CAS conflict ignored", e)
	}
	view, _ = store.Read()
	if view.Revision != 2 || view.AppliedRevision != nil {
		t.Fatal(view)
	}
}
func TestAPNEditorReadDoesNotCreateLockAndRejectsWrongLock(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "absent")
	store := NewAPNStore(directory)
	if _, e := store.Read(); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Lstat(directory); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("GET created private directory")
	}
	if _, e := store.Save(0, apnChanges()); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(directory, "config.lock"), []byte("not a lock"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := store.Save(1, apnChanges()); e == nil {
		t.Fatal("malformed lock accepted")
	}
}
func TestAPNEditorCrossProcessLock(t *testing.T) {
	directory := t.TempDir()
	store := NewAPNStore(directory)
	if _, e := store.Save(0, apnChanges()); e != nil {
		t.Fatal(e)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestAPNEditorLockHelperProcess$", "-test.count=1")
	child.Env = append(os.Environ(), "NCC_TEST_APN_LOCK_DIRECTORY="+directory)
	if e := child.Start(); e != nil {
		t.Fatal(e)
	}
	waited := false
	defer func() {
		if !waited {
			child.Process.Kill()
			child.Wait()
		}
	}()
	ready := filepath.Join(directory, "test-ready")
	until := time.Now().Add(4 * time.Second)
	for {
		if _, e := os.Stat(ready); e == nil {
			break
		}
		if time.Now().After(until) {
			t.Fatal("lock helper did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	view, e := store.Read()
	if e != nil || !view.ApplyInProgress {
		t.Fatal("crossprocess lock not visible to GET", e)
	}
	if _, e = store.Save(1, apnChanges()); !errors.Is(e, errAPNApplyBusy) {
		t.Fatal("crossprocess writer did not report busy", e)
	}
	if e = os.WriteFile(filepath.Join(directory, "test-release"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	e = child.Wait()
	waited = true
	if e != nil {
		t.Fatal(e)
	}
	if _, e = store.Save(1, apnChanges()); e != nil {
		t.Fatal("lock did not release", e)
	}
}
func TestAPNEditorLockRefusesSymlink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "other-private-file")
	if e := os.WriteFile(target, nil, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(target, filepath.Join(directory, "config.lock")); e != nil {
		t.Skip("symlink unavailable on this developer host")
	}
	if _, e := NewAPNStore(directory).Save(0, apnChanges()); e == nil {
		t.Fatal("symlink lock accepted")
	}
	if st, e := os.Stat(target); e != nil || st.Size() != 0 {
		t.Fatal("other private file modified")
	}
}
func TestAPNEditorLockHelperProcess(t *testing.T) {
	directory := os.Getenv("NCC_TEST_APN_LOCK_DIRECTORY")
	if directory == "" {
		return
	}
	unlock, e := apnEditorLock(directory, true, true)
	if e != nil {
		t.Fatal(e)
	}
	defer unlock()
	if e = os.WriteFile(filepath.Join(directory, "test-ready"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	until := time.Now().Add(6 * time.Second)
	for {
		if _, e := os.Stat(filepath.Join(directory, "test-release")); e == nil {
			return
		}
		if time.Now().After(until) {
			t.Fatal("test lock release timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
