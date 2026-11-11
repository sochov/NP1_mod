package nasbackup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	coreconfig "github.com/np1-control-center/ncc/internal/config"
	"github.com/np1-control-center/ncc/internal/recordingmedia"
)

type Result struct {
	Status       string    `json:"status"`
	SHA256       string    `json:"sha256,omitempty"`
	Bytes        string    `json:"bytes"`
	ResumedBytes string    `json:"resumedBytes"`
	OriginalName string    `json:"originalName,omitempty"`
	Camera       string    `json:"camera,omitempty"`
	StartAt      time.Time `json:"startAt,omitempty"`
}
type Progress struct {
	Stage        string `json:"stage"`
	Bytes        string `json:"bytes"`
	TotalBytes   string `json:"totalBytes"`
	ResumedBytes string `json:"resumedBytes"`
}
type progressReader struct {
	reader io.Reader
	count  int64
	report func(int64)
}

func (r *progressReader) Read(data []byte) (int, error) {
	n, err := r.reader.Read(data)
	r.count += int64(n)
	if r.report != nil {
		r.report(r.count)
	}
	return n, err
}

type progressWriter struct {
	writer io.Writer
	count  int64
	report func(int64)
}

func (w *progressWriter) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	w.count += int64(n)
	if w.report != nil {
		w.report(w.count)
	}
	return n, err
}

type checkpoint struct {
	Version        int       `json:"version"`
	Target         string    `json:"target"`
	Hash           string    `json:"hash"`
	Size           int64     `json:"size"`
	Part           string    `json:"part"`
	Final          string    `json:"final"`
	Created        bool      `json:"created"`
	RemoteIdentity string    `json:"remoteIdentity"`
	Complete       bool      `json:"complete"`
	OriginalName   string    `json:"originalName"`
	Camera         string    `json:"camera"`
	StartAt        time.Time `json:"startAt"`
}
type Engine struct {
	directory string
	remote    Remote
	mu        sync.Mutex
}

var originalName = regexp.MustCompile(`(?i)^FILE([ME])_([0-9]{6})_([0-9]{6})_([0-9]+)\.MP4$`)

func NewEngine(stateDirectory string, remote Remote) (*Engine, error) {
	if remote == nil || remote.TargetKey() == "" {
		return nil, errors.New("NAS_TARGET_INVALID")
	}
	if err := prepareDirectory(stateDirectory); err != nil {
		return nil, err
	}
	return &Engine{directory: stateDirectory, remote: remote}, nil
}
func (e *Engine) String() string   { return "NAS backup engine [redacted]" }
func (e *Engine) GoString() string { return e.String() }

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

type writeOnly struct{ io.Writer }

func hashReader(ctx context.Context, reader io.Reader, limit int64) (string, int64, error) {
	hash := sha256.New()
	count, err := io.CopyBuffer(hash, contextReader{ctx, io.LimitReader(reader, limit+1)}, make([]byte, 64*1024))
	if err != nil {
		return "", count, err
	}
	if count > limit {
		return "", count, errors.New("NAS_REMOTE_SIZE_MISMATCH")
	}
	return hex.EncodeToString(hash.Sum(nil)), count, nil
}
func (e *Engine) verifyRemote(ctx context.Context, name string, size int64, digest string) error {
	return e.verifyRemoteProgress(ctx, name, size, digest, nil)
}
func (e *Engine) verifyRemoteProgress(ctx context.Context, name string, size int64, digest string, report func(int64)) error {
	info, err := e.remote.Stat(ctx, name)
	if err != nil || !info.Regular || info.Size != size {
		return safeError(ctx.Err(), "NAS_REMOTE_SIZE_MISMATCH")
	}
	reader, err := e.remote.Open(ctx, name)
	if err != nil {
		return safeError(ctx.Err(), "NAS_READBACK_FAILED")
	}
	hash, count, err := hashReader(ctx, &progressReader{reader: reader, report: report}, size)
	closeErr := reader.Close()
	if err != nil || closeErr != nil || count != size || hash != digest {
		return safeError(ctx.Err(), "NAS_READBACK_MISMATCH")
	}
	after, err := e.remote.Stat(ctx, name)
	if err != nil || after != info {
		return safeError(ctx.Err(), "NAS_REMOTE_CHANGED")
	}
	return nil
}
func (e *Engine) load(name string) (checkpoint, error) {
	filename := filepath.Join(e.directory, name+".json")
	info, err := os.Lstat(filename)
	if err != nil {
		return checkpoint{}, err
	}
	if privateRegular(info) != nil {
		return checkpoint{}, errors.New("NAS_STATE_UNSAFE")
	}
	raw, err := os.ReadFile(filename)
	if err != nil || len(raw) > 8192 {
		return checkpoint{}, errors.New("NAS_STATE_INVALID")
	}
	var value checkpoint
	if json.Unmarshal(raw, &value) != nil || value.Version != 1 {
		return checkpoint{}, errors.New("NAS_STATE_INVALID")
	}
	return value, nil
}
func (e *Engine) save(name string, state checkpoint) error {
	if err := coreconfig.Atomic(filepath.Join(e.directory, name+".json"), state); err != nil {
		return errors.New("NAS_STATE_WRITE_FAILED")
	}
	return nil
}

