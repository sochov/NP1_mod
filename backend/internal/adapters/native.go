package adapters

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/np1-control-center/ncc/internal/model"
)

type NativeItem struct {
	ID          string              `json:"id"`
	Label       string              `json:"label"`
	Description string              `json:"description,omitempty"`
	Group       string              `json:"group"`
	Kind        string              `json:"kind"`
	Value       any                 `json:"value"`
	ValueLabel  string              `json:"valueLabel,omitempty"`
	Writable    bool                `json:"writable"`
	Status      string              `json:"status"`
	Reason      string              `json:"reason"`
	Options     []NativeValueOption `json:"options,omitempty"`
}
type NativeData struct {
	Items []NativeItem `json:"items"`
}
type nativeDefinition struct {
	id, label, group, description string
	index                         int
	min, max                      int64
	binary                        bool
}

// The indices come from this firmware's Setting call sites, not a generic offset guess.
// Unknown enum meanings remain numeric; no ON/OFF is inferred from a nonzero enum.
var nativeDefinitions = []nativeDefinition{
	{"privacy-mode", "プライバシーモード", "プライバシー", "純正のプライバシー設定", 8, 0, 1, false},
	{"driving-support", "運転支援", "運転の案内", "運転支援の案内設定", 39, 0, 1, false},
	{"guidance-frequency", "案内の頻度", "運転の案内", "案内する頻度の設定", 44, 0, 2147483647, false},
	{"service-cabin-camera", "車内カメラの利用", "プライバシー", "純正サービスによる車内カメラの利用設定", 49, 0, 1, false},
	{"tailgating-detection", "後続車の接近検知", "運転の案内", "後方から接近する車両の検知設定", 50, 0, 1, true},
	{"tailgating-sensitivity", "後続車の検知感度", "運転の案内", "後続車の接近を検知する感度", 51, 0, 2, false},
	{"traffic-check", "交通情報の確認", "経路と交通情報", "純正アプリの交通情報設定", 61, 0, 1, false},
	{"beginner-driver", "初心者向けの案内", "経路と交通情報", "初心者ドライバーモードの保存値", 62, 0, 2147483647, false},
	{"traffic-congestion", "渋滞情報", "経路と交通情報", "渋滞に関する設定", 63, 0, 1, false},
	{"traffic-regulation", "交通規制", "経路と交通情報", "交通規制に関する設定", 64, 0, 1, false},
	{"ferry", "フェリーの利用", "経路と交通情報", "経路検索時のフェリー設定", 65, 0, 1, false},
	{"toll-road", "有料道路の利用", "経路と交通情報", "経路検索時の有料道路設定", 66, 0, 1, false},
	{"smart-ic", "スマートICの利用", "経路と交通情報", "経路検索時のスマートIC設定", 67, 0, 1, false},
	{"route-search", "経路の検索方法", "経路と交通情報", "経路検索で優先する条件", 68, 0, 5, false},
	{"daily-voice", "いつものコエ", "案内・コエ", "日常の案内で選ばれている声。音声データの取得状況は未確認です", 77, 0, 5, false},
	{"support-voice", "安心サポートのコエ", "案内・コエ", "安心サポートで選ばれている声。音声データの取得状況は未確認です", 78, 0, 5, false},
	{"front-car-start", "前の車の発進通知", "お知らせ", "前車発進時の通知設定", 79, 0, 2, false},
	{"signal-change", "青信号の通知", "お知らせ", "青信号に変わった時の通知設定", 80, 0, 2, false},
}

type nativeFiles struct{ memory, setting, library, settingHash, libraryHash string }

var nativeStockFiles = nativeFiles{
	"/tmp/victoria/victoria_PMEM_AL_SRV_SET_SETTING_ITEM.map", "/usr/local/bin/Setting", "/usr/local/lib/libAplLib.so",
	"d5b876fd8e5a3c18037b25812f5d989351045ecd024b40a99c551c4d00f7e0ac", "83bcfe30838001b382e58771fd28eabf17e43e6e72568ee5ad80656d223eae00",
}

