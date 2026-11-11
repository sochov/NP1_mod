package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/np1-control-center/ncc/internal/model"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"
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
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	e = d.Decode(&v)
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
	return tailscaleObservation(v)
}
func tailscaleObservation(v map[string]any) model.Observation {
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
	hostName := tailscaleText(self["HostName"])
	dnsName := strings.TrimSuffix(tailscaleText(self["DNSName"]), ".")
	deviceName := hostName
	if name, _, ok := strings.Cut(dnsName, "."); ok && validTailscaleHostName(name) {
		deviceName = name
	}
	ownerID := ""
	switch id := self["UserID"].(type) {
	case json.Number:
		ownerID = id.String()
	case string:
		ownerID = id
	case float64:
		if id >= 0 && id <= 1<<53 && id == float64(int64(id)) {
			ownerID = strconv.FormatInt(int64(id), 10)
		}
	}
	users, _ := v["User"].(map[string]any)
	owner, _ := users[ownerID].(map[string]any)
	return model.Value("tailscale status --json", map[string]any{"backendState": v["BackendState"], "online": self["Online"], "ips": v["TailscaleIPs"], "homeRelay": self["Relay"], "activePeerConnections": connections, "version": v["Version"], "hostName": hostName, "deviceName": deviceName, "dnsName": dnsName, "accountName": tailscaleText(owner["DisplayName"]), "accountLogin": tailscaleText(owner["LoginName"]), "nameWritable": v["BackendState"] == "Running" && hostName != "" && tailscaleText(self["ID"]) != "", "remoteAccess": "管理IFのHTTP 80 / HTTPS 443。到達性はクライアント側から個別に確認"})
}
func tailscaleText(value any) string {
	text, ok := value.(string)
	if !ok || len(text) > 512 || strings.ContainsFunc(text, unicode.IsControl) {
		return ""
	}
	return strings.TrimSpace(text)
}
func TailscaleLogin() (string, error) {
	if !tailscaleEditMu.TryLock() {
		return "", errors.New("Tailscaleの設定を変更中です")
	}
	defer tailscaleEditMu.Unlock()
	if _, e := os.Stat(tailscale); e != nil {
		return "", errors.New("Tailscale is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	args := []string{"--socket=/run/np1-tailscale/tailscaled.sock", "up", "--accept-dns=false", "--accept-routes=false", "--ssh=false", "--netfilter-mode=off", "--timeout=10s"}
	// Reauthentication preserves an owner's chosen name. Only an unregistered
	// device receives the initial application default.
	previous, previousError := tailscaleStatus()
	if previousError != nil {
		return "", errors.New("Tailscaleの現在の設定を取得できません")
	}
	previousSelf, _ := previous["Self"].(map[string]any)
	if previous["BackendState"] == "NoState" {
		args = append(args, "--hostname=np1-control")
	} else if name := tailscaleText(previousSelf["HostName"]); name != "" {
		args = append(args, "--hostname="+name)
	}
	_ = exec.CommandContext(ctx, tailscale, args...).Run()
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
