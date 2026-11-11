package adapters

import (
	"context"
	"errors"
	"os"
	"path"
	"strconv"
	"time"
)

// Open accepts only a card-scoped recording ID, never an arbitrary path. The
// caller closes both returned handles. Finalized metadata and mount identity
// are checked again instead of trusting an earlier cached catalog response.
func (index *RecordingIndex) Open(ctx context.Context, id string) (*os.Root, *os.File, RecordingClip, error) {
	index.mu.Lock()
	defer index.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, nil, RecordingClip{}, err
	}
	token, err := index.mount()
	if err != nil || token == "" {
		return nil, nil, RecordingClip{}, errors.New("NO_SD")
	}
	relative, err := recordingPath(id, token)
	if err != nil {
		return nil, nil, RecordingClip{}, errors.New("SD_CHANGED")
	}
	root, file, err := openContained(index.root, relative)
	if err != nil {
		return nil, nil, RecordingClip{}, errors.New("RECORDING_UNAVAILABLE")
	}
	fail := func(code string) (*os.Root, *os.File, RecordingClip, error) {
		_ = file.Close()
		_ = root.Close()
		return nil, nil, RecordingClip{}, errors.New(code)
	}
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || time.Since(before.ModTime()) < 2*time.Second {
		return fail("RECORDING_NOT_FINALIZED")
	}
	meta, err := ReadRecordingMetadata(file, before.Size())
	if err != nil {
		return fail("RECORDING_NOT_FINALIZED")
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return fail("RECORDING_CHANGED")
	}
	current, err := index.mount()
	if err != nil || current != token {
		return fail("SD_CHANGED")
	}
	if ctx.Err() != nil {
		return fail("RECORDING_OPEN_CANCELLED")
	}
	camera, start, _ := parseRecordingFilename(path.Base(relative))
	kind := ""
	for _, directory := range recordingDirectories {
		if path.Dir(relative) == directory.path {
			kind = directory.kind
		}
	}
	clip := RecordingClip{ID: id, Camera: camera, Kind: kind, Name: path.Base(relative), StartAt: start, EndAt: start.Add(time.Duration(meta.DurationSeconds * float64(time.Second))), SizeBytes: strconv.FormatInt(before.Size(), 10), MP4Metadata: meta}
	return root, file, clip, nil
}
