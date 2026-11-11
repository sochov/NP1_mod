package adapters

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/np1-control-center/ncc/internal/modem"
)

const APNFieldReadyPath = APNDirectory + "/field-ready.json"
const APNWorkerProtocolVersion = 1

type apnFieldReceipt struct {
	Version       int       `json:"version"`
	Protocol      string    `json:"protocol"`
	VerifiedAt    time.Time `json:"verifiedAt"`
	DriverVersion string    `json:"driverVersion"`
	ModuleSHA256  string    `json:"moduleSHA256"`
	Profile1Only  bool      `json:"profile1Only"`
}

// This receipt is created only by a separate reproducible field-validation
// action, never a page GET, worker startup, saved editor change, or unit enable.
func APNFieldReady() bool { return apnFieldReadyAt(APNFieldReadyPath, time.Now().UTC()) }
func apnFieldReadyAt(path string, now time.Time) bool {
	if apnEditorDirectory(filepath.Dir(path), false) != nil {
		return false
	}
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 1024 || apnEditorOwner(info) != nil {
		return false
	}
	if runtime.GOOS == "linux" && info.Mode().Perm() != 0600 {
		return false
	}
	root, e := os.OpenRoot(filepath.Dir(path))
	if e != nil {
		return false
	}
	defer root.Close()
	f, e := root.Open(filepath.Base(path))
	if e != nil {
		return false
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(opened, info) {
		return false
	}
	raw, e := io.ReadAll(io.LimitReader(f, 1025))
	if e != nil || len(raw) > 1024 {
		return false
	}
	var receipt apnFieldReceipt
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&receipt) != nil || d.Decode(new(any)) != io.EOF {
		return false
	}
	return receipt.Version == 1 && receipt.Protocol == "np1-native-apn-profile1-v1" && receipt.Profile1Only && receipt.DriverVersion == modem.GobiNetDriverVersion && receipt.ModuleSHA256 == modem.GobiNetModuleSHA256 && !receipt.VerifiedAt.IsZero() && !receipt.VerifiedAt.After(now.Add(time.Minute))
}
