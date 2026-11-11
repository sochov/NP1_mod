package api

import (
	"net/http"
	"regexp"
)

var nasJobIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (s *Server) nasBackupRoutes() {
	s.mux.HandleFunc("GET /api/v1/nas-backup", func(w http.ResponseWriter, r *http.Request) { s.proxy(w, r, "/nas-backup") })
	for _, route := range []struct{ method, suffix, audit string }{
		{"PUT", "/policies", "nas.settings.update"}, {"POST", "/test", "nas.connection.test"}, {"POST", "/jobs", "nas.backup.start"},
	} {
		s.mux.HandleFunc(route.method+" /api/v1/nas-backup"+route.suffix, func(w http.ResponseWriter, r *http.Request) {
			s.forwardControlLimit(w, r, "/nas-backup"+route.suffix, route.audit, 16384)
		})
	}
	s.mux.HandleFunc("DELETE /api/v1/nas-backup/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !nasJobIDPattern.MatchString(id) {
			fail(w, 404, "NAS_JOB_NOT_FOUND", "転送が見つかりません")
			return
		}
		s.forwardControlLimit(w, r, "/nas-backup/jobs/"+id, "nas.backup.cancel", 1024)
	})
}
