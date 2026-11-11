package adapters

import (
	"encoding/json"
	stdlog "log"
	"os"
	"path/filepath"
	"time"
)

// Metadata only. Raw helper output stays in the same root-only operation dir.
type alexaDiagnosticRecord struct {
	Stage     string    `json:"stage"`
	Code      string    `json:"code"`
	At        time.Time `json:"at"`
	ElapsedMS int64     `json:"elapsedMs"`
}
type alexaOperation struct {
	root, dir, stage, code string
	started                time.Time
	change                 bool
	trace                  *os.File
}

func newAlexaOperation(root string, change bool) (*alexaOperation, error) {
	kind := "get"
	if change {
		kind = "put"
	}
	dir, e := os.MkdirTemp(root, "alexa-"+kind+"-")
	if e != nil {
		return nil, e
	}
	f, e := os.OpenFile(filepath.Join(dir, "stages.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return nil, e
	}
	d := &alexaOperation{root: root, dir: dir, stage: "begin", started: time.Now(), change: change, trace: f}
	d.event("ENTER")
	return d, nil
}
func (d *alexaOperation) record(code string) alexaDiagnosticRecord {
	return alexaDiagnosticRecord{Stage: d.stage, Code: code, At: time.Now().UTC(), ElapsedMS: time.Since(d.started).Milliseconds()}
}
func (d *alexaOperation) event(code string) {
	b, _ := json.Marshal(d.record(code))
	d.trace.Write(append(b, '\n'))
}
func (d *alexaOperation) enter(stage string)  { d.stage = stage; d.event("ENTER") }
func (d *alexaOperation) failure(code string) { d.code = code; d.event(code) }
func (d *alexaOperation) finish(result error) {
	defer d.trace.Close()
	if result == nil {
		d.event("OK")
		return
	}
	if d.code == "" {
		d.code = "OPERATION_FAILED"
	}
	d.event(d.code)
	name := "last-alexa-read-failure.json"
	if d.change {
		name = "last-alexa-change-failure.json"
	}
	b, _ := json.Marshal(d.record(d.code))
	os.WriteFile(filepath.Join(d.root, name), append(b, '\n'), 0600)
	stdlog.Printf("Alexa control failed stage=%s code=%s elapsed_ms=%d", d.stage, d.code, time.Since(d.started).Milliseconds())
}
