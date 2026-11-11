package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"

	"github.com/np1-control-center/ncc/internal/adapters"
)

func (s *Server) controlStockService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !adapters.IsStockServiceID(id) {
		fail(w, 404, "UNKNOWN_STOCK_SERVICE", "対象の純正サービスはありません")
		return
	}
	var body struct {
		Action               string `json:"action"`
		ExpectedInvocationID string `json:"expectedInvocationID"`
		ExpectedState        string `json:"expectedState"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Action != "start" && body.Action != "stop" && body.Action != "restart" {
		fail(w, 400, "INVALID_SERVICE_ACTION", "起動・停止・再起動のいずれかを指定してください")
		return
	}
	encoded, _ := json.Marshal(body)
	r.Body = io.NopCloser(bytes.NewReader(encoded))
	// Persist the fixed ID/action before submission. A stock crash/reboot can
	// otherwise erase the broker journal before its final response is recorded.
	audit := "stock-service." + id + "." + body.Action
	s.audit(audit, "requested")
	s.forwardControl(w, r, "/system/services/"+url.PathEscape(id)+"/actions", audit)
}
