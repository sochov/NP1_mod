package audio

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
)

func (m *Manager) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /audio/sessions", func(w http.ResponseWriter, r *http.Request) {
		b, e := io.ReadAll(io.LimitReader(r.Body, 2))
		if e != nil || len(b) != 0 || r.URL.RawQuery != "" {
			reply(w, nil, failure(400, "INVALID_AUDIO_REQUEST"))
			return
		}
		s, e := m.Start()
		reply(w, s, e)
	})
	mux.HandleFunc("PUT /audio/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !validID(r.PathValue("id")) || r.URL.RawQuery != "" {
			reply(w, nil, failure(400, "INVALID_AUDIO_REQUEST"))
			return
		}
		sequence, pcm, e := decodeFrame(w, r)
		if e != nil {
			reply(w, nil, failure(400, "INVALID_AUDIO_FRAME"))
			return
		}
		e = m.Frame(r.PathValue("id"), sequence, pcm)
		reply(w, map[string]any{"accepted": e == nil, "sequence": sequence}, e)
	})
	mux.HandleFunc("DELETE /audio/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		b, e := io.ReadAll(io.LimitReader(r.Body, 2))
		if e != nil || len(b) != 0 || r.URL.RawQuery != "" || !validID(r.PathValue("id")) {
			reply(w, nil, failure(400, "INVALID_AUDIO_REQUEST"))
			return
		}
		reply(w, map[string]bool{"stopped": true}, m.Stop(r.PathValue("id")))
	})
}
func validID(id string) bool {
	if len(id) != 48 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func decodeFrame(w http.ResponseWriter, r *http.Request) (uint64, []byte, error) {
	invalid := errors.New("invalid audio frame")
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 14000))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return 0, nil, invalid
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		token, e = d.Token()
		if e != nil {
			return 0, nil, invalid
		}
		name, ok := token.(string)
		if !ok || (name != "sequence" && name != "pcm") {
			return 0, nil, invalid
		}
		if _, exists := fields[name]; exists {
			return 0, nil, invalid
		}
		var raw json.RawMessage
		if d.Decode(&raw) != nil {
			return 0, nil, invalid
		}
		fields[name] = raw
	}
	if token, e = d.Token(); e != nil || token != json.Delim('}') {
		return 0, nil, invalid
	}
	if _, e = d.Token(); e != io.EOF {
		return 0, nil, invalid
	}
	sequence, e := strconv.ParseUint(string(fields["sequence"]), 10, 64)
	if e != nil || sequence == 0 || sequence > 1<<53-1 {
		return 0, nil, invalid
	}
	var encoded string
	if json.Unmarshal(fields["pcm"], &encoded) != nil {
		return 0, nil, invalid
	}
	pcm, e := base64.StdEncoding.Strict().DecodeString(encoded)
	if e != nil || len(pcm) == 0 || len(pcm) > MaxChunkBytes || len(pcm)%2 != 0 {
		return 0, nil, invalid
	}
	return sequence, pcm, nil
}
func reply(w http.ResponseWriter, value any, e error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if e != nil {
		status, code := 503, "AUDIO_UNAVAILABLE"
		var p *Problem
		if errors.As(e, &p) {
			status, code = p.Status, p.Code
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "error", "code": code})
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}
