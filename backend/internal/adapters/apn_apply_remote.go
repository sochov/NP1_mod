package adapters

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"time"
)

const APNControlSocket = "/run/np1-control-apn/control.sock"

var apnRemoteJobID = regexp.MustCompile(`^[a-f0-9]{48}$`)

func APNApplyAvailable() bool {
	info, e := os.Lstat(APNControlSocket)
	return e == nil && info.Mode()&os.ModeSocket != 0
}
func HandleAPNApplyRemote(w http.ResponseWriter, r *http.Request) {
	target := "/apply"
	if r.Method == http.MethodGet {
		id := r.PathValue("id")
		if !apnRemoteJobID.MatchString(id) {
			nativeError(w, 400, "INVALID_APN_JOB", "APN適用処理を確認してください")
			return
		}
		target = "/jobs/" + id
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", APNControlSocket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 8 * time.Second}
	request, e := http.NewRequestWithContext(r.Context(), r.Method, "http://apn"+target, http.MaxBytesReader(w, r.Body, 256))
	if e != nil {
		nativeError(w, 400, "INVALID_APN_REQUEST", "APN入力を確認してください")
		return
	}
	response, e := client.Do(request)
	if e != nil {
		nativeError(w, 503, "APN_WORKER_UNAVAILABLE", "APN適用処理に接続できません")
		return
	}
	defer response.Body.Close()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(response.Body, 32768))
}
