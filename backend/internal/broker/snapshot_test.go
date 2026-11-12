package broker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestConcurrentSnapshotReadersWaitAndRemainSerial(t *testing.T) {
	entered, release := make(chan string, 2), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	handler := snapshotHandler(func(source, color string) ([]byte, error) {
		entered <- source
		if source == "front" {
			<-release
		}
		return []byte("synthetic JPEG"), nil
	})
	first, second := httptest.NewRecorder(), httptest.NewRecorder()
	doneFirst, doneSecond := make(chan struct{}), make(chan struct{})
	go func() {
		handler.ServeHTTP(first, httptest.NewRequest("GET", "/?source=front&color=auto", nil))
		close(doneFirst)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first snapshot never started")
	}
	go func() {
		handler.ServeHTTP(second, httptest.NewRequest("GET", "/?source=in&color=auto", nil))
		close(doneSecond)
	}()
	select {
	case <-entered:
		t.Fatal("JPEG conversions overlapped")
	case <-doneSecond:
		t.Fatal("second reader was rejected instead of waiting")
	case <-time.After(30 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	for _, done := range []chan struct{}{doneFirst, doneSecond} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("queued snapshot did not finish")
		}
	}
	for _, response := range []*httptest.ResponseRecorder{first, second} {
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/jpeg" {
			t.Fatalf("snapshot: %d %s", response.Code, response.Body.String())
		}
	}
}

func TestSnapshotWaitHonorsCancellation(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	handler := snapshotHandler(func(source, color string) ([]byte, error) {
		if source == "front" {
			close(entered)
			<-release
		}
		return []byte("synthetic JPEG"), nil
	})
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/?source=front&color=auto", nil))
		close(done)
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/?source=in&color=auto", nil).WithContext(ctx))
	if response.Code != http.StatusRequestTimeout {
		t.Fatalf("canceled reader: %d", response.Code)
	}
	once.Do(func() { close(release) })
	<-done
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/?source=in&color=auto", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("reader after cancellation: %d", response.Code)
	}
}
