package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/model"
)

// The request selects an opaque ID, never an arbitrary systemd unit or command.
// Start/stop affects the current boot only; unit files and enablement are untouched.
type stockServiceDefinition struct{ id, unit, label, description, impact string }

var stockServiceDefinitions = []stockServiceDefinition{
	{"drive-rec", "app-@DriveRec.service", "ドライブレコーダー", "純正の録画処理", "停止中は録画されません。"},
	{"image-recog", "app-@ImageRecog.service", "映像認識", "カメラ映像の認識・画像出力", "ライブ映像と運転支援に影響します。"},
	{"voice", "app-@Voice.service", "音声・Alexa", "純正の音声対話・案内", "音声案内とAlexaに影響します。"},
	{"navigation", "app-@Navigation.service", "ナビゲーション", "純正の経路案内", "経路案内に影響します。"},
	{"communication", "app-@Communication.service", "通信制御", "純正アプリの通信・接続管理", "4G・Wi-Fiと純正設定の操作が途切れる場合があります。"},
	{"setting", "app-@Setting.service", "純正設定", "純正アプリの設定保持・反映", "停止中は純正設定の変更を反映できません。"},
	{"update", "app-@Update.service", "ソフトウェア更新", "純正ファームウェア更新", "自動更新の許可設定とは別の起動状態です。更新処理中の停止は避けてください。"},
	{"remote-monitoring", "app-@RemoteMonitoring.service", "リモート監視", "純正リモート監視アプリ", "純正リモート監視に影響します。"},
	{"virtual-passenger", "app-@VirtualPassenger.service", "バーチャルパッセンジャー", "純正の対話・同乗者機能", "純正の対話機能に影響します。"},
	{"info-mtg-engine", "app-@InfoMtgEngine.service", "情報案内エンジン", "InfoMtgEngineアプリ", "関連する純正情報案内に影響します。"},
	{"sit-awr-eng", "app-@SitAwrEng.service", "状況認識エンジン", "SitAwrEngアプリ", "関連する純正認識機能に影響します。"},
	{"opening", "app-@Opening.service", "起動案内", "Openingアプリ", "純正の起動時案内に影響します。"},
	{"misc", "app-@Misc.service", "その他の純正機能", "Miscアプリ", "関連する純正機能に影響します。"},
	{"system", "app-@System.service", "純正システム", "純正アプリ群のシステム制御", "複数の純正機能に影響する場合があります。"},
	{"app-manager", "app-AppManager.service", "アプリ管理", "純正アプリの管理プロセス", "複数の純正アプリの管理に影響します。"},
	{"qmmf", "qmmf-server.service", "カメラ基盤", "カメラ・映像処理サーバー", "録画・カメラ映像に影響します。"},
	{"event-av", "NEventWatcherAV.service", "映像・音声イベント", "純正AVイベント処理", "純正の映像・音声機能に影響します。"},
	{"event-com", "NEventWatcherCom.service", "通信イベント", "モデム・通信イベント処理", "4G・Wi-Fiと遠隔接続が途切れる場合があります。"},
	{"event-its", "NEventWatcherITS.service", "ITSイベント", "純正ITSイベント処理", "関連する運転支援機能に影響します。"},
	{"event-sys", "NEventWatcherSys.service", "システムイベント", "純正システムイベント処理", "停止時は本体ハートビートも停止します。起動時はハートビートも起動してください。"},
	{"qcmap", "QCMAP_ConnectionManagerd.service", "純正ネットワーク基盤", "QCMAPの接続管理", "4G・Wi-Fiと遠隔接続が途切れる場合があります。"},
	{"location-launcher", "loc_launcher.service", "測位プロセス管理", "純正GPSプロセスの起動管理", "GPS測位と走行記録に影響します。"},
	{"location-hal", "location_hal_daemon.service", "測位基盤", "純正の位置情報サービス", "GPS測位と走行記録に影響します。"},
	{"pulseaudio", "pulseaudio.service", "音声出力基盤", "純正PulseAudioサーバー", "音声案内とWebからの音声出力に影響します。"},
	{"system-monitor", "NPSystemMonitor.service", "純正システム監視", "純正プロセス・負荷の監視", "純正の状態監視に影響します。"},
	{"socket-server", "socketserver.service", "本体ハートビート", "純正本体監視のハートビート", "停止すると本体の監視と自動復旧に影響します。"},
}

var stockInvocationPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