func nativeFirmwareMatches(path, want string) bool {
	f, e := os.Open(path)
	if e != nil {
		return false
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return false
	}
	h := sha256.New()
	if _, e = io.Copy(h, io.LimitReader(f, 64<<20)); e != nil {
		return false
	}
	return hex.EncodeToString(h.Sum(nil)) == want
}
func parseNativeValues(blob []byte) ([]int32, error) {
	if len(blob) != 7296 || binary.LittleEndian.Uint64(blob[:8]) != 7168 {
		return nil, errors.New("unsupported setting memory size")
	}
	p := blob[128:]
	if binary.LittleEndian.Uint32(p[:4]) != 1 || binary.LittleEndian.Uint32(p[4:8]) != 1 {
		return nil, errors.New("setting memory is not initialized")
	}
	offset, count := binary.LittleEndian.Uint32(p[20:24]), binary.LittleEndian.Uint32(p[24:28])
	if offset != 44 || count != 86 || uint64(offset)+uint64(count)*4 > uint64(len(p)) {
		return nil, errors.New("unsupported setting memory layout")
	}
	values := make([]int32, count)
	for i := range values {
		values[i] = int32(binary.LittleEndian.Uint32(p[int(offset)+i*4:]))
	}
	return values, nil
}
func readNativeValues(files nativeFiles) ([]int32, error) {
	if !nativeFirmwareMatches(files.setting, files.settingHash) || !nativeFirmwareMatches(files.library, files.libraryHash) {
		return nil, errors.New("このファームウェアの設定形式は未確認です")
	}
	readOnce := func() ([]byte, error) {
		f, e := os.Open(files.memory)
		if e != nil {
			return nil, e
		}
		defer f.Close()
		return io.ReadAll(io.LimitReader(f, 8193))
	}
	first, e := readOnce()
	if e != nil {
		return nil, errors.New("純正設定の現在値を取得できません")
	}
	second, e := readOnce()
	if e != nil {
		return nil, errors.New("純正設定の再取得に失敗しました")
	}
	// Ignore the vendor's mutable lock/refcount header, compare the entire actual data twice.
	if len(first) < 128 || len(second) < 128 || !bytes.Equal(first[128:], second[128:]) {
		return nil, errors.New("純正設定が更新中です。もう一度取得してください")
	}
	values, e := parseNativeValues(second)
	if e != nil {
		return nil, errors.New("純正設定のデータ形式を確認できません")
	}
	return values, nil
}
func nativeObservation(values []int32, readError error) model.Observation {
	data := NativeData{Items: make([]NativeItem, 0, len(nativeDefinitions))}
	for _, d := range nativeDefinitions {
		item := NativeItem{ID: d.id, Label: d.label, Group: d.group, Description: d.description, Kind: "info", Status: "unknown", Reason: "現在値と変更経路を確認しています。変更はできません。"}
		if readError == nil && d.index < len(values) {
			v := int64(values[d.index])
			if v >= d.min && v <= d.max {
				item.Value = v
				item.Status = "available"
				item.Reason = "現在値を取得しました。変更後の動作と再取得の検証が済むまで、変更はできません。"
				if label, known := NativeStorageLabel(d.index, v); known {
					item.ValueLabel = label
				} else if d.binary {
					if v == 0 {
						item.ValueLabel = "ON"
					} else {
						item.ValueLabel = "OFF"
					}
				} else {
					item.ValueLabel = "保存値 " + strconv.FormatInt(v, 10) + "（選択肢の対応を確認中）"
					if d.id == "tailgating-sensitivity" {
						item.ValueLabel = []string{"低", "標準", "高"}[v]
					}
				}
			}
		}
		data.Items = append(data.Items, item)
	}
	result := model.Value("firmware-matched stock setting memory (read only)", data)
	if readError != nil {
		result.Status = "unknown"
		result.Verified = false
		result.Reason = readError.Error()
	}
	return result
}
func NativeSettings() model.Observation {
	values, e := readNativeValues(nativeStockFiles)
	result := nativeObservation(values, e)
	if e == nil && nativeControlReady() {
		data := result.Data.(NativeData)
		for i := range data.Items {
			spec, known := nativeSpec(data.Items[i].ID)
			value, valueKnown := data.Items[i].Value.(int64)
			if known && valueKnown && value >= 0 && value <= spec.maximum && data.Items[i].Status == "available" && nativeFirmwareMatches(spec.helper, spec.hash) && (spec.index != 8 || values[23] == 1) {
				data.Items[i].Writable = true
				data.Items[i].Kind = "select"
				data.Items[i].Options = NativeStorageOptions(spec.index)
				data.Items[i].Reason = ""
			}
		}
		result.Data = data
	}
	return result
}
func HandleNativeSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		nativeError(w, 405, "METHOD_NOT_ALLOWED", "読取りのみ対応しています")
		return
	}
	recordingJSON(w, 200, NativeSettings())
}
func nativeError(w http.ResponseWriter, status int, code, reason string) {
	recordingJSON(w, status, map[string]string{"status": "error", "code": code, "reason": reason})
}
func nativeInteger(raw json.RawMessage, d nativeDefinition) (int64, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if e := decoder.Decode(&value); e != nil {
		return 0, e
	}
	number, ok := value.(json.Number)
	if !ok {
		return 0, errors.New("integer required")
	}
	v, e := number.Int64()
	if e != nil || v < d.min || v > d.max {
		return 0, errors.New("integer out of range")
	}
	return v, nil
}
func HandleNativeSetting(w http.ResponseWriter, r *http.Request) {
	handleNativeSettingWithApply(w, r, NativeSettings, nativeApplyRemote)
}
func handleNativeSetting(w http.ResponseWriter, r *http.Request, observe func() model.Observation) {
	handleNativeSettingWithApply(w, r, observe, nil)
}
func handleNativeSettingWithApply(w http.ResponseWriter, r *http.Request, observe func() model.Observation, apply func(string, int64, int64) error) {
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", "PUT")
		nativeError(w, 405, "METHOD_NOT_ALLOWED", "この操作にはPUTが必要です")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		id = strings.TrimPrefix(r.URL.Path, "/native-settings/")
	}
	var definition *nativeDefinition
	for i := range nativeDefinitions {
		if nativeDefinitions[i].id == id {
			definition = &nativeDefinitions[i]
			break
		}
	}
	if definition == nil {
		nativeError(w, 404, "UNKNOWN_NATIVE_SETTING", "未対応の設定項目です")
		return
	}
	var request struct {
		ExpectedValue json.RawMessage `json:"expectedValue"`
		Value         json.RawMessage `json:"value"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(&request); e != nil {
		nativeError(w, 400, "INVALID_NATIVE_SETTING", "設定値の形式が不正です")
		return
	}
	var extra any
	if e := decoder.Decode(&extra); e != io.EOF {
		nativeError(w, 400, "INVALID_NATIVE_SETTING", "設定値の形式が不正です")
		return
	}
	before, e := nativeInteger(request.ExpectedValue, *definition)
	if e != nil {
		nativeError(w, 400, "INVALID_EXPECTED_VALUE", "変更前の値が不正です")
		return
	}
	next, e := nativeInteger(request.Value, *definition)
	if e != nil {
		nativeError(w, 400, "INVALID_NATIVE_VALUE", "設定値の型または範囲が不正です")
		return
	}
	snapshot := observe()
	data, ok := snapshot.Data.(NativeData)
	if snapshot.Status != "available" || !ok {
		nativeError(w, 503, "NATIVE_SETTINGS_UNAVAILABLE", "現在値を確認できません。状態を更新してください")
		return
	}
	found := false
	for _, item := range data.Items {
		if item.ID == id {
			found = true
			v, known := item.Value.(int64)
			if !known {
				nativeError(w, 503, "NATIVE_SETTINGS_UNAVAILABLE", "現在値が未取得です")
				return
			}
			if v != before {
				nativeError(w, 409, "NATIVE_SETTING_CONFLICT", "別の操作で設定が変わりました。状態を更新してください")
				return
			}
			break
		}
	}
	if !found {
		nativeError(w, 503, "NATIVE_SETTINGS_UNAVAILABLE", "現在値が未取得です")
		return
	}
	_, supported := nativeSpec(id)
	if supported && apply != nil {
		if e := apply(id, before, next); e != nil {
			nativeError(w, 503, "NATIVE_SETTING_NOT_CONFIRMED", e.Error())
			return
		}
		recordingJSON(w, 200, observe())
		return
	}
	// Fail closed: no arbitrary key/path/library invocation and no fake successful write.
	nativeError(w, 403, "NATIVE_SETTING_READ_ONLY", "この設定は読取り専用です。純正の変更経路を検証しています")
}
