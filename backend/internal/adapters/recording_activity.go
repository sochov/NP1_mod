package adapters

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"time"
)

const recordingActivityWindow = 15 * time.Second

type recordingActivityFile struct {
	name     string
	size     int64
	modified time.Time
}

type RecordingActivity struct {
	Writing    *bool            `json:"writing"`
	Cameras    map[string]*bool `json:"cameras"`
	ObservedAt time.Time        `json:"observedAt"`
}

type recordingActivity struct {
	token   string
	at      time.Time
	files   map[string]recordingActivityFile
	changed map[string]time.Time
	value   RecordingActivity
}

// Activity observes SD file progress, independently of the service's active flag
// and the finalized-clip cache. It never opens a recording for writing.
func (index *RecordingIndex) Activity(ctx context.Context, now time.Time) (RecordingActivity, error) {
	index.mu.Lock()
	defer index.mu.Unlock()
	unknown := RecordingActivity{Cameras: map[string]*bool{}, ObservedAt: now.UTC()}
	token, err := index.mount()
	if err != nil || token == "" {
		index.activity = recordingActivity{}
		if err == nil {
			err = errors.New("SD identity unavailable")
		}
		return unknown, err
	}
	state := &index.activity
	if state.token != token || now.Before(state.at) || now.Sub(state.at) > 30*time.Second {
		*state = recordingActivity{token: token, changed: map[string]time.Time{}}
	}
	if !state.at.IsZero() && now.Sub(state.at) < 2*time.Second {
		return state.value, nil
	}
	files := map[string]recordingActivityFile{}
	for _, source := range recordingDirectories {
		root, dir, err := openContained(index.root, source.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return unknown, err
		}
		entries, err := dir.ReadDir(20001)
		dir.Close()
		root.Close()
		if err != nil && err != io.EOF {
			return unknown, err
		}
		if len(entries) > 20000 {
			return unknown, errors.New("recording activity inventory truncated")
		}
		for n, entry := range entries {
			if n%32 == 0 {
				if err := ctx.Err(); err != nil {
					return unknown, err
				}
			}
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			camera, _, valid := parseRecordingFilename(entry.Name())
			if !valid {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return unknown, err
			}
			if !info.Mode().IsRegular() || info.Size() == 0 {
				continue
			}
			file := recordingActivityFile{path.Join(source.path, entry.Name()), info.Size(), info.ModTime()}
			previous, exists := files[camera]
			if !exists || file.modified.After(previous.modified) || file.modified.Equal(previous.modified) && file.name > previous.name {
				files[camera] = file
			}
		}
	}
	if after, err := index.mount(); err != nil || after != token {
		index.activity = recordingActivity{}
		return unknown, errors.New("SD changed during activity observation")
	}
	value := RecordingActivity{Cameras: map[string]*bool{}, ObservedAt: now.UTC()}
	anyWriting, allKnown := false, true
	for _, camera := range []string{"front", "in"} {
		file, exists := files[camera]
		fresh := exists && now.Sub(file.modified) >= -2*time.Second && now.Sub(file.modified) <= recordingActivityWindow
		if previous, known := state.files[camera]; !state.at.IsZero() && fresh && (!known || previous.name != file.name || file.size > previous.size || file.modified.After(previous.modified)) {
			state.changed[camera] = now
		}
		changed := state.changed[camera]
		writing := fresh && !changed.IsZero() && now.Sub(changed) <= recordingActivityWindow
		if writing || !fresh || !state.at.IsZero() {
			value.Cameras[camera] = &writing
			anyWriting = anyWriting || writing
		} else {
			allKnown = false
			value.Cameras[camera] = nil
		}
	}
	if anyWriting || allKnown {
		value.Writing = &anyWriting
	}
	state.at, state.files, state.value = now, files, value
	return value, nil
}