// Backup preserves the exact bytes of one confirmed finalized original. The
// caller retains ownership of the already securely-opened source descriptor.
// Cancellation retains only our journal/part, verified afresh on the next run.
func (e *Engine) Backup(ctx context.Context, source *os.File) (result Result, err error) {
	return e.BackupWithProgress(ctx, source, nil)
}
func (e *Engine) BackupWithProgress(ctx context.Context, source *os.File, progress func(Progress)) (result Result, err error) {
	result = Result{Status: "failed", Bytes: "0", ResumedBytes: "0"}
	report := func(stage string, done, total int64) {
		if progress != nil {
			progress(Progress{stage, strconv.FormatInt(done, 10), strconv.FormatInt(total, 10), result.ResumedBytes})
		}
	}
	defer func() {
		if ctx.Err() != nil && err != nil {
			result.Status = "cancelled"
			err = ctx.Err()
		}
	}()
	if !e.mu.TryLock() {
		return result, errors.New("NAS_BUSY")
	}
	defer e.mu.Unlock()
	unlock, err := lockState(e.directory)
	if err != nil {
		return result, err
	}
	defer unlock()
	if source == nil {
		return result, errors.New("NAS_SOURCE_INVALID")
	}
	before, err := source.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() < 24 || before.Size() > 64*1024*1024*1024 {
		return result, errors.New("NAS_SOURCE_INVALID")
	}
	name := filepath.Base(source.Name())
	match := originalName.FindStringSubmatch(name)
	if match == nil || len(name) > 128 {
		return result, errors.New("NAS_ORIGINAL_NAME_INVALID")
	}
	start, err := time.ParseInLocation("20060102_150405", "20"+match[2]+"_"+match[3], time.FixedZone("JST", 9*3600))
	if err != nil {
		return result, errors.New("NAS_ORIGINAL_NAME_INVALID")
	}
	if _, err = recordingmedia.ConfirmOriginal(source, before.Size()); err != nil {
		return result, errors.New("NAS_ORIGINAL_MEDIA_UNCONFIRMED")
	}
	report("hashing", 0, before.Size())
	result.OriginalName, result.StartAt = name, start.UTC()
	result.Camera = "front"
	if strings.EqualFold(match[1], "E") {
		result.Camera = "cabin"
	}
	digest, count, err := hashReader(ctx, &progressReader{reader: io.NewSectionReader(source, 0, before.Size()), report: func(n int64) { report("hashing", n, before.Size()) }}, before.Size())
	if err != nil || count != before.Size() {
		return result, safeError(ctx.Err(), "NAS_SOURCE_READ_FAILED")
	}
	result.SHA256, result.Bytes = digest, strconv.FormatInt(count, 10)
	keyHash := sha256.Sum256([]byte(e.remote.TargetKey() + "\x00" + name + "\x00" + digest))
	key := hex.EncodeToString(keyHash[:])
	state := checkpoint{Version: 1, Target: e.remote.TargetKey(), Hash: digest, Size: count, Part: "ncc-" + key + ".part", Final: strings.TrimSuffix(name, filepath.Ext(name)) + "-" + digest + ".MP4", OriginalName: name, Camera: result.Camera, StartAt: result.StartAt}
	if info, statErr := e.remote.Stat(ctx, state.Final); statErr == nil {
		if !info.Regular {
			return result, errors.New("NAS_FINAL_CONFLICT")
		}
		if err = e.verifyRemoteProgress(ctx, state.Final, count, digest, func(n int64) { report("verifying", n, count) }); err != nil {
			return result, err
		}
		after, statErr := source.Stat()
		if statErr != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
			return result, errors.New("NAS_SOURCE_CHANGED")
		}
		result.Status = "already-complete"
		report("already-complete", count, count)
		return result, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return result, safeError(ctx.Err(), "NAS_REMOTE_STAT_FAILED")
	}
	previous, loadErr := e.load(key)
	if loadErr == nil {
		if previous.Target != state.Target || previous.Hash != state.Hash || previous.Size != state.Size || previous.Part != state.Part || previous.Final != state.Final || previous.OriginalName != state.OriginalName {
			return result, errors.New("NAS_STATE_CONFLICT")
		}
		state = previous
	} else if !os.IsNotExist(loadErr) {
		return result, loadErr
	}
	info, statErr := e.remote.Stat(ctx, state.Part)
	var part Part
	var offset int64
	var openedSize int64
	if statErr == nil {
		if !state.Created || !info.Regular || info.Identity == "" || info.Identity != state.RemoteIdentity || info.Size < 0 || info.Size > count {
			return result, errors.New("NAS_PART_NOT_OWNED")
		}
		offset = info.Size
		openedSize = info.Size
		prefix, _, hashErr := hashReader(ctx, io.NewSectionReader(source, 0, offset), offset)
		if hashErr != nil {
			return result, safeError(ctx.Err(), "NAS_SOURCE_READ_FAILED")
		}
		if err = e.verifyRemote(ctx, state.Part, offset, prefix); err != nil {
			return result, err
		}
		part, err = e.remote.OpenPart(ctx, state.Part, false)
		if restart, ok := e.remote.(interface{ RestartOwnedPartFromZero() bool }); ok && restart.RestartOwnedPartFromZero() {
			offset = 0
			report("restarting", 0, count)
		}
	} else if errors.Is(statErr, os.ErrNotExist) {
		state.Created, state.Complete = false, false
		if err = e.save(key, state); err != nil {
			return result, err
		}
		part, err = e.remote.OpenPart(ctx, state.Part, true)
		if err == nil {
			created, statErr := part.Stat()
			if statErr != nil || !created.Regular || created.Size != 0 || created.Identity == "" {
				_ = part.Close()
				return result, errors.New("NAS_PART_IDENTITY_UNCONFIRMED")
			}
			state.Created, state.RemoteIdentity = true, created.Identity
			if err = e.save(key, state); err != nil {
				_ = part.Close()
				return result, err
			}
		}
	} else {
		return result, safeError(ctx.Err(), "NAS_REMOTE_STAT_FAILED")
	}
	if err != nil {
		return result, safeError(ctx.Err(), "NAS_PART_OPEN_FAILED")
	}
	partClosed := false
	defer func() {
		if !partClosed {
			_ = part.Close()
		}
	}()
	current, err := part.Stat()
	if err != nil || !current.Regular || current.Identity != state.RemoteIdentity || current.Size != openedSize {
		return result, errors.New("NAS_PART_CHANGED")
	}
	result.ResumedBytes = strconv.FormatInt(offset, 10)
	report("uploading", offset, count)
	if _, err = part.Seek(offset, io.SeekStart); err != nil {
		return result, safeError(ctx.Err(), "NAS_PART_SEEK_FAILED")
	}
	upload := &progressWriter{writer: part, count: offset, report: func(n int64) { report("uploading", n, count) }}
	written, err := io.CopyBuffer(writeOnly{upload}, contextReader{ctx, io.NewSectionReader(source, offset, count-offset)}, make([]byte, 64*1024))
	if err != nil || written != count-offset {
		return result, safeError(ctx.Err(), "NAS_UPLOAD_FAILED")
	}
	if err = part.Sync(); err != nil {
		return result, safeError(ctx.Err(), "NAS_UPLOAD_FLUSH_FAILED")
	}
	if err = part.Close(); err != nil {
		return result, safeError(ctx.Err(), "NAS_PART_CLOSE_FAILED")
	}
	partClosed = true
	report("verifying", 0, count)
	if err = e.verifyRemoteProgress(ctx, state.Part, count, digest, func(n int64) { report("verifying", n, count) }); err != nil {
		return result, err
	}
	after, err := source.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return result, errors.New("NAS_SOURCE_CHANGED")
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	report("finalizing", count, count)
	if err = e.remote.RenameNoReplace(ctx, state.Part, state.Final); err != nil {
		return result, safeError(ctx.Err(), "NAS_FINALIZE_CONFLICT")
	}
	if err = e.verifyRemote(ctx, state.Final, count, digest); err != nil {
		return result, err
	}
	state.Complete = true
	if err = e.save(key, state); err != nil {
		return result, err
	}
	result.Status = "complete"
	report("complete", count, count)
	return result, nil
}
