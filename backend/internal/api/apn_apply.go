package api

import (
	"net/http"
	"regexp"
)

var publicAPNJobID = regexp.MustCompile(`^[a-f0-9]{48}$`)

func (s *Server) registerAPNApply() {
	s.mux.HandleFunc("POST /api/v1/network/cellular/apn/apply", func(w http.ResponseWriter, r *http.Request) { s.forwardControl(w, r, "/apn/apply", "apn.apply") })
	s.mux.HandleFunc("GET /api/v1/network/cellular/apn/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !publicAPNJobID.MatchString(id) {
			fail(w, 400, "INVALID_APN_JOB", "APN適用処理を確認してください")
			return
		}
		s.proxy(w, r, "/apn/jobs/"+id)
	})
}
