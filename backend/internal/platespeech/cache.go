package platespeech

import (
	"io/fs"
	"os"
	"path/filepath"
)

// CacheInfo describes one clip cache for the management screen. Missing lists
// fixed vocabulary or owner messages only, never an observed plate.
type CacheInfo struct {
	Style   int      `json:"style"`
	Total   int      `json:"total"`
	Ready   int      `json:"ready"`
	Bytes   int64    `json:"bytes"`
	Missing []string `json:"missing"`
}

const cacheMissingShown = 20

func describeCache(style int, texts []string, path func(string) string, dir string) CacheInfo {
	info := CacheInfo{Style: style, Missing: []string{}}
	seen := map[string]bool{}
	for _, t := range texts {
		if seen[t] {
			continue
		}
		seen[t] = true
		info.Total++
		if st, e := os.Stat(path(t)); e == nil && st.Size() > 0 {
			info.Ready++
		} else if len(info.Missing) < cacheMissingShown {
			info.Missing = append(info.Missing, t)
		}
	}
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, e error) error {
		if e == nil && !d.IsDir() {
			if st, e := d.Info(); e == nil {
				info.Bytes += st.Size()
			}
		}
		return nil
	})
	return info
}

// VocabCache reports the plate cache (rule messages and the vocabulary) for
// the selected voice, with the generation job.
func (m *Manager) VocabCache() (CacheInfo, Job) {
	m.mu.Lock()
	style, texts, job := m.voice.Style, append(append([]string{}, m.priority...), m.vocab...), m.job
	m.mu.Unlock()
	return describeCache(style, texts, func(t string) string { return m.clipPath(style, t) }, m.clipRoot()), job
}

// RegenerateVocab deletes the plate cache and generates it again. Uploaded
// messages are not part of it.
func (m *Manager) RegenerateVocab() error {
	if e := os.RemoveAll(m.clipRoot()); e != nil {
		return e
	}
	m.Refresh()
	return nil
}

// Refresh forgets earlier API failures and fetches the missing clips now.
func (m *Manager) Refresh() {
	m.mu.Lock()
	m.failures = map[string]string{}
	m.gen++
	m.mu.Unlock()
	m.kick()
}

// PreparedCache reports a fixed-vocabulary cache such as the PICS messages.
func (m *Manager) PreparedCache(style int, texts []string) CacheInfo {
	return describeCache(style, texts, func(t string) string { return m.preparedPath(style, t) }, filepath.Join(m.dir, "prepared"))
}

// ClearPrepared deletes every prepared clip; the caller prepares them again.
func (m *Manager) ClearPrepared() error {
	return os.RemoveAll(filepath.Join(m.dir, "prepared"))
}
