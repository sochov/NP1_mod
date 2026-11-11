package adapters

import (
	"encoding/json"
	stdlog "log"
	"os"
	"path/filepath"
	"time"
)

// Metadata only. Raw helper output stays in the same root-only operation dir.
type nativeAudioDiagnosticRecord struct {
	Stage     string    `json:"stage"`
	Code      string    `json:"code"`
	At        time.Time `json:"at"`
	ElapsedMS int64     `json:"elapsedMs"`
}
type nativeAudioOperation struct {
	root, dir, stage, code string
	started                time.Time
	change                 bool
	trace                  *os.File
}

func newNativeAudioOperation(root string, change bool) (*nativeAudioOperation, error) {
	kind := "get"
	if change {
		kind = "put"
	}
	dir, e := os.MkdirTemp(root, "audio-"+kind+"-")
	if e != nil {
		return nil, e
	}
	f, e := os.OpenFile(filepath.Join(dir, "stages.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return nil, e
	}
	d := &nativeAudioOperation{root: root, dir: dir, stage: "begin", started: time.Now(), change: change, trace: f}
	d.event("ENTER")
	return d, nil
}
func (d *nativeAudioOperation) record(code string) nativeAudioDiagnosticRecord {
	return nativeAudioDiagnosticRecord{Stage: d.stage, Code: code, At: time.Now().UTC(), ElapsedMS: time.Since(d.started).Milliseconds()}
}
func (d *nativeAudioOperation) event(code string) {
	b, _ := json.Marshal(d.record(code))
	d.trace.Write(append(b, '\n'))
}
func (d *nativeAudioOperation) enter(stage string)  { d.stage = stage; d.event("ENTER") }
func (d *nativeAudioOperation) failure(code string) { d.code = code; d.event(code) }
func (d *nativeAudioOperation) finish(result error) {
	defer d.trace.Close()
	if result == nil {
		d.event("OK")
		return
	}
	if d.code == "" {
		d.code = "OPERATION_FAILED"
	}
	d.event(d.code)
	name := "last-native-audio-read-failure.json"
	if d.change {
		name = "last-native-audio-change-failure.json"
	}
	b, _ := json.Marshal(d.record(d.code))
	os.WriteFile(filepath.Join(d.root, name), append(b, '\n'), 0600)
	stdlog.Printf("NativeAudio control failed stage=%s code=%s elapsed_ms=%d", d.stage, d.code, time.Since(d.started).Milliseconds())
}
