package adapters

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

const NativeControlSocket = "/run/np1-control-native/control.sock"
const nativeHelper = "/run/np1-control-native/helper"
const nativeHelperHash = "07bcc5263d4838a81fc806d9e4698829173bb3970057427e27b3ed6f5cd83ab7"
const nativeBackup = "/pio_data/SYSTEM/SETTING/Setting.DAT"

var nativeControlMu sync.Mutex

func nativeControlReady() bool {
	info, e := os.Lstat(NativeControlSocket)
	return e == nil && info.Mode()&os.ModeSocket != 0
}

func nativeApplyRemote(id string, before, next int64) error {
	body, _ := json.Marshal(map[string]int64{"expectedValue": before, "value": next})
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", NativeControlSocket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 20 * time.Second}
	req, _ := http.NewRequest("PUT", "http://native/native-settings/"+id, bytes.NewReader(body))
	response, e := client.Do(req)
	if e != nil {
		return errors.New("純正設定の変更サービスに接続できません")
	}
	defer response.Body.Close()
	io.Copy(io.Discard, io.LimitReader(response.Body, 65536))
	if response.StatusCode != 200 {
		return errors.New("変更後の保存を確認できません。状態を更新してください")
	}
	return nil
}

// The native worker is a separate privilege boundary. Only the experimentally
// verified setting is exposed; requests cannot choose a library, path or command.
func HandleNativeControl(w http.ResponseWriter, r *http.Request) {
	handleNativeSettingWithApply(w, r, NativeSettings, applyNativeVerified)
}

func parseNativeBackup(data []byte) (int64, error) {
	return parseNativeBackupValue(data, 507, 2)
}
func parseNativeBackupValue(data []byte, persistentID uint32, maximum int64) (int64, error) {
	if len(data) != 704 || binary.LittleEndian.Uint32(data) != 0 || binary.LittleEndian.Uint32(data[4:]) != 16 || binary.LittleEndian.Uint32(data[12:]) != 86 {
		return 0, errors.New("unsupported setting backup")
	}
	var previous uint32
	found := false
	var result int64
	for offset := 16; offset < len(data); offset += 8 {
		key := binary.LittleEndian.Uint32(data[offset:])
		if key <= previous {
			return 0, errors.New("unordered setting backup")
		}
		previous = key
		if key == persistentID {
			result = int64(int32(binary.LittleEndian.Uint32(data[offset+4:])))
			found = true
		}
	}
	if !found || result < 0 || result > maximum {
		return 0, errors.New("unknown sensitivity backup")
	}
	return result, nil
}

func savedNativeValue(index int, maximum int64) (int64, error) {
	f, e := os.Open(nativeBackup)
	if e != nil {
		return 0, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 705))
	if e != nil {
		return 0, e
	}
	persistentID := uint32(507)
	if index == 50 {
		persistentID = 506
	}
	return parseNativeBackupValue(b, persistentID, maximum)
}

func applyNativeVerified(id string, before, next int64) error {
	index, maximum := 51, int64(2)
	if id == "tailgating-detection" {
		index, maximum = 50, 1
	} else if id != "tailgating-sensitivity" {
		return errors.New("未対応の設定です")
	}
	if before < 0 || before > maximum || next < 0 || next > maximum {
		return errors.New("未対応の設定です")
	}
	if !nativeControlMu.TryLock() {
		return errors.New("別の設定変更を確認中です")
	}
	defer nativeControlMu.Unlock()
	release, e := lockNativeControl()
	if e != nil {
		return e
	}
	defer release()
	if !nativeFirmwareMatches(nativeHelper, nativeHelperHash) {
		return errors.New("設定ヘルパーの検証に失敗しました")
	}
	values, e := readNativeValues(nativeStockFiles)
	if e != nil || int64(values[index]) != before {
		return errors.New("現在値が変わりました。状態を更新してください")
	}
	saved, e := savedNativeValue(index, maximum)
	if e != nil || saved != before {
		return errors.New("純正設定の保存処理が完了していません。再度お試しください")
	}
	if before == next {
		return nil
	}
	// Do not kill a process while it owns a vendor mutex. Vendor mutex waits are
	// bounded in the pinned helper, which releases mappings before its raw exit.
	cmd := exec.Command(nativeHelper, "--apply", id, strconv.FormatInt(before, 10), strconv.FormatInt(next, 10))
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LD_LIBRARY_PATH=/usr/local/lib", "NCC_NATIVE_TRIAL=1"}
	cmd.Stdout = io.Discard
	diagnostic, diagnosticError := os.OpenFile("/run/np1-control-native/last-helper-error.log", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if diagnosticError != nil {
		return errors.New("設定変更の診断記録を保存できません")
	}
	defer diagnostic.Close()
	cmd.Stderr = diagnostic
	if e = cmd.Run(); e != nil {
		return errors.New("純正設定の変更結果を確認できません。状態を更新してください")
	}
	deadline := time.Now().Add(7 * time.Second)
	for time.Now().Before(deadline) {
		values, e = readNativeValues(nativeStockFiles)
		saved, diskError := savedNativeValue(index, maximum)
		if e == nil && int64(values[index]) == next && diskError == nil && saved == next {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("設定の永続保存が未確認です。現在値を更新してください")
}
