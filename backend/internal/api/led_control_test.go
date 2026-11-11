package api

import (
	"encoding/json"
	"github.com/np1-control-center/ncc/internal/config"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type ledResponseTransport func(*http.Request) (*http.Response, error)

func (f ledResponseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLEDPublicAPIPreservesSpecificBrokerFailureStatus(t *testing.T) {
	for _, test := range []struct {
		status int
		code   string
	}{{400, "INVALID_LED_SETTINGS"}, {409, "LED_BUSY"}, {409, "REVISION_CONFLICT"}, {503, "LED_START_FAILED"}, {503, "LED_COMMAND_NOT_CONFIRMED"}} {
		t.Run(test.code, func(t *testing.T) {
			server := New(config.Config{StateDir: t.TempDir(), NetworkTrust: true})
			server.client = &http.Client{Transport: ledResponseTransport(func(request *http.Request) (*http.Response, error) {
				if request.Method != "PUT" || request.URL.Path != "/led" {
					t.Fatalf("wrong fixed broker route %s %s", request.Method, request.URL.Path)
				}
				raw, _ := json.Marshal(map[string]string{"status": "error", "code": test.code, "reason": "synthetic classified LED failure"})
				return &http.Response{StatusCode: test.status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
			})}
			request := httptest.NewRequest("PUT", "http://ncc.invalid/api/v1/device/led", strings.NewReader(`{"expectedRevision":1,"settings":{"mode":"off"}}`))
			response := httptest.NewRecorder()
			server.mux.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("specific %s changed to %d", test.code, response.Code)
			}
			var body struct{ Code, Reason string }
			if e := json.Unmarshal(response.Body.Bytes(), &body); e != nil || body.Code != test.code || body.Reason != "synthetic classified LED failure" {
				t.Fatalf("broker failure was wrapped or lost: %s", response.Body.String())
			}
		})
	}
}
