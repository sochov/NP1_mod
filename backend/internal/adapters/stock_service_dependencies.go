package adapters

import (
	"context"
	"strings"
)

// These clients call the native camera service (NPVINClient or camera config).
// QMMF's only observed native client is NEventWatcherAV. Killing the server
// while that client is alive has caused AV SIGSEGV and a stock full reboot.
var stockCameraClientIDs = []string{"drive-rec", "image-recog", "remote-monitoring", "virtual-passenger", "system"}

func stockServiceDependencies(id, action string) (running, stopped []string) {
	if action == "stop" || action == "restart" {
		switch id {
		case "qmmf":
			stopped = append(append([]string{}, stockCameraClientIDs...), "event-av")
		case "event-av":
			stopped = append([]string{}, stockCameraClientIDs...)
		}
	}
	if action == "start" || action == "restart" {
		if id == "event-av" {
			running = []string{"qmmf"}
		}
		for _, client := range stockCameraClientIDs {
			if id == client {
				running = []string{"qmmf", "event-av"}
			}
		}
	}
	return
}

func stockDependencyReason(id, action string, services []StockService) string {
	running, stopped := stockServiceDependencies(id, action)
	byID := make(map[string]StockService, len(services))
	for _, service := range services {
		byID[service.ID] = service
	}
	label := func(id string) string {
		if def, ok := stockServiceDefinitionFor(id); ok {
			return def.label
		}
		return id
	}
	var needsStop, needsStart []string
	for _, dep := range stopped {
		value, ok := byID[dep]
		if !ok || !value.ProcessKnown || value.LoadState != "loaded" || value.Job != "" || value.MainPID != 0 || (value.State != "inactive" && value.State != "failed") {
			needsStop = append(needsStop, label(dep))
		}
	}
	for _, dep := range running {
		value, ok := byID[dep]
		if !ok || !value.ProcessKnown || value.LoadState != "loaded" || value.Job != "" || value.State != "active" || value.MainPID == 0 || !stockInvocationPattern.MatchString(value.InvocationID) {
			needsStart = append(needsStart, label(dep))
		}
	}
	if len(needsStop) > 0 {
		return "本体の異常復旧による再起動を防ぐため、先に「" + strings.Join(needsStop, "」「") + "」を停止してください。"
	}
	if len(needsStart) > 0 {
		return "先に「" + strings.Join(needsStart, "」「") + "」を起動し、稼働状態を確認してください。"
	}
	return ""
}

func guardStockServiceDependencies(services []StockService) {
	for index := range services {
		service := &services[index]
		var reasons []string
		for _, action := range []string{"start", "stop", "restart"} {
			reason := stockDependencyReason(service.ID, action, services)
			if reason == "" {
				continue
			}
			switch action {
			case "start":
				service.CanStart = false
			case "stop":
				service.CanStop = false
			case "restart":
				service.CanRestart = false
			}
			duplicate := false
			for _, existing := range reasons {
				duplicate = duplicate || existing == reason
			}
			if !duplicate {
				reasons = append(reasons, reason)
			}
		}
		if len(reasons) > 0 {
			if service.ControlReason != "" {
				service.ControlReason += " "
			}
			service.ControlReason += strings.Join(reasons, " ")
		}
	}
}

func verifyStockServiceDependencies(ctx context.Context, def stockServiceDefinition, action string, d stockServiceIO) error {
	running, stopped := stockServiceDependencies(def.id, action)
	ids := append(append([]string{}, running...), stopped...)
	if len(ids) == 0 {
		return nil
	}
	definitions := make([]stockServiceDefinition, 0, len(ids))
	for _, id := range ids {
		definition, ok := stockServiceDefinitionFor(id)
		if !ok {
			return &stockServiceProblem{409, "SERVICE_DEPENDENCY_UNAVAILABLE", "関連するカメラサービスの定義を確認できないため操作できません"}
		}
		definitions = append(definitions, definition)
	}
	values, err := readStockServices(ctx, definitions, d)
	if err != nil {
		return &stockServiceProblem{409, "SERVICE_DEPENDENCY_UNAVAILABLE", "関連するカメラサービスの状態を確認できないため操作できません"}
	}
	if reason := stockDependencyReason(def.id, action, values); reason != "" {
		return &stockServiceProblem{409, "SERVICE_DEPENDENCY_ACTIVE", reason}
	}
	return nil
}

// IsStockServiceID only validates a fixed definition; it performs no I/O.
func IsStockServiceID(id string) bool { _, ok := stockServiceDefinitionFor(id); return ok }
