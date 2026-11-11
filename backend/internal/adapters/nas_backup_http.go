package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/np1-control-center/ncc/internal/nasbackup"
)

var nasJobIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func nasDecode(w http.ResponseWriter, r *http.Request, value any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		nasHTTPError(w, errors.New("NAS_CONFIG_INVALID"))
		return false
	}
	return true
}
func nasHTTPError(w http.ResponseWriter, err error) {
	status, code, reason := NASBackupErrorStatus(err)
	recordingJSON(w, status, map[string]string{"status": "error", "code": code, "reason": reason})
}
func RegisterNASBackupHandlers(mux *http.ServeMux) { registerNASBackupHandlers(mux, nasBackups) }
func registerNASBackupHandlers(mux *http.ServeMux, c *nasBackupController) {
	mux.HandleFunc("GET /nas-backup", func(w http.ResponseWriter, r *http.Request) {
		recordingJSON(w, 200, c.status())
	})
	mux.HandleFunc("PUT /nas-backup/policies", func(w http.ResponseWriter, r *http.Request) {
		var change struct {
			ExpectedRevision *int `json:"expectedRevision"`
			nasbackup.PoliciesChange
		}
		if !nasDecode(w, r, &change) {
			return
		}
		if change.ExpectedRevision == nil {
			nasHTTPError(w, errors.New("NAS_CONFIG_INVALID"))
			return
		}
		value, err := c.savePolicies(*change.ExpectedRevision, change.PoliciesChange)
		if err != nil {
			nasHTTPError(w, err)
			return
		}
		recordingJSON(w, 200, value)
	})
	mux.HandleFunc("POST /nas-backup/test", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ExpectedRevision *int   `json:"expectedRevision"`
			Kind             string `json:"kind"`
			Route            string `json:"route"`
		}
		if !nasDecode(w, r, &request) {
			return
		}
		if request.ExpectedRevision == nil || (request.Kind != "video" && request.Kind != "event") || (request.Route != "wifi" && request.Route != "tailscale") {
			nasHTTPError(w, errors.New("NAS_CONFIG_INVALID"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		value, err := c.testPolicy(ctx, *request.ExpectedRevision, request.Kind, request.Route)
		if err != nil {
			nasHTTPError(w, err)
			return
		}
		recordingJSON(w, 200, value)
	})
	mux.HandleFunc("POST /nas-backup/jobs", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ExpectedRevision *int   `json:"expectedRevision"`
			RecordingID      string `json:"recordingID"`
		}
		if !nasDecode(w, r, &request) {
			return
		}
		if request.ExpectedRevision == nil || len(request.RecordingID) == 0 || len(request.RecordingID) > 1024 {
			nasHTTPError(w, errors.New("NAS_CONFIG_INVALID"))
			return
		}
		value, err := c.start(*request.ExpectedRevision, request.RecordingID)
		if err != nil {
			nasHTTPError(w, err)
			return
		}
		recordingJSON(w, 202, value)
	})
	mux.HandleFunc("DELETE /nas-backup/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !nasJobIDPattern.MatchString(id) {
			nasHTTPError(w, errors.New("NAS_JOB_NOT_FOUND"))
			return
		}
		value, err := c.cancelJob(id)
		if err != nil {
			nasHTTPError(w, err)
			return
		}
		recordingJSON(w, 200, value)
	})
}
