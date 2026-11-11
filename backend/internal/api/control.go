package api

import (
	"io"
	"net/http"
)

// target and action are fixed by the route declaration, never accepted from a request body.
func (s *Server) forwardControl(w http.ResponseWriter, r *http.Request, target, action string) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	request, e := http.NewRequestWithContext(r.Context(), r.Method, "http://broker"+target, r.Body)
	if e != nil {
		fail(w, 400, "INVALID_CONTROL_REQUEST", "入力を確認してください")
		return
	}
	response, e := s.client.Do(request)
	if e != nil {
		fail(w, 503, "CONTROL_UNAVAILABLE", "本体の設定処理へ接続できません")
		return
	}
	defer response.Body.Close()
	result := "rejected"
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		result = "accepted"
	}
	s.audit(action, result)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(response.StatusCode)
	io.Copy(w, io.LimitReader(response.Body, 64*1024))
}
