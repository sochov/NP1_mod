package audio

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This output intentionally ignores ctx. Closing its pipe is the only way to release Write.
type closeOnlyOutput struct {
	entered, closed chan struct{}
	once            sync.Once
	closeCount      atomic.Int32
}

func (p *closeOnlyOutput) Write(b []byte) (int, error) {
	p.once.Do(func() { close(p.entered) })
	<-p.closed
	return 0, io.ErrClosedPipe
}
func (p *closeOnlyOutput) Close() error {
	if p.closeCount.Add(1) != 1 {
		return errors.New("duplicate Close")
	}
	close(p.closed)
	return nil
}
func TestReviewStopAndIdleCloseNonCooperativePipeAndJoinWorkers(t *testing.T) {
	for _, stop := range []string{"explicit", "idle", "manager-close"} {
		t.Run(stop, func(t *testing.T) {
			output := &closeOnlyOutput{entered: make(chan struct{}), closed: make(chan struct{})}
			m := New(func(context.Context) (Process, error) { return output, nil })
			t.Cleanup(m.Close)
			now := time.Now()
			m.now = func() time.Time { return now }
			session, e := m.Start()
			if e != nil {
				t.Fatal(e)
			}
			m.mu.Lock()
			live := m.current
			m.mu.Unlock()
			if e = m.Frame(session.ID, 1, make([]byte, 9600)); e != nil {
				t.Fatal(e)
			}
			select {
			case <-output.entered:
			case <-time.After(time.Second):
				t.Fatal("writer not blocked")
			}
			switch stop {
			case "explicit":
				if e = m.Stop(session.ID); e != nil {
					t.Fatal(e)
				}
			case "manager-close":
				m.Close()
			case "idle":
				m.mu.Lock()
				now = now.Add(IdleTimeout)
				m.mu.Unlock()
			}
			select {
			case <-live.done:
			case <-time.After(700 * time.Millisecond):
				t.Fatal("writer/watchdog not joined")
			}
			if output.closeCount.Load() != 1 {
				t.Fatal("Close was not called once")
			}
			m.mu.Lock()
			current := m.current
			m.mu.Unlock()
			if current != nil {
				t.Fatal("finished output retained its capability")
			}
		})
	}
}
func TestReviewBoundedQueueOwnsBytesAndKeepsNewestChunks(t *testing.T) {
	output := &closeOnlyOutput{entered: make(chan struct{}), closed: make(chan struct{})}
	m := New(func(context.Context) (Process, error) { return output, nil })
	defer m.Close()
	now := time.Now()
	m.now = func() time.Time { return now }
	session, _ := m.Start()
	_ = m.Frame(session.ID, 1, []byte{1, 0})
	<-output.entered
	for i := 2; i <= 20; i++ {
		m.mu.Lock()
		now = now.Add(50 * time.Millisecond)
		m.mu.Unlock()
		data := []byte{byte(i), 0}
		if e := m.Frame(session.ID, uint64(i), data); e != nil {
			t.Fatal(e)
		}
		data[0] = 255
	}
	m.mu.Lock()
	s := m.current
	if len(s.queue) != 3 {
		t.Fatalf("unbounded/wrong queue length: %d", len(s.queue))
	}
	var actual []byte
	for len(s.queue) > 0 {
		actual = append(actual, (<-s.queue)[0])
	}
	m.mu.Unlock()
	if !bytes.Equal(actual, []byte{18, 19, 20}) {
		t.Fatalf("stale data retained or borrowed caller memory: %v", actual)
	}
}
func TestReviewClockRollbackAndSafeSequence(t *testing.T) {
	m, _ := testManager(t, false)
	now := time.Now()
	m.now = func() time.Time { return now }
	session, _ := m.Start()
	requireProblem(t, m.Frame(session.ID, 1<<53, []byte{0, 0}), 400)
	m.mu.Lock()
	now = now.Add(-time.Second)
	m.mu.Unlock()
	requireProblem(t, m.Frame(session.ID, 1, []byte{0, 0}), 410)
	next, e := m.Start()
	if e != nil || next.ID == session.ID {
		t.Fatalf("rollback did not expire old output: %v", e)
	}
}
func TestReviewHTTPStrictPayloadAndSessionBoundaries(t *testing.T) {
	m, _ := testManager(t, false)
	mux := http.NewServeMux()
	m.Register(mux)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	for _, body := range []string{"{}", " ", "null", strings.Repeat("x", 20000)} {
		if w := call("POST", "/audio/sessions", body); w.Code != 400 {
			t.Fatal("nonempty POST accepted")
		}
	}
	if w := call("POST", "/audio/sessions?output=other", ""); w.Code != 400 {
		t.Fatal("caller output selection accepted")
	}
	w := call("POST", "/audio/sessions", "")
	var session Session
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &session) != nil {
		t.Fatal("start failed", w.Body)
	}
	path := "/audio/sessions/" + session.ID
	pcm := base64.StdEncoding.EncodeToString(make([]byte, MaxChunkBytes))
	valid := `{"sequence":1,"pcm":"` + pcm + `"}`
	invalid := []string{`null`, `{}`, valid + `{}`, strings.Replace(valid, `"sequence":1`, `"sequence":1,"sequence":2`, 1), strings.Replace(valid, `"sequence":1`, `"sequence":1,"file":"other"`, 1), strings.Replace(valid, `"sequence":1`, `"sequence":null`, 1), strings.Replace(valid, `"sequence":1`, `"sequence":1.0`, 1), strings.Replace(valid, `"sequence":1`, `"sequence":9007199254740992`, 1), `{"sequence":1,"pcm":null}`, `{"sequence":1,"pcm":"AAAA"}`, `{"sequence":1,"pcm":"a"}`, `{"sequence":1,"pcm":"` + base64.StdEncoding.EncodeToString(make([]byte, MaxChunkBytes+2)) + `"}`, strings.Repeat(" ", 14001)}
	for _, body := range invalid {
		if w = call("PUT", path, body); w.Code != 400 {
			t.Fatalf("invalid body accepted %d %.80s", w.Code, body)
		}
	}
	if w = call("PUT", path, valid); w.Code != 200 {
		t.Fatalf("valid frame rejected: %d %s", w.Code, w.Body)
	}
	if w = call("PUT", path, valid); w.Code != 409 {
		t.Fatal("duplicate seq accepted")
	}
	if w = call("DELETE", path, "{}"); w.Code != 400 {
		t.Fatal("DELETE payload accepted")
	}
	if w = call("DELETE", path+"?sink=other", ""); w.Code != 400 {
		t.Fatal("DELETE query accepted")
	}
	if w = call("DELETE", path, ""); w.Code != 200 {
		t.Fatalf("stop not confirmed %d", w.Code)
	}
	if w = call("PUT", path, valid); w.Code != 410 {
		t.Fatal("stopped capability accepted")
	}
}

func TestReviewPulseCommandHasFixedSinkAndStreamLocalVolume(t *testing.T) {
	cmd := pulseCommand(context.Background())
	if cmd.Path != "/usr/bin/pacat" || cmd.WaitDelay != 500*time.Millisecond {
		t.Fatal("unexpected executable or unbounded pipe wait")
	}
	arguments := " " + strings.Join(cmd.Args[1:], " ") + " "
	for _, value := range []string{"--playback", "--raw", "--server=unix:/run/pulse/native", "--device=primary", "--format=s16le", "--rate=24000", "--channels=1", "--volume=16384"} {
		if !strings.Contains(arguments, " "+value+" ") {
			t.Fatalf("missing fixed output option %s", value)
		}
	}
	for _, value := range []string{"--record", "--file", "--device=default", "set-sink-volume", "set-default-sink"} {
		if strings.Contains(arguments, value) {
			t.Fatalf("unexpected recording/persistent/global output option %s", value)
		}
	}
}
