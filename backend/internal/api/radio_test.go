package api

import (
	"net/http/httptest"
	"os"
	"testing"

	"github.com/np1-control-center/ncc/internal/config"
)

func TestRadioQueriesAreBoundedAndReadOnly(t *testing.T) {
	directory := t.TempDir()
	server := &Server{cfg: config.Config{StateDir: directory}}
	for _, tc := range []struct {
		query  string
		status int
	}{
		{"", 200}, {"?days=365&cellSize=20&metric=sinr", 200},
		{"?days=0", 400}, {"?days=366", 400}, {"?days=abc", 400},
		{"?cellSize=1", 400}, {"?metric=invalid", 400},
	} {
		response := httptest.NewRecorder()
		server.radioHistory(response, httptest.NewRequest("GET", "http://localhost/radio/history"+tc.query, nil))
		if response.Code != tc.status {
			t.Errorf("%s: got %d, want %d", tc.query, response.Code, tc.status)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("read-only queries created history: %v %v", entries, err)
	}
}
