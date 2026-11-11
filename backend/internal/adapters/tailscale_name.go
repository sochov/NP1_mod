package adapters

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/model"
)

var tailscaleEditMu sync.Mutex
var tailscaleHostPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func validTailscaleHostName(value string) bool { return tailscaleHostPattern.MatchString(value) }

type tailscaleNameRequest struct {
	ExpectedHostName string `json:"expectedHostName"`
	HostName         string `json:"hostName"`
}
type tailscaleNameProblem struct {
	status       int
	code, reason string
}

func (p *tailscaleNameProblem) Error() string { return p.reason }

type tailscaleNameIO struct {
	status func() (map[string]any, error)
	set    func(context.Context, string) error
	now    func() time.Time
	sleep  func(time.Duration)
}

func applyTailscaleName(ctx context.Context, body tailscaleNameRequest, d tailscaleNameIO) (model.Observation, error) {
	problem := func(status int, code, reason string) (model.Observation, error) {
		return model.Observation{}, &tailscaleNameProblem{status, code, reason}
	}
	if !validTailscaleHostName(body.HostName) || body.ExpectedHostName == "" || tailscaleText(body.ExpectedHostName) != body.ExpectedHostName {
		return problem(400, "INVALID_TAILSCALE_NAME", "機器名は63文字以内の英小文字・数字・ハイフンで入力してください")
	}
	before, e := d.status()
	if e != nil {
		return problem(503, "TAILSCALE_UNAVAILABLE", "Tailscaleの状態を取得できません")
	}
	self, _ := before["Self"].(map[string]any)
	id := tailscaleText(self["ID"])
	if before["BackendState"] != "Running" || id == "" {
		return problem(409, "TAILSCALE_NOT_REGISTERED", "Tailscaleへ接続してから機器名を変更してください")
	}
	if tailscaleText(self["HostName"]) != body.ExpectedHostName {
		return problem(409, "TAILSCALE_NAME_CONFLICT", "別の操作で機器名が変更されました。最新の状態を取得してください")
	}
	if body.HostName == body.ExpectedHostName {
		return tailscaleObservation(before), nil
	}
	beforeData := tailscaleObservation(before).Data.(map[string]any)
	oldRegistered, _ := beforeData["deviceName"].(string)
	// With automatic machine names, also await the control-plane registration
	// update. A separately assigned console name is intentionally preserved.
	registrationChanges := oldRegistered == body.ExpectedHostName && beforeData["dnsName"] != ""
	if e = d.set(ctx, body.HostName); e != nil {
		return problem(503, "TAILSCALE_RENAME_FAILED", "機器名を変更できませんでした。現在の状態を取得してください")
	}
	until := d.now().Add(6 * time.Second)
	for d.now().Before(until) && ctx.Err() == nil {
		after, e := d.status()
		if e == nil {
			next, _ := after["Self"].(map[string]any)
			if tailscaleText(next["ID"]) != id {
				return problem(409, "TAILSCALE_IDENTITY_CHANGED", "Tailscaleの登録状態が変わりました。最新の状態を取得してください")
			}
			if tailscaleText(next["HostName"]) == body.HostName {
				observed := tailscaleObservation(after)
				name, _ := observed.Data.(map[string]any)["deviceName"].(string)
				if !registrationChanges || name != "" && name != oldRegistered {
					return observed, nil
				}
			}
		}
		d.sleep(100 * time.Millisecond)
	}
	return problem(503, "TAILSCALE_RENAME_NOT_CONFIRMED", "機器名の反映をまだ確認できません。現在の状態を取得してください")
}
func HandleTailscaleName(w http.ResponseWriter, r *http.Request) {
	var body tailscaleNameRequest
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&body) != nil || d.Decode(&extra) != io.EOF {
		nativeError(w, 400, "INVALID_TAILSCALE_NAME", "機器名の入力を確認してください")
		return
	}
	if !tailscaleEditMu.TryLock() {
		nativeError(w, 409, "TAILSCALE_BUSY", "Tailscaleの設定を変更中です")
		return
	}
	defer tailscaleEditMu.Unlock()
	value, e := applyTailscaleName(r.Context(), body, tailscaleNameIO{status: tailscaleStatus, set: func(parent context.Context, name string) error {
		ctx, cancel := context.WithTimeout(parent, 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, tailscale, "--socket=/run/np1-tailscale/tailscaled.sock", "set", "--hostname="+name)
		cmd.WaitDelay = time.Second
		return cmd.Run()
	}, now: time.Now, sleep: time.Sleep})
	if e != nil {
		p := e.(*tailscaleNameProblem)
		nativeError(w, p.status, p.code, p.reason)
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}
