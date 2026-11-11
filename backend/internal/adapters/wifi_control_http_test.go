package adapters

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWiFiHTTPOnlyAllowsTypedBoundedOperations(t *testing.T) {
	previous := wifiController
	c, _ := wifiTestController(t)
	wifiController = c
	defer func() { wifiController = previous }()
	mux := http.NewServeMux()
	RegisterWiFiHandlers(mux)
	for _, body := range []string{`{"command":"SAVE_CONFIG"}`, `{"networkID":4,"profileID":"ncc-anything"}`, `{}`, `{"networkID":4} {"networkID":7}`} {
		request := httptest.NewRequest("POST", "/wifi/connect-saved", strings.NewReader(body))
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != 400 {
			t.Fatalf("unexpected status %d", response.Code)
		}
	}
	request := httptest.NewRequest("POST", "/wifi/connect-new", strings.NewReader(`{"ssid":"Private LAN","security":"wpa2","password":"fixture-private-secret"}`))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != 200 || strings.Contains(response.Body.String(), "fixture-private-secret") {
		t.Fatal("typed connection failed or returned secret")
	}
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("GET", "/wifi/networks", nil))
	if response.Code != 200 || strings.Contains(response.Body.String(), "fixture-private-secret") || strings.Contains(response.Body.String(), `"password"`) {
		t.Fatal("public network list contains credential")
	}
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("POST", "/wifi/command", strings.NewReader(`{}`)))
	if response.Code != 404 {
		t.Fatal("arbitrary command endpoint exists")
	}
}
