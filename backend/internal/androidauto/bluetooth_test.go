package androidauto

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/np1-control-center/ncc/internal/androidauto/stockbt"
)

func TestBTStoreKeepsKeysPrivate(t *testing.T) {
	dir := t.TempDir()
	s := New(Config{DataDir: dir, StateDir: dir})
	a, _ := stockbt.ParseAddr("64:49:7D:E0:DB:42")
	var k stockbt.LinkKey
	for i := range k.Key {
		k.Key[i] = byte(0xa0 + i)
	}
	k.Type = 4
	if err := s.bt.remember(a, k); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, btStoreFile))
	if err != nil || info.Mode().Perm() != 0o600 && os.PathSeparator == '/' {
		t.Fatalf("store file %v %v", info, err)
	}
	// A new service instance reads the head unit back.
	s2 := New(Config{DataDir: dir, StateDir: dir})
	st := s2.bt.status()
	if len(st.HeadUnits) != 1 || st.HeadUnits[0].Address != "64:49:7D:E0:DB:42" {
		t.Fatalf("head units %+v", st.HeadUnits)
	}
	b, _ := json.Marshal(st)
	if strings.Contains(string(b), "a0a1a2") {
		t.Fatalf("status exposes the link key: %s", b)
	}
	got, key, ok := parseStored(s2.bt.store[0])
	if !ok || got != a || key != k.Key {
		t.Fatalf("stored %v %x", got, key)
	}
	if err = s2.bt.removeStored(a.String()); err != nil || len(s2.bt.status().HeadUnits) != 0 {
		t.Fatalf("remove: %v", err)
	}
	if err = s2.bt.removeStored(a.String()); err == nil {
		t.Fatal("removed an unknown head unit")
	}
}
