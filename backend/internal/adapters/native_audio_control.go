package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/np1-control-center/ncc/internal/model"
)

const nativeAudioHelper = "/run/np1-control-native/audio-helper"
const nativeAudioHash = "3775648059142273549bebb1aa15806b25b9ee2cfb888747862e0f5df1f51156"

var errNativeAudioConflict = errors.New("NATIVE_AUDIO_CONFLICT")

type NativeAudioData struct {
	MasterVolume int64 `json:"masterVolume"`
	Mute         struct {
		Value   int64 `json:"value"`
		Enabled bool  `json:"enabled"`
	} `json:"mute"`
	Correction struct {
		Value int64  `json:"value"`
		Label string `json:"label"`
	} `json:"correction"`
	Writable bool   `json:"writable"`
	Reason   string `json:"reason,omitempty"`
}
type nativeAudioValue struct{ volume, mute, correction int64 }
type nativeAudioPut struct {
	id            string
	ExpectedValue int64 `json:"expectedValue"`
	Value         int64 `json:"value"`
}

func nativeAudioSpec(id string) (index int, key uint32, maximum int64, known bool) {
	switch id {
	case "mute":
		return 1, 466, 1, true
	case "volume-correction":
		return 22, 468, 2, true
	default:
		return 0, 0, 0, false
	}
}
func nativeAudioObservation(value nativeAudioValue, writable bool, reason string) model.Observation {
	if !nativeAudioValid(value) {
		return model.Missing("純正音声設定", "現在値の形式を確認できません")
	}
	data := NativeAudioData{MasterVolume: value.volume, Writable: writable, Reason: reason}
	data.Mute.Value, data.Mute.Enabled = value.mute, value.mute == 0
	data.Correction.Value = value.correction
	data.Correction.Label = []string{"OFF", "弱", "強"}[value.correction]
	return model.Value("純正Setting CommonFreeStyle 0x400の照合済み応答", data)
}
func nativeAudioValid(value nativeAudioValue) bool {
	return value.volume >= 0 && value.volume <= 2147483647 && value.mute >= 0 && value.mute <= 1 && value.correction >= 0 && value.correction <= 2
}

func parseNativeAudioPut(w http.ResponseWriter, r *http.Request, id string) (nativeAudioPut, error) {
	_, _, maximum, known := nativeAudioSpec(id)
	if !known {
		return nativeAudioPut{}, errors.New("unknown audio setting")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.UseNumber()
	open, err := decoder.Token()
	if err != nil || open != json.Delim('{') {
		return nativeAudioPut{}, errors.New("object required")
	}
	values := map[string]int64{}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || (key != "expectedValue" && key != "value") {
			return nativeAudioPut{}, errors.New("unknown audio property")
		}
		if _, exists := values[key]; exists {
			return nativeAudioPut{}, errors.New("duplicate audio property")
		}
		var raw any
		if err := decoder.Decode(&raw); err != nil {
			return nativeAudioPut{}, err
		}
		number, ok := raw.(json.Number)
		if !ok {
			return nativeAudioPut{}, errors.New("integer required")
		}
		value, err := number.Int64()
		if err != nil || value < 0 || value > maximum {
			return nativeAudioPut{}, errors.New("audio value outside proven range")
		}
		values[key] = value
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') || len(values) != 2 {
		return nativeAudioPut{}, errors.New("both audio values required")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nativeAudioPut{}, errors.New("trailing audio data")
	}
	return nativeAudioPut{id: id, ExpectedValue: values["expectedValue"], Value: values["value"]}, nil
}

func handleNativeAudio(w http.ResponseWriter, r *http.Request, action func(*nativeAudioPut) (model.Observation, error)) {
	var request *nativeAudioPut
	if r.Method == http.MethodPut {
		id := r.PathValue("id")
		if _, _, _, known := nativeAudioSpec(id); !known {
			nativeError(w, 404, "UNKNOWN_NATIVE_AUDIO_SETTING", "未対応の音声設定です")
			return
		}
		value, err := parseNativeAudioPut(w, r, id)
		if err != nil {
			nativeError(w, 400, "INVALID_NATIVE_AUDIO_SETTING", "設定値の形式または範囲が不正です")
			return
		}
		request = &value
	} else if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, PUT")
		nativeError(w, 405, "METHOD_NOT_ALLOWED", "GETまたはPUTが必要です")
		return
	}
	result, err := action(request)
	if err != nil {
		if errors.Is(err, errNativeAudioConflict) {
			nativeError(w, 409, "NATIVE_AUDIO_CONFLICT", "設定が変更されたか、別の操作が進行中です。状態を更新してください。")
			return
		}
		nativeError(w, 503, "NATIVE_AUDIO_UNAVAILABLE", "純正音声設定の現在値または保存を確認できません。状態を更新してください。")
		return
	}
	recordingJSON(w, 200, result)
}
func HandleNativeAudioRemote(w http.ResponseWriter, r *http.Request) {
	handleNativeAudio(w, r, nativeAudioRemote)
}
func HandleNativeAudioLocal(w http.ResponseWriter, r *http.Request) {
	handleNativeAudio(w, r, nativeAudioLocal)
}

