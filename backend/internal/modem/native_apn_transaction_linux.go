//go:build linux

package modem

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"time"
)

type privateNativeAPNNetwork struct {
	State        string   `json:"state"`
	SSID         string   `json:"ssid"`
	ID           string   `json:"networkID"`
	Address      string   `json:"address"`
	Prefix       string   `json:"prefix"`
	DefaultRoute []string `json:"defaultRoute"`
	Stock        string   `json:"stock"`
}
type privateNativeAPNBackup struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Capture       string                  `json:"capture"`
	ObservedAt    time.Time               `json:"observedAt"`
	Network       privateNativeAPNNetwork `json:"network"`
}

func lockNativeAPNTransaction() (func(), error) {
	root, e := profileRecoveryRoot(ProfileRecoveryDirectory, true, verifyProfileRecoveryOwner)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	file, e := root.OpenFile("native-apply.lock", unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	info, e := file.Stat()
	if e != nil || verifyProfileRecoveryOwner(info, 0600) != nil {
		file.Close()
		return nil, errors.New("APN_APPLY_LOCK_INVALID")
	}
	if unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
		file.Close()
		return nil, errors.New("APN_APPLY_IN_PROGRESS")
	}
	return func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN); file.Close() }, nil
}
func saveNativeAPNTransactionBackup(capture ProfileRecovery, network nativeAPNNetworkBaseline) error {
	summary, e := capture.Save()
	if e != nil {
		return e
	}
	verified, e := LoadProfileRecovery(summary.BackupFile)
	if e != nil || !sameNativeProfile1(capture.profiles[0], verified.profiles[0]) {
		return errors.New("APN_PRIVATE_BACKUP_VERIFY_FAILED")
	}
	root, e := profileRecoveryRoot(ProfileRecoveryDirectory, false, verifyProfileRecoveryOwner)
	if e != nil {
		return e
	}
	defer root.Close()
	return writeRecoveryMetadata(root, "native-transaction.json", privateNativeAPNBackup{1, summary.BackupFile, capture.observedAt, privateNativeAPNNetwork{network.wifi.state, network.wifi.ssid, network.wifi.id, network.wifi.address, network.prefix, network.defaultRoute, network.stock}})
}

// RecoverNativeAPNManagementRoute reads our fixed private transaction metadata
// and restores only a missing original default, never an APN or arbitrary route.
func RecoverNativeAPNManagementRoute(ctx context.Context) (string, error) {
	if e := requireProfileRecoveryRoot(); e != nil {
		return "unknown", e
	}
	root, e := profileRecoveryRoot(ProfileRecoveryDirectory, false, verifyProfileRecoveryOwner)
	if e != nil {
		return "unknown", e
	}
	defer root.Close()
	var backup privateNativeAPNBackup
	if e = readRecoveryMetadata(root, "native-transaction.json", &backup); e != nil {
		return "unknown", e
	}
	if backup.SchemaVersion != 1 || time.Since(backup.ObservedAt) > time.Hour || time.Until(backup.ObservedAt) > time.Minute {
		return "unknown", errors.New("APN_NETWORK_BACKUP_STALE")
	}
	if _, e = LoadProfileRecovery(backup.Capture); e != nil {
		return "unknown", e
	}
	wifi := nativeAPNWiFiState{backup.Network.State, backup.Network.SSID, backup.Network.ID, backup.Network.Address}
	route, e := parseNativeAPNDefault(stringsJoinRoute(backup.Network.DefaultRoute), wifi.address)
	if e != nil {
		return "unknown", e
	}
	return restoreNativeAPNDefault(ctx, nativeAPNNetworkBaseline{wifi, backup.Network.Prefix, route, backup.Network.Stock})
}
func stringsJoinRoute(route []string) string {
	var out string
	for i, part := range route {
		if i > 0 {
			out += " "
		}
		out += part
	}
	return out
}
