package adapters

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Only process-private singleton maps, never SDK global/shared application data.
// The native main holds this non-robust mutex for its entire process lifetime.
var stockServiceSyncNames = map[string]string{
	"drive-rec": "DriveRec", "image-recog": "ImageRecog", "voice": "Voice",
	"navigation": "Navigation", "communication": "Communication", "setting": "Setting",
	"update": "Update", "remote-monitoring": "RemoteMonitoring",
	"virtual-passenger": "VirtualPassenger", "info-mtg-engine": "InfoMtgEngine",
	"sit-awr-eng": "SitAwrEng", "opening": "Opening", "misc": "Misc", "system": "System",
	"app-manager": "AppManager", "system-monitor": "NPSystemMonitor",
}

type stockSyncIdentity struct{ major, minor, inode, links, uid uint64 }

const stockSyncBackupLimit = 4096

type stockSyncEnvironment struct {
	mapRoot, procRoot string
	stopped           func(context.Context, stockServiceDefinition) error
	identity          func(os.FileInfo) (stockSyncIdentity, error)
}

func retireStockServiceSync(ctx context.Context, def stockServiceDefinition) error {
	return retireStockServiceSyncAt(ctx, def, stockSyncEnvironment{
		mapRoot: "/tmp/victoria", procRoot: "/proc", identity: stockSyncIdentityFor,
		stopped: func(parent context.Context, d stockServiceDefinition) error {
			readCtx, cancel := context.WithTimeout(parent, 2*time.Second)
			defer cancel()
			out, err := exec.CommandContext(readCtx, "systemctl", "show", d.unit,
				"-p", "MainPID", "-p", "ActiveState", "-p", "SubState", "-p", "Job").Output()
			if err != nil {
				return errors.New("stock singleton unit state unavailable")
			}
			fields := map[string]string{}
			for _, line := range strings.Split(string(out), "\n") {
				if key, value, ok := strings.Cut(line, "="); ok {
					fields[key] = value
				}
			}
			if fields["MainPID"] != "0" || (fields["ActiveState"] != "inactive" && fields["ActiveState"] != "failed") || fields["Job"] != "" {
				return errors.New("stock singleton unit is not stopped")
			}
			return nil
		},
	})
}

func stockSyncHeader(data []byte) (uint32, error) {
	if len(data) != 128 || binary.LittleEndian.Uint64(data[:8]) != 0 ||
		(binary.LittleEndian.Uint32(data[16:20]) != 1 && binary.LittleEndian.Uint32(data[16:20]) != 2) ||
		binary.LittleEndian.Uint32(data[20:24]) != 1 || binary.LittleEndian.Uint32(data[24:28]) == 0 ||
		binary.LittleEndian.Uint32(data[28:32]) != 1 || binary.LittleEndian.Uint32(data[32:36]) != 129 ||
		binary.LittleEndian.Uint64(data[112:120]) != 1 {
		return 0, errors.New("unknown stock singleton map format")
	}
	return binary.LittleEndian.Uint32(data[24:28]), nil
}

func stockSyncOwnerDead(procRoot string, owner uint32) error {
	_, err := os.Stat(filepath.Join(procRoot, strconv.FormatUint(uint64(owner), 10)))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err == nil {
		return errors.New("stock singleton mutex owner is alive")
	}
	return errors.New("stock singleton owner state unavailable")
}

func stockSyncUnreferenced(ctx context.Context, env stockSyncEnvironment, path string, info os.FileInfo) error {
	want, err := env.identity(info)
	if err != nil {
		return err
	}
	processes, err := os.ReadDir(env.procRoot)
	if err != nil {
		return errors.New("process inventory unavailable")
	}
	for _, process := range processes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := strconv.ParseUint(process.Name(), 10, 32); err != nil {
			continue
		}
		base := filepath.Join(env.procRoot, process.Name())
		maps, readErr := os.ReadFile(filepath.Join(base, "maps"))
		if readErr != nil {
			if _, gone := os.Stat(base); errors.Is(gone, os.ErrNotExist) {
				continue
			}
			return errors.New("live process mappings unavailable")
		}
		for _, line := range strings.Split(string(maps), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			// /proc/maps file names may contain literal newlines; continuations
			// have no address range. The inode belongs to the first record line.
			start, end, ok := strings.Cut(fields[0], "-")
			if !ok {
				continue
			}
			if _, e := strconv.ParseUint(start, 16, 64); e != nil {
				continue
			}
			if _, e := strconv.ParseUint(end, 16, 64); e != nil {
				continue
			}
			if len(fields) < 5 {
				return errors.New("process mapping malformed")
			}
			major, minor, ok := strings.Cut(fields[3], ":")
			if !ok {
				return errors.New("process mapping device malformed")
			}
			ma, e1 := strconv.ParseUint(major, 16, 64)
			mi, e2 := strconv.ParseUint(minor, 16, 64)
			ino, e3 := strconv.ParseUint(fields[4], 10, 64)
			if e1 != nil || e2 != nil || e3 != nil {
				return errors.New("process mapping identity malformed")
			}
			if (ma == want.major && mi == want.minor && ino == want.inode) || strings.Contains(line, path) {
				return errors.New("stock singleton map is still mapped")
			}
		}
		fds, readErr := os.ReadDir(filepath.Join(base, "fd"))
		if readErr != nil {
			if _, gone := os.Stat(base); errors.Is(gone, os.ErrNotExist) {
				continue
			}
			return errors.New("live process descriptors unavailable")
		}
		for _, fd := range fds {
			if err := ctx.Err(); err != nil {
				return err
			}
			fdPath := filepath.Join(base, "fd", fd.Name())
			target, statErr := os.Stat(fdPath)
			if statErr != nil {
				if _, gone := os.Lstat(fdPath); errors.Is(gone, os.ErrNotExist) {
					continue
				}
				return errors.New("live process descriptor unavailable")
			}
			if os.SameFile(info, target) {
				return errors.New("stock singleton map has an open descriptor")
			}
		}
	}
	return nil
}

