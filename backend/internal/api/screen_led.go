package api

import (
	"io"
	"net/http"
	"net/url"

	"github.com/np1-control-center/ncc/internal/led"
)

func (s *Server) registerScreenLEDRoutes() {
	s.mux.HandleFunc("POST /api/v1/device/led/screen/sessions", func(w http.ResponseWriter, r *http.Request) {
		s.screenLEDControl(w, r, "/led/screen/sessions", "led.screen.start")
	})
	for _, method := range []string{"PUT", "DELETE"} {
		s.mux.HandleFunc(method+" /api/v1/device/led/screen/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			if !led.ValidScreenID(id) {
				fail(w, 400, "INVALID_SCREEN_ID", "画面連動セッションを確認してください")
				return
			}
			action := ""
			if r.Method == "DELETE" {
				action = "led.screen.stop"
			}
			s.screenLEDControl(w, r, "/led/screen/sessions/"+url.PathEscape(id), action)
		})
	}
}

// Frame acknowledgements mean the worker's RAM queue accepted RGB, not optical readback.
// Per-frame traffic is deliberately excluded from persistent audit logs and contains no session token there.
func (s *Server) screenLEDControl(w http.ResponseWriter, r *http.Request, target, action string) {
	if len(r.URL.Query()) != 0 {
		fail(w, 400, "INVALID_SCREEN_REQUEST", "画面連動のURLを確認してください")
		return
	}
	request, e := http.NewRequestWithContext(r.Context(), r.Method, "http://broker"+target, http.MaxBytesReader(w, r.Body, 1024))
	if e != nil {
		fail(w, 400, "INVALID_SCREEN_REQUEST", "入力を確認してください")
		return
	}
	request.Header.Set("Content-Type", "application/json")
	response, e := s.client.Do(request)
	if e != nil {
		fail(w, 503, "SCREEN_LED_UNAVAILABLE", "画面連動LEDへ接続できません")
		return
	}
	defer response.Body.Close()
	if action != "" {
		result := "rejected"
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			result = "accepted"
		}
		s.audit(action, result)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(response.Body, 4096))
}
