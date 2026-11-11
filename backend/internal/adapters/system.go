package adapters

import (
	"github.com/np1-control-center/ncc/internal/model"
	"strconv"
	"strings"
)

func Memory() model.Observation {
	values := map[string]uint64{}
	for _, line := range strings.Split(read("/proc/meminfo"), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 {
			v, e := strconv.ParseUint(f[1], 10, 64)
			if e == nil {
				values[strings.TrimSuffix(f[0], ":")] = v * 1024
			}
		}
	}
	total, ok := values["MemTotal"]
	available, ok2 := values["MemAvailable"]
	if !ok || !ok2 || total == 0 || available > total {
		return model.Missing("/proc/meminfo", "RAM未取得")
	}
	return model.Value("/proc/meminfo", map[string]any{"totalBytes": strconv.FormatUint(total, 10), "availableBytes": strconv.FormatUint(available, 10), "usedBytes": strconv.FormatUint(total-available, 10), "usedPercent": float64(total-available) * 100 / float64(total)})
}
func (o *Observer) cpu() model.Observation {
	line := strings.Split(read("/proc/stat"), "\n")[0]
	f := strings.Fields(line)
	if len(f) < 9 {
		return model.Missing("/proc/stat", "CPU未取得")
	}
	var total, idle uint64
	for i := 1; i <= 8; i++ {
		v, e := strconv.ParseUint(f[i], 10, 64)
		if e != nil {
			return model.Missing("/proc/stat", "CPU未取得")
		}
		total += v
		if i == 4 || i == 5 {
			idle += v
		}
	}
	previousTotal, previousIdle := o.cpuTotal, o.cpuIdle
	o.cpuTotal, o.cpuIdle = total, idle
	if previousTotal == 0 || total <= previousTotal || idle < previousIdle {
		return model.Missing("/proc/stat", "CPU計測を開始しています")
	}
	delta, idleDelta := total-previousTotal, idle-previousIdle
	if idleDelta > delta {
		return model.Missing("/proc/stat", "CPU再計測中")
	}
	return model.Value("/proc/stat", map[string]any{"usedPercent": float64(delta-idleDelta) * 100 / float64(delta)})
}