func readStockSyncFile(path string, identity func(os.FileInfo) (stockSyncIdentity, error)) (os.FileInfo, []byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != 128 {
		return nil, nil, errors.New("stock singleton path is not a regular 128-byte map")
	}
	id, err := identity(info)
	if err != nil || id.links != 1 {
		return nil, nil, errors.New("stock singleton identity or link count invalid")
	}
	file, err := os.OpenFile(path, os.O_RDONLY|apnEditorNoFollow(), 0)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, nil, errors.New("stock singleton changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, 129))
	if err != nil || len(data) != 128 {
		return nil, nil, errors.New("stock singleton read failed")
	}
	return info, data, nil
}

func retireStockServiceSyncAt(ctx context.Context, def stockServiceDefinition, env stockSyncEnvironment) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name, known := stockServiceSyncNames[def.id]
	if !known {
		return nil
	}
	unit := "app-@" + name + ".service"
	if def.id == "app-manager" {
		unit = "app-AppManager.service"
	}
	if def.id == "system-monitor" {
		unit = "NPSystemMonitor.service"
	}
	if def.unit != unit {
		return errors.New("stock singleton unit mismatch")
	}
	if err := env.stopped(ctx, def); err != nil {
		return err
	}
	rootInfo, err := os.Lstat(env.mapRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("stock singleton directory invalid")
	}
	path := filepath.Join(env.mapRoot, "victoria_"+name+".map")
	info, data, err := readStockSyncFile(path, env.identity)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	owner, err := stockSyncHeader(data)
	if err != nil {
		return err
	}
	if err = stockSyncOwnerDead(env.procRoot, owner); err != nil {
		return err
	}
	if err = stockSyncUnreferenced(ctx, env, path, info); err != nil {
		return err
	}
	backupRoot := filepath.Join(env.mapRoot, ".ncc-service-backups")
	if err = os.Mkdir(backupRoot, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	backupInfo, err := os.Lstat(backupRoot)
	if err != nil || !backupInfo.IsDir() || backupInfo.Mode()&os.ModeSymlink != 0 || !stockSyncPrivateMode(backupInfo) {
		return errors.New("stock singleton backup directory invalid")
	}
	backupID, err := env.identity(backupInfo)
	if err != nil || backupID.uid != 0 {
		return errors.New("stock singleton backup owner invalid")
	}
	entries, err := os.ReadDir(backupRoot)
	if err != nil || len(entries) >= stockSyncBackupLimit {
		return errors.New("stock singleton backup limit reached")
	}
	backup, err := os.MkdirTemp(backupRoot, name+"-")
	if err != nil {
		return err
	}
	// Only the newly created empty directory may be removed on a refusal.
	completed := false
	defer func() {
		if !completed {
			_ = os.Remove(filepath.Join(backup, "owner"))
			_ = os.Remove(backup)
		}
	}()
	marker := []byte("ncc-stock-singleton-v1\nunit=" + def.unit + "\n")
	if err = os.WriteFile(filepath.Join(backup, "owner"), marker, 0600); err != nil {
		return err
	}
	if err = env.stopped(ctx, def); err != nil {
		return err
	}
	if err = stockSyncOwnerDead(env.procRoot, owner); err != nil {
		return err
	}
	if err = stockSyncUnreferenced(ctx, env, path, info); err != nil {
		return err
	}
	current, currentData, err := readStockSyncFile(path, env.identity)
	if err != nil || !os.SameFile(info, current) || !bytes.Equal(data, currentData) {
		return errors.New("stock singleton changed before quarantine")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Rename(path, filepath.Join(backup, "singleton.map")); err != nil {
		return fmt.Errorf("stock singleton quarantine failed: %w", err)
	}
	completed = true
	return nil
}