type StockService struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Label         string `json:"label"`
	Description   string `json:"description"`
	Impact        string `json:"impact"`
	State         string `json:"state"`
	SubState      string `json:"subState"`
	MainPID       uint64 `json:"mainPID"`
	InvocationID  string `json:"invocationID"`
	LoadState     string `json:"loadState"`
	UnitFileState string `json:"unitFileState"`
	CanStart      bool   `json:"canStart"`
	CanStop       bool   `json:"canStop"`
	CanRestart    bool   `json:"canRestart"`
	ControlReason string `json:"controlReason,omitempty"`
	Job           string `json:"-"`
	ProcessKnown  bool   `json:"-"`
}

type stockServiceIO struct {
	run          func(context.Context, ...string) (string, error)
	prepareStart func(context.Context, stockServiceDefinition) error
	now          func() time.Time
	sleep        func(context.Context, time.Duration) error
}

func stockServiceRuntime() stockServiceIO {
	return stockServiceIO{run: func(parent context.Context, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(parent, 2*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "systemctl", args...)
		cmd.WaitDelay = 250 * time.Millisecond
		out, err := cmd.Output()
		return string(out), err
	}, prepareStart: retireStockServiceSync, now: time.Now, sleep: func(ctx context.Context, delay time.Duration) error {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}}
}

func stockServiceDefinitionFor(id string) (stockServiceDefinition, bool) {
	for _, def := range stockServiceDefinitions {
		if def.id == id {
			return def, true
		}
	}
	return stockServiceDefinition{}, false
}

func readStockServices(ctx context.Context, defs []stockServiceDefinition, d stockServiceIO) ([]StockService, error) {
	args := []string{"show", "--no-pager", "--property=Id,LoadState,ActiveState,SubState,MainPID,InvocationID,UnitFileState,CanStart,CanStop,RefuseManualStart,RefuseManualStop,Job", "--"}
	for _, def := range defs {
		args = append(args, def.unit)
	}
	out, err := d.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	properties := map[string]map[string]string{}
	// Id may occur after other properties. Empty lines delimit each unit.
	for _, block := range strings.Split(strings.ReplaceAll(out, "\r", ""), "\n\n") {
		v := map[string]string{}
		for _, line := range strings.Split(block, "\n") {
			if key, value, ok := strings.Cut(line, "="); ok {
				v[key] = value
			}
		}
		if v["Id"] != "" {
			properties[v["Id"]] = v
		}
	}
	services := make([]StockService, 0, len(defs))
	for _, def := range defs {
		v, ok := properties[def.unit]
		if !ok || v["LoadState"] == "" || v["ActiveState"] == "" {
			return nil, fmt.Errorf("incomplete service observation")
		}
		pid, pidErr := strconv.ParseUint(v["MainPID"], 10, 64)
		stable := (v["ActiveState"] == "active" || v["ActiveState"] == "inactive" || v["ActiveState"] == "failed") && v["Job"] == ""
		loaded := v["LoadState"] == "loaded"
		metadata := pidErr == nil && (v["InvocationID"] == "" || stockInvocationPattern.MatchString(v["InvocationID"]))
		if v["ActiveState"] == "active" && (pid == 0 || v["InvocationID"] == "") {
			metadata = false
		}
		if (v["ActiveState"] == "inactive" || v["ActiveState"] == "failed") && pid != 0 {
			metadata = false
		}
		stable = stable && metadata
		canStart := loaded && stable && v["CanStart"] == "yes" && v["RefuseManualStart"] != "yes"
		canStop := loaded && stable && v["CanStop"] == "yes" && v["RefuseManualStop"] != "yes"
		reason := ""
		if !loaded {
			reason = "この本体ではサービスが利用できません。"
		} else if !metadata {
			reason = "プロセス情報を確認できないため操作できません。"
		}
		services = append(services, StockService{ID: def.id, Name: def.unit, Label: def.label, Description: def.description, Impact: def.impact, State: v["ActiveState"], SubState: v["SubState"], MainPID: pid, InvocationID: v["InvocationID"], LoadState: v["LoadState"], UnitFileState: v["UnitFileState"], CanStart: canStart, CanStop: canStop, CanRestart: canStart && canStop, ControlReason: reason, Job: v["Job"], ProcessKnown: metadata})
	}
	return services, nil
}

func StockServices(ctx context.Context) model.Observation {
	services, err := readStockServices(ctx, stockServiceDefinitions, stockServiceRuntime())
	if err != nil {
		return model.Missing("systemctl show (fixed stock units)", "純正サービスの状態を取得できません")
	}
	guardStockServiceDependencies(services)
	value := model.Value("systemctl show (fixed stock units)", services)
	value.RefreshSeconds = 5
	return value
}

