package adapters

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/model"
)

const LED = "/sys/devices/platform/soc/88c000.i2c/i2c-2/2-0066"

type Observer struct {
	mu     sync.Mutex
	at     time.Time
	cached map[string]model.Observation
}

func read(path string) string {
	b, e := os.ReadFile(path)
	if e != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
func command(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	b, e := exec.CommandContext(ctx, name, args...).Output()
	return strings.TrimSpace(string(b)), e
}
func text(source, path string) model.Observation {
	v := read(path)
	if v == "" {
		return model.Missing(source, "取得できません")
	}
	return model.Value(source, v)
}
func (o *Observer) Get() map[string]model.Observation {
	o.mu.Lock()
	defer o.mu.Unlock()
	if time.Since(o.at) < 10*time.Second {
		return o.cached
	}
	m := map[string]model.Observation{}
	uptime := strings.Fields(read("/proc/uptime"))
	if len(uptime) > 0 {
		n, e := strconv.ParseFloat(uptime[0], 64)
		if e == nil {
			m["uptime"] = model.Value("/proc/uptime", n)
		}
	}
	m["memory"] = text("/proc/meminfo", "/proc/meminfo")
	m["load"] = text("/proc/loadavg", "/proc/loadavg")
	m["kernel"] = text("/proc/sys/kernel/osrelease", "/proc/sys/kernel/osrelease")
	m["usb"] = text("sysfs USB role", "/sys/bus/platform/devices/a600000.ssusb/mode")
	led := map[string]string{}
	for _, k := range []string{"mode", "status", "value"} {
		led[k] = read(LED + "/led_" + k)
	}
	m["led"] = model.Value("sysfs ENE6K582", led)
	if led["mode"] == "" {
		m["led"] = model.Missing("sysfs ENE6K582", "LEDにアクセスできません")
	}
	temperatures := []map[string]any{}
	zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
	for _, z := range zones {
		v, e := strconv.ParseFloat(read(z+"/temp"), 64)
		if e != nil {
			continue
		}
		if v > 1000 {
			v /= 1000
		}
		if v < -40 || v > 150 {
			continue
		}
		temperatures = append(temperatures, map[string]any{"name": read(z + "/type"), "celsius": v, "source": z + "/temp"})
	}
	m["temperatures"] = model.Value("sysfs thermal (外気温ではありません)", temperatures)
	services := []map[string]string{}
	for _, u := range []string{"app-@Communication.service", "app-@DriveRec.service", "app-@ImageRecog.service", "app-@Voice.service", "qmmf-server.service", "QCMAP_ConnectionManagerd.service", "NEventWatcherCom.service"} {
		v, e := command("systemctl", "is-active", u)
		if e != nil && v == "" {
			v = "unknown"
		}
		services = append(services, map[string]string{"name": u, "state": v})
	}
	m["services"] = model.Value("systemctl is-active", services)
	if v, e := command("ip", "-j", "address", "show"); e == nil {
		var a any
		if json.Unmarshal([]byte(v), &a) == nil {
			m["interfaces"] = model.Value("ip -j address", a)
		}
	}
	if _, ok := m["interfaces"]; !ok {
		v, e := command("ip", "-br", "addr")
		if e == nil {
			m["interfaces"] = model.Value("ip -br addr", v)
		} else {
			m["interfaces"] = model.Missing("ip address", "インターフェース未取得")
		}
	}
	if v, e := command("ip", "route", "show"); e == nil {
		m["routes"] = model.Value("ip route", v)
	} else {
		m["routes"] = model.Missing("ip route", "経路未取得")
	}
	// Read-only STATUS on the vendor-owned control socket. Never start another supplicant.
	if v, e := VendorStatus(); e == nil {
		safe := map[string]string{}
		for _, l := range strings.Split(v, "\n") {
			k, val, ok := strings.Cut(l, "=")
			if ok && (k == "wpa_state" || k == "freq" || k == "ip_address" || k == "key_mgmt") {
				safe[k] = val
			}
		}
		m["wifi"] = model.Value("vendor wpa_cli STATUS", safe)
	} else {
		m["wifi"] = model.Missing("vendor WPA STATUS", "STA状態未取得: "+e.Error())
	}
	m["traffic"] = Traffic()
	m["modem"] = model.Missing("QCMAP adapter", "QCMAPが制御中。安全な照会IPCは調査中。LTE接続・電波値を推測しません")
	m["hotspot"] = model.Missing("QCMAP adapter", "AP bridgeの状態とSSID/DHCP提供は別です。AP制御経路未検証")
	m["internet"] = model.Missing("active probe disabled", "経路の存在だけではインターネット疎通を確認できません")
	m["gps"] = model.Missing("vendor GNSS adapter", "純正GNSS照会経路未検証。位置情報の記録は既定OFF")
	m["tailscale"] = TailscaleStatus()
	m["sd"] = SDStatus()
	o.cached = m
	o.at = time.Now()
	return m
}
func Traffic() model.Observation {
	f, e := os.Open("/proc/net/dev")
	if e != nil {
		return model.Missing("/proc/net/dev", e.Error())
	}
	defer f.Close()
	items := []map[string]string{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		name, rest, ok := strings.Cut(s.Text(), ":")
		if !ok {
			continue
		}
		v := strings.Fields(rest)
		if len(v) < 16 {
			continue
		}
		items = append(items, map[string]string{"interface": strings.TrimSpace(name), "rxBytes": v[0], "txBytes": v[8]})
	}
	return model.Value("/proc/net/dev (boot counter, decimal uint64)", items)
}
func Capabilities() []model.Capability {
	caps := []model.Capability{
		{ID: "system", Status: "available", Reason: "proc/sysfs と固定 systemd unit の読み取り"},
		{ID: "network", Status: "available", Reason: "STA/route/インターフェース読み取り。疎通は別途実測が必要"},
		{ID: "traffic", Status: "available", Reason: "boot毎の64bitカウンタ。日次永続集計は開発中"},
		{ID: "storage", Status: "available", Reason: "SD mount確認・限定root内一覧とHTTP Range。書込みは無効"},
		{ID: "camera", Status: "available", Reason: "既存NV12 exportのJPEG。セッションとプライバシー許可必須"},
		{ID: "hotspot", Status: "unknown", Reason: "純正IPC解析・SSID/DHCP・AP+STA試験が必要"},
		{ID: "apn", Status: "unknown", Reason: "QCMAP所有中。プロファイル変更は無効"},
		{ID: "led", Status: "available", Reason: "状態の読み取りのみ。純正表示の復元経路は未検証"},
		{ID: "usb", Status: "available", Reason: "role読み取り。代替管理経路未確立のため切替拒否"},
		{ID: "gps", Status: "unknown", Reason: "GNSS取得経路・走行試験が必要"},
		{ID: "radio-map", Status: "unknown", Reason: "空間集計ライブラリは実装。実機観測の収集は未検証"},
		{ID: "tailscale", Status: "unknown", Reason: "TUN存在確認済み。導入・tailnet認証が必要"},
		{ID: "nas", Status: "unknown", Reason: "転送先・資格情報・AP+STA経路の検証が必要"},
		{ID: "audio", Status: "unknown", Reason: "純正PulseAudioとの共存・音量readbackが未検証"},
		{ID: "mqtt", Status: "unsupported", Reason: "P3: 未実装"},
	}
	for i := range caps {
		switch caps[i].ID {
		case "storage":
			caps[i].Status = SDStatus().Status
		case "led":
			if read(LED+"/led_mode") == "" {
				caps[i].Status = "unknown"
				caps[i].Reason = "sysfsは存在しますが読み戻しできません。制御は無効です"
			}
		case "tailscale":
			if _, e := os.Stat(tailscale); e == nil {
				caps[i].Status = "available"
				caps[i].Reason = "導入済み。ログイン状態の読取り。遠隔到達は認証後に別途検証"
			}
		case "camera":
			p, _ := CameraPath("front")
			if st, e := os.Stat(p); e != nil {
				caps[i].Status = "unknown"
			} else if time.Since(st.ModTime()) > 15*time.Second {
				caps[i].Status = "stale"
			}
		}
	}
	return caps
}
func CameraPath(source string) (string, error) {
	name := ""
	switch source {
	case "front":
		name = "frontcamera"
	case "in":
		name = "incamera"
	default:
		return "", fmt.Errorf("invalid camera")
	}
	return "/tmp/application/imagerecog/imagedata/" + name + "/imagedata/value", nil
}
