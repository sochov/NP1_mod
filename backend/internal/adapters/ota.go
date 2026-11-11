package adapters

import (
	"encoding/json"
	"errors"
	"github.com/np1-control-center/ncc/internal/model"
	"github.com/np1-control-center/ncc/internal/otapolicy"
	"io"
	"net/http"
)

func OTAPolicy() model.Observation {
	state, e := otapolicy.Run("status")
	if e != nil {
		return model.Missing("純正OTA取得抑止flag", "本体更新の設定を取得できません。対応firmwareと管理記録を確認してください")
	}
	return model.Value("純正Update GetDownloadListのpresence flag", state)
}

// HandleOTAPolicy must run behind the root broker boundary; the Web worker
// cannot read/write the root-only restoration ledger or choose a flag path.
func HandleOTAPolicy(w http.ResponseWriter, r *http.Request) {
	handleOTAPolicy(w, r, OTAPolicy, otapolicy.Set)
}
func handleOTAPolicy(w http.ResponseWriter, r *http.Request, observe func() model.Observation, apply func(string, bool) (otapolicy.Status, error)) {
	if r.Method == http.MethodGet {
		recordingJSON(w, 200, observe())
		return
	}
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, PUT")
		nativeError(w, 405, "METHOD_NOT_ALLOWED", "GETまたはPUTが必要です")
		return
	}
	var request struct {
		ExpectedToken *string `json:"expectedToken"`
		Disabled      *bool   `json:"disabled"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || request.ExpectedToken == nil || request.Disabled == nil || len(*request.ExpectedToken) != 64 {
		nativeError(w, 400, "INVALID_OTA_POLICY", "設定値の形式が不正です")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		nativeError(w, 400, "INVALID_OTA_POLICY", "設定値の形式が不正です")
		return
	}
	state, e := apply(*request.ExpectedToken, *request.Disabled)
	if e != nil {
		if errors.Is(e, otapolicy.ErrConflict) {
			nativeError(w, 409, "OTA_POLICY_CONFLICT", "別の操作で設定が変わりました。状態を更新してください")
		} else {
			nativeError(w, 503, "OTA_POLICY_UNAVAILABLE", "変更後の設定を確認できません。管理記録と本体の状態を確認してください")
		}
		return
	}
	recordingJSON(w, 200, model.Value("純正Update GetDownloadListのpresence flag", state))
}
