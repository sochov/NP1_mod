package api

import (
	"net/http"
	"time"

	"github.com/np1-control-center/ncc/internal/pics"
	"github.com/np1-control-center/ncc/internal/platespeech"
)

// voiceCache is one clip cache on the management screen.
type voiceCache struct {
	platespeech.CacheInfo
	State     string    `json:"state"` // plate: the generation job state; pics: preparing, ready, error
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
}

func (p *picsService) speechCache() voiceCache {
	p.mu.Lock()
	style, preparing, prepared, reason := p.settings.Style, p.prepCancel != nil, p.speechPrepared, p.speechReason
	p.mu.Unlock()
	c := voiceCache{CacheInfo: p.voice.PreparedCache(style, pics.Vocabulary()), Error: reason}
	switch {
	case c.Ready == c.Total && prepared:
		c.State = "ready"
	case preparing:
		c.State = "generating"
	case reason != "":
		c.State = "error"
	default:
		c.State = "waiting"
	}
	return c
}

// regenerateSpeech deletes the PICS clips and prepares them again; cues are
// spoken again as soon as their own clips are back.
func (p *picsService) regenerateSpeech() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.prepCancel != nil {
		p.prepCancel()
	}
	err := p.voice.ClearPrepared()
	p.restartPreparation()
	return err
}

func (p *picsService) refreshSpeech() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.restartPreparation()
}

func (s *Server) registerVoiceCache() {
	voice := s.plates.voice
	status := func() map[string]any {
		info, job := voice.VocabCache()
		_, key := voice.APIConfig()
		return map[string]any{
			"keyConfigured": key,
			"plate":         voiceCache{CacheInfo: info, State: job.State, Error: job.Error, UpdatedAt: job.UpdatedAt},
			"pics":          s.pics.speechCache(),
		}
	}
	s.mux.HandleFunc("GET /api/v1/voicevox/cache", func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, http.StatusOK, status())
	})
	s.mux.HandleFunc("POST /api/v1/voicevox/cache/{target}/{action}", func(w http.ResponseWriter, r *http.Request) {
		target, action := r.PathValue("target"), r.PathValue("action")
		if (target != "plate" && target != "pics") || (action != "refresh" && action != "regenerate") {
			fail(w, http.StatusNotFound, "UNKNOWN_VOICE_CACHE", "対象の音声キャッシュがありません")
			return
		}
		var err error
		switch target + "/" + action {
		case "plate/refresh":
			voice.Refresh()
		case "plate/regenerate":
			err = voice.RegenerateVocab()
		case "pics/refresh":
			s.pics.refreshSpeech()
		case "pics/regenerate":
			err = s.pics.regenerateSpeech()
		}
		if err != nil {
			s.audit("voicevox.cache."+action, "failed")
			fail(w, http.StatusInternalServerError, "VOICE_CACHE_FAILED", "音声キャッシュを削除できませんでした")
			return
		}
		s.audit("voicevox.cache."+action, target)
		jsonOut(w, http.StatusOK, status())
	})
}