func nativeAudioRemote(request *nativeAudioPut) (model.Observation, error) {
	path, method := "/audio-settings", http.MethodGet
	var body io.Reader
	if request != nil {
		if _, _, _, known := nativeAudioSpec(request.id); !known {
			return model.Observation{}, errors.New("unknown audio id")
		}
		path += "/" + request.id
		method = http.MethodPut
		data, _ := json.Marshal(request)
		body = bytes.NewReader(data)
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", NativeControlSocket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	req, _ := http.NewRequest(method, "http://native"+path, body)
	reply, err := client.Do(req)
	if err != nil {
		return model.Observation{}, err
	}
	defer reply.Body.Close()
	if reply.StatusCode != 200 {
		var failure struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(reply.Body, 4096)).Decode(&failure)
		if failure.Code == "NATIVE_AUDIO_CONFLICT" {
			return model.Observation{}, errNativeAudioConflict
		}
		return model.Observation{}, errors.New("audio worker unavailable")
	}
	var observed model.Observation
	if json.NewDecoder(io.LimitReader(reply.Body, 16384)).Decode(&observed) != nil || observed.Status != "available" {
		return model.Observation{}, errors.New("invalid audio observation")
	}
	return observed, nil
}

func parseNativeAudioOutput(data []byte) (nativeAudioValue, error) {
	if len(data) > 65536 {
		return nativeAudioValue{}, errors.New("oversized audio output")
	}
	values := map[string]int64{}
	cleanup := false
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "cleanup=matched; own-receiver-removed" {
			if cleanup {
				return nativeAudioValue{}, errors.New("duplicate audio cleanup")
			}
			cleanup = true
			continue
		}
		key, raw, ok := strings.Cut(line, "=")
		if !ok || (key != "audio-volume" && key != "audio-mute" && key != "audio-correction") {
			continue
		}
		if _, duplicate := values[key]; duplicate {
			return nativeAudioValue{}, errors.New("duplicate audio value")
		}
		value, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || strconv.FormatInt(value, 10) != raw {
			return nativeAudioValue{}, errors.New("invalid audio integer")
		}
		values[key] = value
	}
	value := nativeAudioValue{values["audio-volume"], values["audio-mute"], values["audio-correction"]}
	if len(values) != 3 || !cleanup || !nativeAudioValid(value) {
		return nativeAudioValue{}, errors.New("incomplete or unknown audio result")
	}
	return value, nil
}

type nativeAudioOwner = nativeSettingIdentity

