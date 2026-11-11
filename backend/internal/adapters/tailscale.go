package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/np1-control-center/ncc/internal/model"
	"os"
	"os/exec"
	"time"
)

const tailscale = "/run/np1-tailscale/tailscale"

func tailscaleStatus() (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	b, e := exec.CommandContext(ctx, tailscale, "--socket=/run/np1-tailscale/tailscaled.sock", "status", "--json").Output()
	if len(b) == 0 {
		return nil, e
	}
	var v map[string]any
	e = json.Unmarshal(b, &v)
	return v, e
}
func TailscaleStatus() model.Observation {
	if _, e := os.Stat(tailscale); e != nil {
		return model.Missing("tailscale", "未導入")
	}
	v, e := tailscaleStatus()
	if e != nil {
		return model.Missing("tailscale status --json", "daemon状態未取得")
	}
	self, _ := v["Self"].(map[string]any)
	connections := []string{}
	if peers, ok := v["Peer"].(map[string]any); ok {
		for _, raw := range peers {
			peer, ok := raw.(map[string]any)
			if !ok || peer["Active"] != true {
				continue
			}
			kind := "Unknown"
			if a, ok := peer["CurAddr"].(string); ok && a != "" {
				kind = "Direct"
			} else if relay, ok := peer["Relay"].(string); ok && relay != "" {
				kind = "DERP"
			}
			connections = append(connections, kind)
		}
	}
	return model.Value("tailscale status --json", map[string]any{"backendState": v["BackendState"], "online": self["Online"], "ips": v["TailscaleIPs"], "homeRelay": self["Relay"], "activePeerConnections": connections, "version": v["Version"], "remoteAccess": "管理IFのHTTP 80 / HTTPS 443。到達性はクライアント側から個別に確認"})
}
func TailscaleLogin() (string, error) {
	if _, e := os.Stat(tailscale); e != nil {
		return "", errors.New("Tailscale is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, tailscale, "--socket=/run/np1-tailscale/tailscaled.sock", "up", "--accept-dns=false", "--accept-routes=false", "--ssh=false", "--netfilter-mode=off", "--hostname=np1-control", "--timeout=10s").Run()
	v, e := tailscaleStatus()
	if e != nil {
		return "", e
	}
	u, _ := v["AuthURL"].(string)
	if u == "" && v["BackendState"] != "Running" {
		return "", errors.New("認証URLを取得できません。WAN疎通を確認してください")
	}
	return u, nil
}
