package adapters

import (
	"encoding/json"
	"io"
	"net/http"
)

func HandleAPNSettings(w http.ResponseWriter, r *http.Request) {
	view, e := APNEditorSettings()
	if e != nil {
		nativeError(w, 503, "APN_SETTINGS_UNAVAILABLE", "APN設定を取得できません")
		return
	}
	recordingJSON(w, 200, struct {
		APNEditorView
		ApplyAvailable bool   `json:"applyAvailable"`
		ApplyReason    string `json:"applyReason"`
	}{view, false, "純正の適用経路・接続確認・元設定復旧を検証中です"})
}
func HandleSaveAPNSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ExpectedRevision int                `json:"expectedRevision"`
		Profiles         []APNProfileChange `json:"profiles"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil {
		nativeError(w, 400, "INVALID_APN_SETTINGS", "APN設定の形式を確認してください")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		nativeError(w, 400, "INVALID_APN_SETTINGS", "APN設定は1件だけ指定してください")
		return
	}
	value, e := SaveAPNSettings(body.ExpectedRevision, body.Profiles)
	if e != nil {
		if e.Error() == "REVISION_CONFLICT" {
			nativeError(w, 409, "REVISION_CONFLICT", "別の操作でAPN設定が変更されました")
		} else {
			nativeError(w, 400, "INVALID_APN_SETTINGS", "APN・認証情報の文字数と形式を確認してください")
		}
		return
	}
	recordingJSON(w, 200, struct {
		APNSettingsView
		ApplyAvailable bool   `json:"applyAvailable"`
		ApplyReason    string `json:"applyReason"`
	}{value, false, "純正の適用経路と元設定復旧を検証中です"})
}