type stockServiceRequest struct {
	Action               string `json:"action"`
	ExpectedInvocationID string `json:"expectedInvocationID"`
	ExpectedState        string `json:"expectedState"`
}

type stockServiceResult struct {
	Status     string       `json:"status"`
	Action     string       `json:"action"`
	Previous   StockService `json:"previous"`
	Service    StockService `json:"service"`
	ObservedAt time.Time    `json:"observedAt"`
}

type stockServiceProblem struct {
	status       int
	code, reason string
}

func (p *stockServiceProblem) Error() string { return p.reason }

func controlStockService(ctx context.Context, id string, body stockServiceRequest, d stockServiceIO) (stockServiceResult, error) {
	problem := func(status int, code, reason string) (stockServiceResult, error) {
		return stockServiceResult{}, &stockServiceProblem{status, code, reason}
	}
	def, ok := stockServiceDefinitionFor(id)
	if !ok {
		return problem(404, "UNKNOWN_STOCK_SERVICE", "対象の純正サービスはありません")
	}
	if body.Action != "start" && body.Action != "stop" && body.Action != "restart" {
		return problem(400, "INVALID_SERVICE_ACTION", "起動・停止・再起動のいずれかを指定してください")
	}
	if body.ExpectedState != "active" && body.ExpectedState != "inactive" && body.ExpectedState != "failed" {
		return problem(400, "INVALID_SERVICE_STATE", "最新のサービス状態を取得してください")
	}
	before, err := readStockServices(ctx, []stockServiceDefinition{def}, d)
	if err != nil {
		return problem(503, "SERVICE_UNAVAILABLE", "純正サービスの状態を取得できません")
	}
	previous := before[0]
	if previous.State != body.ExpectedState || previous.InvocationID != body.ExpectedInvocationID {
		return problem(409, "SERVICE_STATE_CHANGED", "サービスの状態が変わりました。最新の状態を取得してください")
	}
	allowed := map[string]bool{"start": previous.CanStart, "stop": previous.CanStop, "restart": previous.CanRestart}
	if !allowed[body.Action] {
		return problem(409, "SERVICE_NOT_CONTROLLABLE", "現在の状態ではこの操作を行えません")
	}
	if err := verifyStockServiceDependencies(ctx, def, body.Action, d); err != nil {
		return stockServiceResult{}, err
	}
	var heartbeat *StockService
	if id == "event-sys" && body.Action == "restart" {
		peer, _ := stockServiceDefinitionFor("socket-server")
		observed, err := readStockServices(ctx, []stockServiceDefinition{peer}, d)
		if err != nil {
			return problem(503, "SERVICE_UNAVAILABLE", "本体ハートビートの状態を取得できません")
		}
		if observed[0].State == "active" {
			heartbeat = &observed[0]
		}
	}
	// The stock AppManager and apps have mutual Requires=. A normal stop/restart
	// would therefore stop other apps; a normal start could revive apps the owner
	// deliberately stopped. Suppress dependency jobs for every individual action.
	// This does not edit the dependency graph or startup enablement.
	action := body.Action
	if action == "restart" {
		action = "stop"
	}
	if action == "start" && previous.State != "active" && d.prepareStart != nil {
		if err = d.prepareStart(ctx, def); err != nil {
			log.Printf("stock-service id=%s action=%s singleton-preparation: %v", def.id, body.Action, err)
			return problem(409, "SERVICE_START_RESOURCE_BUSY", "純正アプリの起動用同期資源を準備できませんでした。別のプロセスが使用中か、状態が未確認です")
		}
	}
	if _, err = d.run(ctx, "--no-block", "--job-mode=ignore-dependencies", action, "--", def.unit); err != nil {
		return problem(503, "SERVICE_COMMAND_FAILED", "サービスの操作に失敗しました。最新の状態を確認してください")
	}
	if body.Action == "restart" {
		// Release only a proven dead process's private singleton lock between stop
		// and start. The SDK's non-robust lifetime mutex otherwise survives TERM.
		if _, err = waitStockService(ctx, def, "stop", previous, d); err != nil {
			return stockServiceResult{}, err
		}
		if d.prepareStart != nil {
			if err = d.prepareStart(ctx, def); err != nil {
				log.Printf("stock-service id=%s action=%s singleton-preparation: %v", def.id, body.Action, err)
				return problem(409, "SERVICE_START_RESOURCE_BUSY", "停止後の純正アプリの同期資源を準備できませんでした。最新の状態を確認してください")
			}
		}
		if _, err = d.run(ctx, "--no-block", "--job-mode=ignore-dependencies", "start", "--", def.unit); err != nil {
			return problem(503, "SERVICE_COMMAND_FAILED", "停止後の起動に失敗しました。最新の状態を確認してください")
		}
	}
	current, err := waitStockService(ctx, def, body.Action, previous, d)
	if err != nil {
		return stockServiceResult{}, err
	}
	if heartbeat != nil {
		// NEventWatcherSys's native ExecStop explicitly stops socketserver. Its
		// restart must also restore a heartbeat that was running beforehand.
		peer, _ := stockServiceDefinitionFor("socket-server")
		if _, err = d.run(ctx, "--no-block", "--job-mode=ignore-dependencies", "start", "--", peer.unit); err != nil {
			return problem(503, "SERVICE_PEER_NOT_RESTORED", "システムイベントは起動しましたが、本体ハートビートを再開できませんでした")
		}
		if _, err = waitStockService(ctx, peer, "start", *heartbeat, d); err != nil {
			return problem(503, "SERVICE_PEER_NOT_RESTORED", "システムイベントは起動しましたが、本体ハートビートの再開を確認できません")
		}
	}
	return stockServiceResult{"applied", body.Action, previous, current, d.now().UTC()}, nil
}

