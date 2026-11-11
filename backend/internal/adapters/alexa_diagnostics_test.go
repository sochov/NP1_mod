package adapters

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAlexaChangeFailureSurvivesSuccessfulGET(t *testing.T) {
	root := t.TempDir()
	put, e := newAlexaOperation(root, true)
	if e != nil {
		t.Fatal(e)
	}
	put.enter("apply")
	put.failure("HELPER_EXIT_78")
	put.finish(errors.New("raw private error must not be serialized"))
	first, e := os.ReadFile(filepath.Join(root, "last-alexa-change-failure.json"))
	if e != nil {
		t.Fatal(e)
	}
	get, e := newAlexaOperation(root, false)
	if e != nil {
		t.Fatal(e)
	}
	get.enter("read-current")
	get.finish(nil)
	after, e := os.ReadFile(filepath.Join(root, "last-alexa-change-failure.json"))
	if e != nil || string(after) != string(first) {
		t.Fatal("successful GET erased first PUT failure")
	}
	if strings.Contains(string(after), "raw private") || !strings.Contains(string(after), "HELPER_EXIT_78") {
		t.Fatal("diagnostic leaked raw error or lost code")
	}
	trace, e := os.ReadFile(filepath.Join(put.dir, "stages.jsonl"))
	if e != nil || !strings.Contains(string(trace), "elapsedMs") || !strings.Contains(string(trace), "apply") {
		t.Fatal("stage timings missing")
	}
}
