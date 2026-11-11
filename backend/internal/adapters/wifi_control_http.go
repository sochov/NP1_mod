package adapters

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func wifiDecode(w http.ResponseWriter, r *http.Request, value any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil {
		nativeError(w, 400, "INVALID_WIFI_REQUEST", "入力の形式を確認してください")
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		nativeError(w, 400, "INVALID_WIFI_REQUEST", "JSONは1件だけ指定してください")
		return false
	}
	return true
}
func wifiHTTPError(w http.ResponseWriter, e error) {
	code := e.Error()
	status := 400
	if strings.Contains(code, "UNAVAILABLE") || strings.Contains(code, "ROLLBACK") || strings.Contains(code, "CONFIG") {
		status = 503
	}
	if strings.Contains(code, "PENDING") || strings.Contains(code, "CONFLICT") || strings.Contains(code, "NOT_CONNECTED") || strings.Contains(code, "NOT_FOUND") {
		status = 409
	}
	nativeError(w, status, code, code)
}

// RegisterWiFiHandlers exposes only typed operations on the root broker socket.
func RegisterWiFiHandlers(mux *http.ServeMux) {
	mux.HandleFunc("GET /wifi/networks", func(w http.ResponseWriter, r *http.Request) {
		value, e := WiFiNetworks(r.Context())
		if e != nil {
			wifiHTTPError(w, e)
			return
		}
		recordingJSON(w, 200, value)
	})
	mux.HandleFunc("POST /wifi/scan", func(w http.ResponseWriter, r *http.Request) {
		var body struct{}
		if !wifiDecode(w, r, &body) {
			return
		}
		value, e := WiFiScan(r.Context())
		if e != nil {
			wifiHTTPError(w, e)
			return
		}
		recordingJSON(w, 200, value)
	})
	mux.HandleFunc("POST /wifi/connect-new", func(w http.ResponseWriter, r *http.Request) {
		var body WiFiNewConnection
		if !wifiDecode(w, r, &body) {
			return
		}
		value, e := WiFiConnectNew(r.Context(), body)
		body.Password = ""
		if e != nil {
			wifiHTTPError(w, e)
			return
		}
		recordingJSON(w, 200, value)
	})
	mux.HandleFunc("POST /wifi/connect-saved", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			NetworkID *int   `json:"networkID"`
			ProfileID string `json:"profileID"`
		}
		if !wifiDecode(w, r, &body) {
			return
		}
		if (body.NetworkID == nil) == (body.ProfileID == "") {
			nativeError(w, 400, "INVALID_WIFI_REQUEST", "接続先は1つだけ指定してください")
			return
		}
		var value WiFiTransaction
		var e error
		if body.NetworkID != nil {
			value, e = WiFiConnectSaved(r.Context(), *body.NetworkID)
		} else {
			value, e = WiFiConnectProfile(r.Context(), body.ProfileID)
		}
		if e != nil {
			wifiHTTPError(w, e)
			return
		}
		recordingJSON(w, 200, value)
	})
	for _, action := range []string{"confirm", "rollback"} {
		mux.HandleFunc("POST /wifi/"+action, func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				TransactionID string `json:"transactionID"`
			}
			if !wifiDecode(w, r, &body) {
				return
			}
			var value WiFiTransaction
			var e error
			if action == "confirm" {
				value, e = WiFiConfirm(r.Context(), body.TransactionID)
			} else {
				value, e = WiFiRollback(r.Context(), body.TransactionID)
			}
			if e != nil {
				wifiHTTPError(w, e)
				return
			}
			recordingJSON(w, 200, value)
		})
	}
	mux.HandleFunc("GET /hotspot", func(w http.ResponseWriter, r *http.Request) { recordingJSON(w, 200, APStatus()) })
	mux.HandleFunc("PUT /hotspot", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ExpectedRevision int      `json:"expectedRevision"`
			Settings         APChange `json:"settings"`
		}
		if !wifiDecode(w, r, &body) {
			return
		}
		value, e := ApplyAP(body.ExpectedRevision, body.Settings)
		body.Settings.Passphrase = ""
		if e != nil {
			code := apSafeError(e)
			nativeError(w, 409, code, code)
			return
		}
		recordingJSON(w, 200, value)
	})
}
