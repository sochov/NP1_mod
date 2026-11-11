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

const alexaHelper = "/run/np1-control-native/alexa-helper"
const alexaHelperHash = "83519cc1562148414da244a616056c7f1ad8a13f5e83dc1a088058226afb8c45"

// Enable only after the pinned Voice change/readback/persistent-544 roundtrip.
const alexaWritesVerified = true

var errAlexaConflict = errors.New("ALEXA_CONFLICT")
var errAlexaFixed = errors.New("ALEXA_FIXED_BY_LINKAGE")

type alexaPut struct {
	ExpectedValue int64 `json:"expectedValue"`
	Enabled       bool  `json:"enabled"`
}
type AlexaSettingsData struct {
	WakeWord AlexaWakeWordState `json:"wakeWord"`
	Linkage  map[string]string  `json:"linkage"`
	Writable bool               `json:"writable"`
	Reason   string             `json:"reason,omitempty"`
}

func alexaObservation(value int) model.Observation {
	word := AlexaWakeWordState{Value: value, Enabled: value != 1, Fixed: value == 2}
	status := "not-fixed"
	reason := ""
	if word.Fixed {
		status = "guard-active"
		reason = "Alexaの純正連携保護が有効なため変更できません"
	} else if !alexaWritesVerified {
		reason = "設定変更は実機で検証中です"
	}
	return model.Value("純正Voice CommonFreeStyle 0x0b00の照合済み応答", AlexaSettingsData{WakeWord: word, Linkage: map[string]string{"status": status, "source": "純正Voiceのウェイクワード応答。アカウント認証や現在のBluetooth接続を示すものではありません"}, Writable: alexaWritesVerified && !word.Fixed, Reason: reason})
}
func parseAlexaPut(w http.ResponseWriter, r *http.Request) (alexaPut, error) {
	var raw struct {
		ExpectedValue json.RawMessage `json:"expectedValue"`
		Enabled       *bool           `json:"enabled"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&raw) != nil || raw.Enabled == nil {
		return alexaPut{}, errors.New("invalid Alexa request")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return alexaPut{}, errors.New("invalid Alexa request")
	}
	d := json.NewDecoder(bytes.NewReader(raw.ExpectedValue))
	d.UseNumber()
	var value any
	if d.Decode(&value) != nil {
		return alexaPut{}, errors.New("expected enum required")
	}
	n, ok := value.(json.Number)
	if !ok {
		return alexaPut{}, errors.New("expected enum required")
	}
	before, e := n.Int64()
	if e != nil || before < 0 || before > 2 {
		return alexaPut{}, errors.New("expected enum out of range")
	}
	return alexaPut{before, *raw.Enabled}, nil
}
func handleAlexa(w http.ResponseWriter, r *http.Request, action func(*alexaPut) (model.Observation, error)) {
	var request *alexaPut
	if r.Method == http.MethodPut {
		p, e := parseAlexaPut(w, r)
		if e != nil {
			nativeError(w, 400, "INVALID_ALEXA_SETTING", "Alexa設定の形式が不正です")
			return
		}
		request = &p
	} else if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, PUT")
		nativeError(w, 405, "METHOD_NOT_ALLOWED", "GETまたはPUTが必要です")
		return
	}
	value, e := action(request)
	if e != nil {
		code, status, reason := "ALEXA_UNAVAILABLE", 503, "Alexaの設定を取得・変更できません。状態を更新してください"
		if errors.Is(e, errAlexaConflict) {
			code, status, reason = "ALEXA_CONFLICT", 409, "別の操作でAlexa設定が変わりました。状態を更新してください"
		}
		if errors.Is(e, errAlexaFixed) {
			code, status, reason = "ALEXA_FIXED_BY_LINKAGE", 409, "Alexaの純正連携保護が有効なため変更できません"
		}
		nativeError(w, status, code, reason)
		return
	}
	recordingJSON(w, 200, value)
}

// Distinct public/private handlers prevent broker/socket proxy recursion.
func HandleAlexaRemote(w http.ResponseWriter, r *http.Request) { handleAlexa(w, r, alexaRemote) }
func HandleAlexaLocal(w http.ResponseWriter, r *http.Request)  { handleAlexa(w, r, alexaLocal) }
func alexaRemote(request *alexaPut) (model.Observation, error) {
	method := http.MethodGet
	var body io.Reader
	if request != nil {
		method = http.MethodPut
		b, _ := json.Marshal(request)
		body = bytes.NewReader(b)
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", NativeControlSocket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 25 * time.Second}
	req, _ := http.NewRequest(method, "http://native/alexa-settings", body)
	response, e := client.Do(req)
	if e != nil {
		return model.Observation{}, e
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		var problem struct {
			Code string `json:"code"`
		}
		json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&problem)
		if problem.Code == "ALEXA_CONFLICT" {
			return model.Observation{}, errAlexaConflict
		}
		if problem.Code == "ALEXA_FIXED_BY_LINKAGE" {
			return model.Observation{}, errAlexaFixed
		}
		return model.Observation{}, errors.New("Alexa worker unavailable")
	}
	var result model.Observation
	e = json.NewDecoder(io.LimitReader(response.Body, 16384)).Decode(&result)
	if e != nil || result.Status != "available" {
		return model.Observation{}, errors.New("invalid Alexa worker observation")
	}
	return result, nil
}
func parseAlexaHelperOutput(data []byte) (int, error) {
	if len(data) > 65536 {
		return 0, errors.New("oversized helper output")
	}
	found, cleanup := false, false
	value := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "alexa-wire=") {
			if found {
				return 0, errors.New("duplicate Alexa value")
			}
			raw := strings.TrimPrefix(line, "alexa-wire=")
			if raw != "0" && raw != "1" && raw != "2" {
				return 0, errors.New("invalid Alexa value")
			}
			value = int(raw[0] - '0')
			found = true
		}
		if line == "cleanup=matched; own-receiver-removed" {
			cleanup = true
		}
	}
	if !found || !cleanup {
		return 0, errors.New("uncorrelated or incomplete helper result")
	}
	return value, nil
}
func alexaVoicePID() (string, error) {
	value, e := command("systemctl", "show", "app-@Voice.service", "-p", "MainPID", "--value")
	if e != nil {
		return "", e
	}
	pid, e := strconv.ParseInt(value, 10, 32)
	if e != nil || pid <= 0 {
		return "", errors.New("Voice PID unavailable")
	}
	return strconv.FormatInt(pid, 10), nil
}
func runAlexaHelper(voicePID string, diagnostic *alexaOperation, phase string, args ...string) (int, error) {
	diagnostic.enter(phase)
	if !nativeFirmwareMatches(alexaHelper, alexaHelperHash) {
		diagnostic.failure("HELPER_FINGERPRINT_MISMATCH")
		return 0, errors.New("Alexa helper fingerprint mismatch")
	}
	log, e := os.OpenFile(diagnostic.dir+"/"+phase+"-error.log", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return 0, e
	}
	defer log.Close()
	out, e := os.OpenFile(diagnostic.dir+"/"+phase+"-result.txt", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if e != nil {
		return 0, e
	}
	defer out.Close()
	cmd := exec.Command(alexaHelper, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LD_LIBRARY_PATH=/usr/local/lib", "NCC_NATIVE_ALEXA_TRIAL=1"}
	cmd.Stdout = out
	cmd.Stderr = log
	// No context kill: the helper releases owned vendor mutexes itself.
	if e = cmd.Run(); e != nil {
		code := "HELPER_EXEC_FAILED"
		var failure *exec.ExitError
		if errors.As(e, &failure) {
			code = "HELPER_EXIT_" + strconv.Itoa(failure.ExitCode())
		}
		diagnostic.failure(code)
		return 0, e
	}
	diagnostic.event("HELPER_DONE")
	if _, e = out.Seek(0, 0); e != nil {
		return 0, e
	}
	b, e := io.ReadAll(io.LimitReader(out, 65537))
	if e != nil {
		return 0, e
	}
	after, e := alexaVoicePID()
	if e != nil || after != voicePID {
		diagnostic.failure("VOICE_PROCESS_CHANGED")
		return 0, errors.New("Voice process changed")
	}
	value, e := parseAlexaHelperOutput(b)
	if e != nil {
		diagnostic.failure("HELPER_RESULT_INVALID")
	}
	return value, e
}
func savedAlexaValue() (int64, error) {
	f, e := os.Open(nativeBackup)
	if e != nil {
		return 0, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 705))
	if e != nil {
		return 0, e
	}
	return parseNativeBackupValue(b, 544, 1)
}
func alexaLocal(request *alexaPut) (observation model.Observation, resultError error) {
	if !nativeControlMu.TryLock() {
		return model.Observation{}, errAlexaConflict
	}
	defer nativeControlMu.Unlock()
	release, e := lockNativeControl()
	if e != nil {
		return model.Observation{}, errAlexaConflict
	}
	defer release()
	diagnostic, e := newAlexaOperation("/run/np1-control-native", request != nil)
	if e != nil {
		return model.Observation{}, e
	}
	defer func() { diagnostic.finish(resultError) }()
	diagnostic.enter("voice-pid")
	pid, e := alexaVoicePID()
	if e != nil {
		return model.Observation{}, e
	}
	if request == nil {
		before, e := runAlexaHelper(pid, diagnostic, "read-current", "--get", pid)
		if e != nil {
			return model.Observation{}, e
		}
		return alexaObservation(before), nil
	}
	before := int(request.ExpectedValue)
	if before == 2 {
		actual, e := runAlexaHelper(pid, diagnostic, "check-fixed", "--get", pid)
		if e != nil {
			return model.Observation{}, e
		}
		diagnostic.failure("EXPECTED_VALUE_NOT_WRITABLE")
		if actual == 2 {
			return model.Observation{}, errAlexaFixed
		}
		return model.Observation{}, errAlexaConflict
	}
	if !alexaWritesVerified {
		return model.Observation{}, errors.New("Alexa writes are not field verified")
	}
	next := 1
	if request.Enabled {
		next = 0
	}
	if before == next {
		actual, e := runAlexaHelper(pid, diagnostic, "confirm-unchanged", "--get", pid)
		if e != nil {
			return model.Observation{}, e
		}
		if actual == 2 {
			return model.Observation{}, errAlexaFixed
		}
		if actual != before {
			return model.Observation{}, errAlexaConflict
		}
		return alexaObservation(actual), nil
	}
	// --apply itself performs a fresh correlated Voice GET before Set, so an
	// extra process here repeats all firmware hashes under the CPU quota.
	diagnostic.enter("preflight-storage")
	values, e := readNativeValues(nativeStockFiles)
	saved, diskError := savedAlexaValue()
	if e != nil || diskError != nil || int(values[85]) != before || saved != int64(before) {
		diagnostic.failure("BEFORE_STORAGE_CONFLICT")
		return model.Observation{}, errAlexaConflict
	}
	result, e := runAlexaHelper(pid, diagnostic, "apply", "--apply", pid, strconv.Itoa(before), strconv.Itoa(next))
	if e != nil || result != next {
		return model.Observation{}, errors.New("Alexa change unconfirmed")
	}
	diagnostic.enter("persistent-readback")
	deadline := time.Now().Add(7 * time.Second)
	for time.Now().Before(deadline) {
		values, e = readNativeValues(nativeStockFiles)
		saved, diskError = savedAlexaValue()
		if e == nil && diskError == nil && int(values[85]) == next && saved == int64(next) {
			diagnostic.event("PERSISTENT_MATCHED")
			actual, e := runAlexaHelper(pid, diagnostic, "final-live-get", "--get", pid)
			if e != nil || actual != next {
				return model.Observation{}, errors.New("Alexa final live state changed")
			}
			return alexaObservation(actual), nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	diagnostic.failure("PERSISTENT_SAVE_TIMEOUT")
	return model.Observation{}, errors.New("Alexa persistent save unconfirmed")
}
