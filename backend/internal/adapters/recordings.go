package adapters

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/model"
)

const recordingDirectory = "DRIVERECDATA/Video"

var recordingDirectories = []struct{ path, kind string }{{recordingDirectory, "normal"}, {"DRIVERECDATA/Event", "event"}, {"DRIVERECDATA/Parking", "parking"}}

var recordingFilename = regexp.MustCompile(`(?i)^FILE([ME])_([0-9]{6})_([0-9]{6})_[0-9]+\.MP4$`)
var recordingZone = time.FixedZone("JST", 9*3600)

type RecordingClip struct {
	ID        string    `json:"id"`
	Camera    string    `json:"camera"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	StartAt   time.Time `json:"startAt"`
	EndAt     time.Time `json:"endAt"`
	SizeBytes string    `json:"sizeBytes"`
	MP4Metadata
}
type RecordingDay struct {
	Date            string  `json:"date"`
	Count           int     `json:"count"`
	FrontCount      int     `json:"frontCount"`
	CabinCount      int     `json:"cabinCount"`
	RecordedSeconds float64 `json:"recordedSeconds"`
}
type RecordingCatalog struct {
	Clips           []RecordingClip `json:"clips"`
	Days            []RecordingDay  `json:"days"`
	PendingFiles    int             `json:"pendingFiles"`
	UnreadableFiles int             `json:"unreadableFiles"`
	Truncated       bool            `json:"truncated"`
	Timezone        string          `json:"timezone"`
	IndexedAt       time.Time       `json:"indexedAt"`
}
type recordingCachedFile struct {
	size     int64
	modified time.Time
	clip     RecordingClip
}
type RecordingIndex struct {
	mu       sync.Mutex
	root     string
	mount    func() (string, error)
	token    string
	at       time.Time
	catalog  RecordingCatalog
	files    map[string]recordingCachedFile
	activity recordingActivity
}

func NewRecordingIndex(root string, mount func() (string, error)) *RecordingIndex {
	return &RecordingIndex{root: root, mount: mount, files: map[string]recordingCachedFile{}}
}
func recordingTokenID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8])
}
func recordingID(token, relative string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(recordingTokenID(token) + "|" + relative))
}
func parseRecordingFilename(name string) (string, time.Time, bool) {
	m := recordingFilename.FindStringSubmatch(name)
	if m == nil {
		return "", time.Time{}, false
	}
	start, e := time.ParseInLocation("20060102_150405", "20"+m[2]+"_"+m[3], recordingZone)
	if e != nil {
		return "", time.Time{}, false
	}
	camera := "front"
	if strings.EqualFold(m[1], "E") {
		camera = "in"
	}
	return camera, start.UTC(), true
}
func (index *RecordingIndex) Catalog(ctx context.Context, date string) (RecordingCatalog, error) {
	index.mu.Lock()
	defer index.mu.Unlock()
	token, e := index.mount()
	if e != nil {
		index.catalog = RecordingCatalog{}
		index.at = time.Time{}
		index.files = map[string]recordingCachedFile{}
		return RecordingCatalog{}, e
	}
	if token == "" {
		return RecordingCatalog{}, errors.New("SD identity unavailable")
	}
	if token != index.token {
		index.files = map[string]recordingCachedFile{}
		index.at = time.Time{}
	}
	if time.Since(index.at) > 15*time.Second {
		catalog, files, e := index.scan(ctx, token)
		if e != nil {
			return RecordingCatalog{}, e
		}
		after, e := index.mount()
		if e != nil || after != token {
			index.at = time.Time{}
			return RecordingCatalog{}, errors.New("SD_CHANGED")
		}
		index.catalog = catalog
		index.files = files
		index.token = token
		index.at = time.Now()
	}
	result := index.catalog
	if date != "" {
		start, e := time.ParseInLocation("2006-01-02", date, recordingZone)
		if e != nil || start.Format("2006-01-02") != date {
			return RecordingCatalog{}, errors.New("INVALID_DATE")
		}
		end := start.AddDate(0, 0, 1)
		clips := []RecordingClip{}
		for _, clip := range result.Clips {
			if clip.StartAt.Before(end) && clip.EndAt.After(start) {
				clips = append(clips, clip)
			}
		}
		result.Clips = clips
	}
	return result, nil
}
func (index *RecordingIndex) scan(ctx context.Context, token string) (RecordingCatalog, map[string]recordingCachedFile, error) {
	out := RecordingCatalog{Clips: []RecordingClip{}, Days: []RecordingDay{}, Timezone: "Asia/Tokyo", IndexedAt: time.Now().UTC()}
	files := map[string]recordingCachedFile{}
	for _, source := range recordingDirectories {
		root, dir, e := openContained(index.root, source.path)
		if e != nil {
			if errors.Is(e, os.ErrNotExist) {
				continue
			}
			return out, nil, e
		}
		entries, e := dir.ReadDir(20001)
		dir.Close()
		root.Close()
		if e != nil && e != io.EOF {
			return out, nil, e
		}
		if len(entries) > 20000 {
			entries = entries[:20000]
			out.Truncated = true
		}
		for n, entry := range entries {
			if n%32 == 0 {
				if e = ctx.Err(); e != nil {
					return out, nil, e
				}
			}
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			camera, start, ok := parseRecordingFilename(entry.Name())
			if !ok {
				continue
			}
			st, e := entry.Info()
			if e != nil || !st.Mode().IsRegular() {
				continue
			}
			relative := path.Join(source.path, entry.Name())
			if cached, ok := index.files[relative]; ok && cached.size == st.Size() && cached.modified.Equal(st.ModTime()) {
				out.Clips = append(out.Clips, cached.clip)
				files[relative] = cached
				continue
			}
			if time.Since(st.ModTime()) < 2*time.Second {
				out.PendingFiles++
				continue
			}
			container, stream, e := openContained(index.root, relative)
			if e != nil {
				out.UnreadableFiles++
				continue
			}
			metadata, parseErr := ReadRecordingMetadata(stream, st.Size())
			after, statErr := stream.Stat()
			stream.Close()
			container.Close()
			if parseErr != nil || statErr != nil || after.Size() != st.Size() || !after.ModTime().Equal(st.ModTime()) {
				if time.Since(st.ModTime()) < 30*time.Second {
					out.PendingFiles++
				} else {
					out.UnreadableFiles++
				}
				continue
			}
			clip := RecordingClip{ID: recordingID(token, relative), Camera: camera, Kind: source.kind, Name: entry.Name(), StartAt: start, EndAt: start.Add(time.Duration(metadata.DurationSeconds * float64(time.Second))), SizeBytes: strconv.FormatInt(st.Size(), 10), MP4Metadata: metadata}
			out.Clips = append(out.Clips, clip)
			files[relative] = recordingCachedFile{st.Size(), st.ModTime(), clip}
		}
	}
	sort.Slice(out.Clips, func(i, j int) bool {
		if out.Clips[i].StartAt.Equal(out.Clips[j].StartAt) {
			return out.Clips[i].Camera < out.Clips[j].Camera
		}
		return out.Clips[i].StartAt.Before(out.Clips[j].StartAt)
	})
	out.Days = recordingDays(out.Clips)
	return out, files, nil
}
func recordingDays(clips []RecordingClip) []RecordingDay {
	type interval struct{ start, end time.Time }
	days := map[string]*RecordingDay{}
	intervals := map[string][]interval{}
	for _, clip := range clips {
		day := time.Date(clip.StartAt.In(recordingZone).Year(), clip.StartAt.In(recordingZone).Month(), clip.StartAt.In(recordingZone).Day(), 0, 0, 0, 0, recordingZone)
		for day.Before(clip.EndAt) {
			next := day.AddDate(0, 0, 1)
			key := day.Format("2006-01-02")
			item := days[key]
			if item == nil {
				item = &RecordingDay{Date: key}
				days[key] = item
			}
			item.Count++
			if clip.Camera == "front" {
				item.FrontCount++
			} else {
				item.CabinCount++
			}
			start, end := clip.StartAt, clip.EndAt
			if start.Before(day) {
				start = day
			}
			if end.After(next) {
				end = next
			}
			intervals[key] = append(intervals[key], interval{start, end})
			day = next
		}
	}
	result := []RecordingDay{}
	for day, item := range days {
		spans := intervals[day]
		sort.Slice(spans, func(i, j int) bool { return spans[i].start.Before(spans[j].start) })
		if len(spans) > 0 {
			a, b := spans[0].start, spans[0].end
			for _, span := range spans[1:] {
				if !span.start.After(b) {
					if span.end.After(b) {
						b = span.end
					}
				} else {
					item.RecordedSeconds += b.Sub(a).Seconds()
					a, b = span.start, span.end
				}
			}
			item.RecordedSeconds += b.Sub(a).Seconds()
		}
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date > result[j].Date })
	return result
}

var recordings = NewRecordingIndex(SDRoot, func() (string, error) {
	if SDStatus().Status != "available" {
		return "", errors.New("NO_SD")
	}
	return recordingMountToken(SDRoot)
})

func recordingJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func recordingError(w http.ResponseWriter, e error) {
	status := 503
	code := "RECORDINGS_UNAVAILABLE"
	if e.Error() == "INVALID_DATE" {
		status = 400
		code = "INVALID_DATE"
	}
	// The stock loop recorder deletes the oldest clips when the card fills up.
	if errors.Is(e, os.ErrNotExist) {
		recordingJSON(w, 404, map[string]any{"status": "error", "code": "RECORDING_GONE", "reason": "この録画はループ録画で上書きされ、本体に残っていません"})
		return
	}
	recordingJSON(w, status, map[string]any{"status": "error", "code": code, "reason": "録画情報を取得できません: " + e.Error()})
}
func HandleRecordings(w http.ResponseWriter, r *http.Request) {
	catalog, e := recordings.Catalog(r.Context(), r.URL.Query().Get("date"))
	if e != nil {
		recordingError(w, e)
		return
	}
	recordingJSON(w, 200, model.Value("SD finalized MP4 metadata", catalog))
}
func HandleRecordingDays(w http.ResponseWriter, r *http.Request) {
	catalog, e := recordings.Catalog(r.Context(), "")
	if e != nil {
		recordingError(w, e)
		return
	}
	recordingJSON(w, 200, model.Value("SD finalized MP4 metadata", catalog.Days))
}
func recordingPath(id, token string) (string, error) {
	if len(id) > 1024 {
		return "", errors.New("invalid recording ID")
	}
	raw, e := base64.RawURLEncoding.DecodeString(id)
	if e != nil {
		return "", e
	}
	fingerprint, relative, ok := strings.Cut(string(raw), "|")
	validDirectory := false
	for _, source := range recordingDirectories {
		if path.Dir(relative) == source.path {
			validDirectory = true
		}
	}
	if !ok || fingerprint != recordingTokenID(token) || !validDirectory || path.Clean(relative) != relative || strings.ContainsAny(relative, "\\\x00") {
		return "", errors.New("SD_CHANGED or invalid recording ID")
	}
	if _, _, ok := parseRecordingFilename(path.Base(relative)); !ok {
		return "", errors.New("invalid recording name")
	}
	return relative, nil
}
func HandleRecordingStream(w http.ResponseWriter, r *http.Request)   { serveRecording(w, r, false) }
func HandleRecordingDownload(w http.ResponseWriter, r *http.Request) { serveRecording(w, r, true) }
func serveRecording(w http.ResponseWriter, r *http.Request, download bool) {
	if SDStatus().Status != "available" {
		recordingError(w, errors.New("NO_SD"))
		return
	}
	token, e := recordingMountToken(SDRoot)
	if e != nil {
		recordingError(w, e)
		return
	}
	relative, e := recordingPath(r.PathValue("id"), token)
	if e != nil {
		recordingJSON(w, 409, map[string]string{"status": "error", "code": "SD_CHANGED", "reason": "SDカードまたは録画が変わりました。録画一覧を更新してください"})
		return
	}
	root, f, e := OpenSD(relative)
	if e != nil {
		recordingError(w, e)
		return
	}
	defer root.Close()
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		recordingError(w, e)
		return
	}
	if _, e = ReadRecordingMetadata(f, st.Size()); e != nil {
		recordingJSON(w, 409, map[string]string{"status": "error", "code": "RECORDING_NOT_FINALIZED", "reason": "録画はまだ確定していません"})
		return
	}
	q := url.Values{}
	if download {
		q.Set("download", "1")
	}
	r.URL.RawQuery = q.Encode()
	serveOpenedFile(w, r, f)
}

// RecordingSummary supplies high-level availability without claiming service-active means recording.
func RecordingSummary(ctx context.Context) model.Observation {
	catalog, e := recordings.Catalog(ctx, "")
	if e != nil {
		return model.Missing("SD recording index", e.Error())
	}
	activity, activityError := recordings.Activity(ctx, time.Now())
	result := map[string]any{"finalizedClips": len(catalog.Clips), "pendingFiles": catalog.PendingFiles, "unreadableFiles": catalog.UnreadableFiles, "indexedAt": catalog.IndexedAt, "writing": activity.Writing, "cameras": activity.Cameras, "writingObservedAt": activity.ObservedAt}
	if activityError != nil {
		result["writing"] = nil
		result["writingReason"] = "録画ファイルの更新を確認できません"
	}
	return model.Value("SD MP4 metadata and observed file updates", result)
}
