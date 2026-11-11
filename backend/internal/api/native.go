package api

import (
	"io"
	"net/http"
	"net/url"
)

func (s *Server) updateNative(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	req, e := http.NewRequestWithContext(r.Context(), "PUT", "http://broker/native-settings/"+url.PathEscape(r.PathValue("id")), r.Body)
	if e != nil {
		fail(w, 400, "INVALID_NATIVE_SETTING", "設定項目を確認してください")
		return
	}
	resp, e := s.client.Do(req)
	if e != nil {
		fail(w, 503, "NATIVE_SETTINGS_UNAVAILABLE", "純正設定の状態を取得できません")
		return
	}
	defer resp.Body.Close()
	result := "rejected"
	if resp.StatusCode == http.StatusOK {
		result = "accepted"
	}
	s.audit("native.setting.update", result)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, io.LimitReader(resp.Body, 64*1024))
}