func nativeAudioSettingOwner() (nativeAudioOwner, error) {
	return nativeSettingOwnerState()
}
func runNativeAudioHelper(owner nativeAudioOwner, diagnostic *nativeAudioOperation, phase string, args ...string) (nativeAudioValue, error) {
	diagnostic.enter(phase)
	if !nativeFirmwareMatches(nativeAudioHelper, nativeAudioHash) {
		diagnostic.failure("HELPER_FINGERPRINT_MISMATCH")
		return nativeAudioValue{}, errors.New("audio helper fingerprint mismatch")
	}
	stderr, err := os.OpenFile(diagnostic.dir+"/"+phase+"-error.log", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nativeAudioValue{}, err
	}
	defer stderr.Close()
	stdout, err := os.OpenFile(diagnostic.dir+"/"+phase+"-result.txt", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nativeAudioValue{}, err
	}
	defer stdout.Close()
	cmd := exec.Command(nativeAudioHelper, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LD_LIBRARY_PATH=/usr/local/lib", "NCC_NATIVE_AUDIO_TRIAL=1"}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	// Never context-kill a child while it may own vendor locks.
	if err = cmd.Run(); err != nil {
		diagnostic.failure("HELPER_EXEC_FAILED")
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 75 {
			return nativeAudioValue{}, errNativeAudioConflict
		}
		return nativeAudioValue{}, err
	}
	if _, err = stdout.Seek(0, 0); err != nil {
		return nativeAudioValue{}, err
	}
	data, err := io.ReadAll(io.LimitReader(stdout, 65537))
	if err != nil {
		return nativeAudioValue{}, err
	}
	after, err := nativeAudioSettingOwner()
	if err != nil || after != owner {
		diagnostic.failure("SETTING_PROCESS_CHANGED")
		return nativeAudioValue{}, errors.New("Setting process changed")
	}
	value, err := parseNativeAudioOutput(data)
	if err != nil {
		diagnostic.failure("HELPER_RESULT_INVALID")
	}
	return value, err
}
func savedNativeAudioValue(key uint32, maximum int64) (int64, error) {
	file, err := os.Open(nativeBackup)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 705))
	if err != nil {
		return 0, err
	}
	return parseNativeBackupValue(data, key, maximum)
}
func nativeAudioStored(value nativeAudioValue) bool {
	mute, e1 := savedNativeAudioValue(466, 1)
	correction, e2 := savedNativeAudioValue(468, 2)
	return e1 == nil && e2 == nil && mute == value.mute && correction == value.correction
}

func nativeAudioLocal(request *nativeAudioPut) (result model.Observation, resultError error) {
	if !nativeControlMu.TryLock() {
		return model.Observation{}, errNativeAudioConflict
	}
	defer nativeControlMu.Unlock()
	release, err := lockNativeControl()
	if err != nil {
		return model.Observation{}, errNativeAudioConflict
	}
	defer release()
	diagnostic, err := newNativeAudioOperation("/run/np1-control-native", request != nil)
	if err != nil {
		return model.Observation{}, err
	}
	defer func() { diagnostic.finish(resultError) }()
	owner, err := nativeAudioSettingOwner()
	if err != nil {
		return model.Observation{}, err
	}
	if request == nil {
		value, err := runNativeAudioHelper(owner, diagnostic, "read-current", "--get", owner.pid)
		if err != nil {
			return model.Observation{}, err
		}
		stored := nativeAudioStored(value)
		reason := ""
		if !stored {
			reason = "保存状態を確認できないため、変更できません。状態を更新してください。"
		}
		return nativeAudioObservation(value, stored, reason), nil
	}
	index, key, maximum, known := nativeAudioSpec(request.id)
	if !known || request.ExpectedValue < 0 || request.ExpectedValue > maximum || request.Value < 0 || request.Value > maximum {
		return model.Observation{}, errors.New("unsupported audio setting")
	}
	values, err := readNativeValues(nativeStockFiles)
	saved, diskErr := savedNativeAudioValue(key, maximum)
	if err != nil || diskErr != nil || int64(values[index]) != request.ExpectedValue || saved != request.ExpectedValue {
		diagnostic.failure("BEFORE_STORAGE_CONFLICT")
		return model.Observation{}, errNativeAudioConflict
	}
	value, err := runNativeAudioHelper(owner, diagnostic, "apply", "--apply", owner.pid, request.id, strconv.FormatInt(request.ExpectedValue, 10), strconv.FormatInt(request.Value, 10))
	if err != nil {
		return model.Observation{}, err
	}
	actual := value.mute
	if index == 22 {
		actual = value.correction
	}
	if actual != request.Value {
		return model.Observation{}, errors.New("audio apply unconfirmed")
	}
	diagnostic.enter("persistent-readback")
	until := time.Now().Add(7 * time.Second)
	for time.Now().Before(until) {
		values, err = readNativeValues(nativeStockFiles)
		saved, diskErr = savedNativeAudioValue(key, maximum)
		if err == nil && diskErr == nil && int64(values[index]) == request.Value && saved == request.Value {
			final, err := runNativeAudioHelper(owner, diagnostic, "final-live-get", "--get", owner.pid)
			if err != nil {
				return model.Observation{}, err
			}
			if final != value || !nativeAudioStored(final) {
				return model.Observation{}, errors.New("final audio read-back changed")
			}
			return nativeAudioObservation(final, true, ""), nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	diagnostic.failure("PERSISTENT_SAVE_TIMEOUT")
	return model.Observation{}, errors.New("audio save unconfirmed")
}
