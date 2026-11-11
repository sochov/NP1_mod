package api

import (
	"net/http"
	"regexp"
)

var audioSessionID = regexp.MustCompile(`^[a-f0-9]{48}$`)

func (s *Server) registerAudio() {
	s.mux.HandleFunc("POST /api/v1/device/audio/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			fail(w, 400, "INVALID_AUDIO_REQUEST", "音声のURLを確認してください")
			return
		}
		s.forwardControl(w, r, "/audio/sessions", "audio.start")
	})
	for _, method := range []string{"PUT", "DELETE"} {
		s.mux.HandleFunc(method+" /api/v1/device/audio/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			if r.URL.RawQuery != "" {
				fail(w, 400, "INVALID_AUDIO_REQUEST", "音声のURLを確認してください")
				return
			}
			if !audioSessionID.MatchString(id) {
				fail(w, 400, "INVALID_AUDIO_SESSION", "音声セッションを確認してください")
				return
			}
			action := ""
			if r.Method == http.MethodDelete {
				action = "audio.stop"
			}
			s.forwardControlLimit(w, r, "/audio/sessions/"+id, action, 14000)
		})
	}
}
