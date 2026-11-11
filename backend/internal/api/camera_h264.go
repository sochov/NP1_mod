package api

import (
	"io"
	"net/http"
	"net/url"
	"time"
)

func (s *Server) cameraH264(w http.ResponseWriter, r *http.Request) {
	owner, _ := s.auth.Get(r)
	id := r.PathValue("id")
	s.mu.Lock()
	camera, ok := s.cameras[id]
	if !ok || camera.Owner != owner.CSRF || time.Now().After(camera.Expires) {
		s.mu.Unlock()
		fail(w, 403, "CAMERA_SESSION_EXPIRED", "配信を再開してください")
		return
	}
	active := 0
	for _, entry := range s.cameras {
		active += entry.Active
	}
	if active >= 2 || camera.Active != 0 {
		s.mu.Unlock()
		fail(w, 429, "CAMERA_BUSY", "映像の配信数が上限に達しています")
		return
	}
	camera.Active++
	camera.Expires = time.Now().Add(30 * time.Second)
	s.cameras[id] = camera
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if current, ok := s.cameras[id]; ok {
			current.Active--
			current.Expires = time.Now().Add(30 * time.Second)
			s.cameras[id] = current
		}
	}()
	request, err := http.NewRequestWithContext(r.Context(), "GET", "http://broker/camera/h264?source="+url.QueryEscape(camera.Source), nil)
	if err != nil {
		fail(w, 500, "CAMERA_REQUEST_FAILED", "映像を取得できません")
		return
	}
	response, err := s.client.Do(request)
	if err != nil {
		fail(w, 503, "CAMERA_H264_UNAVAILABLE", "映像の準備を待っています")
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		status := http.StatusServiceUnavailable
		if response.StatusCode == 429 {
			status = 429
		}
		fail(w, status, "CAMERA_H264_UNAVAILABLE", "映像の準備を待っています")
		return
	}
	for _, key := range []string{"Content-Type", "X-Camera-Codec", "X-Camera-Width", "X-Camera-Height", "X-Camera-FPS", "X-Camera-Audio"} {
		if value := response.Header.Get(key); value != "" {
			w.Header().Set(key, value)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	controller := http.NewResponseController(w)
	var buffer [32768]byte
	for {
		n, readError := response.Body.Read(buffer[:])
		if n > 0 {
			s.mu.Lock()
			current, exists := s.cameras[id]
			if exists {
				current.Expires = time.Now().Add(30 * time.Second)
				s.cameras[id] = current
			}
			s.mu.Unlock()
			if !exists {
				return
			}
			_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := w.Write(buffer[:n]); err != nil {
				return
			}
			if controller.Flush() != nil {
				return
			}
		}
		if readError != nil {
			if readError != io.EOF {
				return
			}
			return
		}
	}
}