func waitStockService(ctx context.Context, def stockServiceDefinition, action string, previous StockService, d stockServiceIO) (StockService, error) {
	problem := func(status int, code, reason string) (StockService, error) {
		return StockService{}, &stockServiceProblem{status, code, reason}
	}
	until := d.now().Add(12 * time.Second)
	stable := 0
	var last StockService
	for d.now().Before(until) && ctx.Err() == nil {
		after, err := readStockServices(ctx, []stockServiceDefinition{def}, d)
		if err == nil {
			current := after[0]
			matches := current.Job == "" && current.LoadState == "loaded"
			if action == "stop" {
				matches = matches && current.State == "inactive" && current.MainPID == 0
			} else {
				matches = matches && current.State == "active" && current.MainPID > 0 && current.InvocationID != ""
				if action == "restart" {
					matches = matches && current.InvocationID != previous.InvocationID
				}
			}
			if matches {
				if current.State == last.State && current.InvocationID == last.InvocationID && current.MainPID == last.MainPID {
					stable++
				} else {
					stable = 1
				}
				// Type=simple reports active before vendor initialization finishes.
				// Observe three seconds of the same live invocation, or one second
				// of inactive/PID0 for a stop; a momentary start is not success.
				required := 13
				if action == "stop" {
					required = 5
				}
				if stable >= required {
					return current, nil
				}
			} else {
				stable = 0
			}
			last = current
			if current.State == "failed" && current.Job == "" {
				return problem(503, "SERVICE_FAILED", "サービスが失敗状態になりました。状態を確認して再度起動してください")
			}
		} else {
			stable = 0
			last = StockService{}
		}
		if d.sleep(ctx, 250*time.Millisecond) != nil {
			break
		}
	}
	return problem(503, "SERVICE_NOT_CONFIRMED", "操作後の状態をまだ確認できません。処理は継続している場合があります。最新の状態を取得してください")
}

var stockServiceMu sync.Mutex

func RegisterStockServiceHandlers(mux *http.ServeMux, invalidate func()) {
	mux.HandleFunc("GET /system/services", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(StockServices(r.Context()))
	})
	mux.HandleFunc("POST /system/services/{id}/actions", func(w http.ResponseWriter, r *http.Request) {
		var body stockServiceRequest
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		d.DisallowUnknownFields()
		var extra any
		if d.Decode(&body) != nil || d.Decode(&extra) != io.EOF {
			nativeError(w, 400, "INVALID_SERVICE_REQUEST", "サービス操作の入力を確認してください")
			return
		}
		if !stockServiceMu.TryLock() {
			nativeError(w, 409, "SERVICE_BUSY", "別のサービスを操作中です")
			return
		}
		defer stockServiceMu.Unlock()
		defer invalidate()
		// A network service may disconnect its caller. Finish checking the submitted
		// systemd job even if the browser goes away; never blindly undo owner intent.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 17*time.Second)
		defer cancel()
		value, err := controlStockService(ctx, r.PathValue("id"), body, stockServiceRuntime())
		if err != nil {
			p := err.(*stockServiceProblem)
			nativeError(w, p.status, p.code, p.reason)
			return
		}
		log.Printf("stock-service id=%s action=%s before=%s after=%s verified=true", value.Service.ID, value.Action, value.Previous.State, value.Service.State)
		_ = json.NewEncoder(w).Encode(value)
	})
}
