package adapters

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stockSyncFixture(t *testing.T) (stockServiceDefinition, stockSyncEnvironment, string) {
	t.Helper()
	root := t.TempDir()
	maps := filepath.Join(root, "maps")
	proc := filepath.Join(root, "proc")
	if err := os.MkdirAll(maps, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(proc, 0700); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 128)
	binary.LittleEndian.PutUint32(data[16:], 2)
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint32(data[24:], 123456)
	binary.LittleEndian.PutUint32(data[28:], 1)
	binary.LittleEndian.PutUint32(data[32:], 129)
	binary.LittleEndian.PutUint64(data[112:], 1)
	path := filepath.Join(maps, "victoria_Opening.map")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	def := stockServiceDefinition{id: "opening", unit: "app-@Opening.service"}
	env := stockSyncEnvironment{mapRoot: maps, procRoot: proc,
		identity: func(os.FileInfo) (stockSyncIdentity, error) {
			return stockSyncIdentity{major: 1, minor: 2, inode: 3, links: 1, uid: 0}, nil
		},
		stopped: func(context.Context, stockServiceDefinition) error { return nil },
	}
	return def, env, path
}

func TestStockSyncQuarantinePreservesOriginal(t *testing.T) {
	def, env, path := stockSyncFixture(t)
	before, _ := os.ReadFile(path)
	if err := retireStockServiceSyncAt(context.Background(), def, env); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("source still exists", err)
	}
	files, err := filepath.Glob(filepath.Join(env.mapRoot, ".ncc-service-backups", "Opening-*", "singleton.map"))
	if err != nil || len(files) != 1 {
		t.Fatal("missing private original", files, err)
	}
	after, _ := os.ReadFile(files[0])
	if string(before) != string(after) {
		t.Fatal("map contents changed")
	}
	marker, err := os.ReadFile(filepath.Join(filepath.Dir(files[0]), "owner"))
	if err != nil || !strings.Contains(string(marker), "unit=app-@Opening.service") {
		t.Fatal("missing ownership marker")
	}
	// A second start sees absence and does not create another backup.
	if err := retireStockServiceSyncAt(context.Background(), def, env); err != nil {
		t.Fatal(err)
	}
}

func TestStockSyncRejectsUnsafeStates(t *testing.T) {
	for _, name := range []string{"owner-alive", "mapped", "descriptor", "unreadable-process", "wrong-unit", "unit-active", "two-links", "wrong-format", "changed-final", "canceled", "backup-limit", "backup-owner"} {
		t.Run(name, func(t *testing.T) {
			def, env, path := stockSyncFixture(t)
			original, _ := os.ReadFile(path)
			ctx := context.Background()
			switch name {
			case "owner-alive":
				if err := os.Mkdir(filepath.Join(env.procRoot, "123456"), 0700); err != nil {
					t.Fatal(err)
				}
			case "mapped", "descriptor", "unreadable-process":
				base := filepath.Join(env.procRoot, "777")
				if err := os.MkdirAll(filepath.Join(base, "fd"), 0700); err != nil {
					t.Fatal(err)
				}
				if name != "unreadable-process" {
					text := ""
					if name == "mapped" {
						text = "1000-2000 rw-s 00000000 01:02 3 /irrelevant-alias\n"
					}
					if err := os.WriteFile(filepath.Join(base, "maps"), []byte(text), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if name == "descriptor" {
					if err := os.Link(path, filepath.Join(base, "fd", "9")); err != nil {
						t.Fatal(err)
					}
				}
			case "wrong-unit":
				def.unit = "app-@Voice.service"
			case "unit-active":
				env.stopped = func(context.Context, stockServiceDefinition) error { return errors.New("active") }
			case "two-links":
				env.identity = func(os.FileInfo) (stockSyncIdentity, error) { return stockSyncIdentity{links: 2}, nil }
			case "wrong-format":
				bad := append([]byte(nil), original...)
				bad[32] = 145 // robust/different mutex forbidden
				if err := os.WriteFile(path, bad, 0600); err != nil {
					t.Fatal(err)
				}
				original = bad
			case "changed-final":
				calls := 0
				env.stopped = func(context.Context, stockServiceDefinition) error {
					calls++
					if calls == 2 {
						changed := append([]byte(nil), original...)
						changed[90] = 1
						return os.WriteFile(path, changed, 0600)
					}
					return nil
				}
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "backup-limit":
				backup := filepath.Join(env.mapRoot, ".ncc-service-backups")
				if err := os.Mkdir(backup, 0700); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < stockSyncBackupLimit; i++ {
					if err := os.WriteFile(filepath.Join(backup, fmt.Sprintf("fixture-%04d", i)), nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "backup-owner":
				env.identity = func(info os.FileInfo) (stockSyncIdentity, error) {
					id := stockSyncIdentity{links: 1}
					if info.IsDir() {
						id.uid = 1000
					}
					return id, nil
				}
			}
			if err := retireStockServiceSyncAt(ctx, def, env); err == nil {
				t.Fatal("unsafe quarantine accepted")
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("original removed on refusal", err)
			}
		})
	}
}

func TestStockSyncAbsentAndNonAppDoNotTouchGlobalMaps(t *testing.T) {
	def, env, path := stockSyncFixture(t)
	def.id = "qmmf"
	if err := retireStockServiceSyncAt(context.Background(), def, env); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	def.id = "opening"
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := retireStockServiceSyncAt(context.Background(), def, env); err != nil {
		t.Fatal(err)
	}
}

func TestStockSyncManagerAndMonitorUseFixedUnits(t *testing.T) {
	for _, candidate := range []struct{ id, unit, name string }{
		{"app-manager", "app-AppManager.service", "AppManager"},
		{"system-monitor", "NPSystemMonitor.service", "NPSystemMonitor"},
	} {
		t.Run(candidate.id, func(t *testing.T) {
			def, env, path := stockSyncFixture(t)
			def.id, def.unit = candidate.id, candidate.unit
			target := filepath.Join(env.mapRoot, "victoria_"+candidate.name+".map")
			if err := os.Rename(path, target); err != nil {
				t.Fatal(err)
			}
			if err := retireStockServiceSyncAt(context.Background(), def, env); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("fixed singleton not retired", err)
			}
		})
	}
}

func TestStockSyncMappingFilenameNewlineIsNotARecord(t *testing.T) {
	def, env, _ := stockSyncFixture(t)
	base := filepath.Join(env.procRoot, "777")
	if err := os.MkdirAll(filepath.Join(base, "fd"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "maps"), []byte("1000-2000 r-xp 00000000 01:02 999 /name\nwith-newline\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := retireStockServiceSyncAt(context.Background(), def, env); err != nil {
		t.Fatal(err)
	}
}
