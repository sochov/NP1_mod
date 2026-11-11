package led

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func screenFixture(t *testing.T) (*ScreenStore, *time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 11, 1, 0, 0, 0, time.UTC)
	s := NewScreenStore(filepath.Join(t.TempDir(), "runtime", "frame.json"))
	s.now = func() time.Time { return now }
	return s, &now
}
func screenValue(seq uint64) ScreenUpdate {
	return ScreenUpdate{Sequence: seq, Zones: [5][3]int{{255, 0, 0}, {0, 255, 0}, {0, 0, 255}, {255, 255, 255}, {255, 128, 0}}}
}
func screenStatus(t *testing.T, e error, want int) {
	t.Helper()
	if e == nil {
		t.Fatal("expected rejection")
	}
	status, _, _ := ScreenErrorStatus(e)
	if status != want {
		t.Fatalf("status=%d want=%d: %v", status, want, e)
	}
}
func TestScreenLeaseAndExpiry(t *testing.T) {
	s, now := screenFixture(t)
	session, e := s.Begin()
	if e != nil {
		t.Fatal(e)
	}
	if !ValidScreenID(session.ID) || session.FrameIntervalMS != 100 || session.IdleTimeoutMS != 1500 {
		t.Fatalf("bad session %+v", session)
	}
	if _, e := os.Stat(s.path); !os.IsNotExist(e) {
		t.Fatal("starting must not invent a frame")
	}
	_, e = s.Begin()
	screenStatus(t, e, 409)
	if e = s.Update(context.Background(), session.ID, screenValue(1)); e != nil {
		t.Fatal(e)
	}
	frame, active, e := ReadScreenFrame(s.path, *now)
	if e != nil || !active || frame.Zones != screenValue(1).Zones {
		t.Fatalf("frame %+v active=%t err=%v", frame, active, e)
	}
	data, _ := os.ReadFile(s.path)
	if strings.Contains(string(data), session.ID) || strings.Contains(string(data), "screenImage") {
		t.Fatal("token/image must not be stored in frame")
	}
	*now = now.Add(1499 * time.Millisecond)
	_, active, e = ReadScreenFrame(s.path, *now)
	if e != nil || !active {
		t.Fatal("lease expired early")
	}
	*now = now.Add(time.Millisecond)
	_, active, e = ReadScreenFrame(s.path, *now)
	if e != nil || active {
		t.Fatal("lease did not expire")
	}
	screenStatus(t, s.Update(context.Background(), session.ID, screenValue(2)), 410)
	next, e := s.Begin()
	if e != nil || next.ID == session.ID {
		t.Fatal("expired capability reused")
	}
	screenStatus(t, s.Update(context.Background(), session.ID, screenValue(3)), 404)
}
func TestScreenResetSequenceAndAtomicFailure(t *testing.T) {
	s, now := screenFixture(t)
	session, _ := s.Begin()
	if e := s.Update(context.Background(), session.ID, screenValue(4)); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(s.path)
	*now = now.Add(ScreenFrameInterval)
	screenStatus(t, s.Update(context.Background(), session.ID, screenValue(4)), 409)
	screenStatus(t, s.Update(context.Background(), strings.Repeat("f", 64), screenValue(5)), 404)
	s.save = func(string, any) error { return errors.New("synthetic atomic write failure") }
	if e := s.Update(context.Background(), session.ID, screenValue(5)); e == nil {
		t.Fatal("write failure accepted")
	}
	after, _ := os.ReadFile(s.path)
	if string(before) != string(after) || s.sequence != 4 {
		t.Fatal("failed write replaced confirmed frame")
	}
	if e := s.End(session.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(s.path); !os.IsNotExist(e) {
		t.Fatal("reset left a frame")
	}
	screenStatus(t, s.Update(context.Background(), session.ID, screenValue(6)), 404)
	if e := s.Reset(); e != nil {
		t.Fatal(e)
	}
}
func TestScreenClockRollbackAndMalformedRuntime(t *testing.T) {
	s, now := screenFixture(t)
	session, _ := s.Begin()
	_ = s.Update(context.Background(), session.ID, screenValue(1))
	*now = now.Add(-time.Second)
	if _, active, e := ReadScreenFrame(s.path, *now); e != nil || active {
		t.Fatal("future frame replayed after clock rollback")
	}
	screenStatus(t, s.Update(context.Background(), session.ID, screenValue(2)), 410)
	valid, _ := json.Marshal(ScreenFrame{screenValue(1), *now, now.Add(ScreenIdleTimeout)})
	for _, data := range []string{`{}`, string(valid) + ` {}`, strings.Replace(string(valid), `"sequence":1`, `"sequence":1,"sequence":2`, 1), strings.Replace(string(valid), `[255,0,0]`, `[255,0]`, 1), strings.Repeat("x", 4097)} {
		if e := os.WriteFile(s.path, []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
		if _, active, e := ReadScreenFrame(s.path, *now); e == nil || active {
			t.Fatalf("malformed runtime accepted: %.80s", data)
		}
	}
}
func TestStrictScreenJSON(t *testing.T) {
	valid, _ := json.Marshal(screenValue(1))
	var value ScreenUpdate
	if e := json.Unmarshal(valid, &value); e != nil {
		t.Fatal(e)
	}
	cases := []string{`null`, `[]`, `{}`, string(valid) + ` {}`, strings.Replace(string(valid), `"sequence":1`, `"sequence":1,"sequence":2`, 1), strings.Replace(string(valid), `"sequence":1`, `"sequence":1,"image":"no"`, 1)}
	for _, replacement := range []string{`0`, `-1`, `1.0`, `null`, `"1"`, `9007199254740992`} {
		cases = append(cases, strings.Replace(string(valid), `"sequence":1`, `"sequence":`+replacement, 1))
	}
	for _, replacement := range []string{`[255,0]`, `[255,0,0,0]`, `[256,0,0]`, `[-1,0,0]`, `[1.0,0,0]`, `[null,0,0]`, `null`} {
		cases = append(cases, strings.Replace(string(valid), `[255,0,0]`, replacement, 1))
	}
	cases = append(cases, strings.Replace(string(valid), `,[255,128,0]`, "", 1), strings.Replace(string(valid), `,[255,128,0]`, `,[255,128,0],[0,0,0]`, 1))
	for _, data := range cases {
		if e := json.Unmarshal([]byte(data), &value); e == nil {
			t.Fatalf("invalid JSON accepted: %s", data)
		}
	}
}
func TestScreenRateWaitCancellationAndReset(t *testing.T) {
	s := NewScreenStore(filepath.Join(t.TempDir(), "frame.json"))
	session, _ := s.Begin()
	if e := s.Update(context.Background(), session.ID, screenValue(1)); e != nil {
		t.Fatal(e)
	}
	result := make(chan error, 1)
	go func() { result <- s.Update(context.Background(), session.ID, screenValue(2)) }()
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		waiting := s.waiting
		s.mu.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("update did not wait")
		}
		time.Sleep(time.Millisecond)
	}
	screenStatus(t, s.Update(context.Background(), session.ID, screenValue(3)), 409)
	if e := s.Reset(); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-result:
		screenStatus(t, e, 410)
	case <-time.After(300 * time.Millisecond):
		t.Fatal("reset failed to release waiter")
	}
	session, _ = s.Begin()
	_ = s.Update(context.Background(), session.ID, screenValue(1))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := s.Update(ctx, session.ID, screenValue(2)); !errors.Is(e, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", e)
	}
}
func TestScreenMinimumIntervalAndLeaseRenewal(t *testing.T) {
	s := NewScreenStore(filepath.Join(t.TempDir(), "frame.json"))
	session, _ := s.Begin()
	if e := s.Update(context.Background(), session.ID, screenValue(1)); e != nil {
		t.Fatal(e)
	}
	first, _, _ := ReadScreenFrame(s.path, time.Now())
	if e := s.Update(context.Background(), session.ID, screenValue(2)); e != nil {
		t.Fatal(e)
	}
	second, active, e := ReadScreenFrame(s.path, time.Now())
	if e != nil || !active || second.UpdatedAt.Sub(first.UpdatedAt) < ScreenFrameInterval {
		t.Fatal("frame rate exceeded 10fps")
	}
	if second.ExpiresAt.Sub(second.UpdatedAt) != ScreenIdleTimeout {
		t.Fatal("lease not renewed")
	}
}
func TestScreenNeverFollowsSymlink(t *testing.T) {
	s, _ := screenFixture(t)
	target := filepath.Join(t.TempDir(), "untouched.json")
	if e := os.WriteFile(target, []byte("untouched"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(filepath.Dir(s.path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(target, s.path); e != nil {
		t.Skip("host cannot create symlinks")
	}
	if _, e := s.Begin(); e == nil {
		t.Fatal("symlink accepted")
	}
	if _, _, e := ReadScreenFrame(s.path, time.Now()); e == nil {
		t.Fatal("symlink read accepted")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "untouched" {
		t.Fatal("symlink target changed")
	}
}
