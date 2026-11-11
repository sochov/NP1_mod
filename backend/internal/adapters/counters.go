package adapters

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// TrafficInterfaces selects a single physical EC25 interface and the verified
// firmware's STA interface. AP bridges, QMAP children and tunnels are excluded.
func TrafficInterfaces() map[string]string {
	roles := map[string]string{}
	entries, _ := os.ReadDir("/sys/class/net")
	candidates := []string{}
	for _, entry := range entries {
		name := entry.Name()
		base := "/sys/class/net/" + name
		device, e := filepath.EvalSymlinks(base + "/device")
		if e != nil {
			continue
		}
		driver, e := filepath.EvalSymlinks(base + "/device/driver")
		if e != nil || filepath.Base(driver) != "GobiNet" {
			continue
		}
		resolved, e := filepath.EvalSymlinks(base)
		if e != nil || resolved != filepath.Join(device, "net", name) {
			continue
		}
		parent := device
		for parent != "/" {
			vendor := read(filepath.Join(parent, "idVendor"))
			if vendor != "" {
				if vendor == "2c7c" && read(filepath.Join(parent, "idProduct")) == "0125" {
					candidates = append(candidates, name)
				}
				break
			}
			parent = filepath.Dir(parent)
		}
	}
	if len(candidates) == 1 {
		roles["cellular"] = candidates[0]
	}
	if resolved, e := filepath.EvalSymlinks("/sys/class/net/wlan0"); e == nil && strings.HasSuffix(resolved, "/18800000.qcom,icnss/net/wlan0") {
		roles["wifi"] = "wlan0"
	}
	return roles
}

func defaultInterface(routes string) string {
	best := ""
	metric := uint64(^uint32(0))
	for _, line := range strings.Split(routes, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 || fields[1] != "00000000" {
			continue
		}
		flags, e := strconv.ParseUint(fields[3], 16, 32)
		if e != nil || flags&1 == 0 {
			continue
		}
		value, e := strconv.ParseUint(fields[6], 10, 32)
		if e == nil && value < metric {
			best = fields[0]
			metric = value
		}
	}
	return best
}

func TrafficSnapshot() map[string]any {
	return map[string]any{"traffic": Traffic(), "bootID": read("/proc/sys/kernel/random/boot_id"), "interfaces": TrafficInterfaces(), "wan": defaultInterface(read("/proc/net/route"))}
}
