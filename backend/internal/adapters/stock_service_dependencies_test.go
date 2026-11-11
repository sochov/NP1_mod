package adapters

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func cameraDependencyFixture() []StockService {
	values := []StockService{}
	for _, def := range stockServiceDefinitions {
		values = append(values, StockService{ID: def.id, Name: def.unit, Label: def.label, LoadState: "loaded", State: "active", MainPID: 10, InvocationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CanStart: true, CanStop: true, CanRestart: true, ProcessKnown: true})
	}
	return values
}

func setCameraFixtureState(values []StockService, id, state string) {
	for index := range values {
		if values[index].ID == id {
			values[index].State = state
			values[index].MainPID = 0
			if state == "active" {
				values[index].MainPID = 10
			}
		}
	}
}

func TestCameraDependencyGuardsReflectFiveClientsAndActualAVCrash(t *testing.T) {
	values := cameraDependencyFixture()
	guardStockServiceDependencies(values)
	for _, item := range values {
		if item.ID == "qmmf" || item.ID == "event-av" {
			if item.CanStop || item.CanRestart || item.ControlReason == "" {
				t.Fatal("live camera dependency can still be stopped", item)
			}
		}
		if item.ID == "opening" && (!item.CanStop || !item.CanRestart) {
			t.Fatal("unrelated service was disabled")
		}
	}
	for _, protected := range append(append([]string{}, stockCameraClientIDs...), "event-av") {
		values = cameraDependencyFixture()
		for _, id := range append(append([]string{}, stockCameraClientIDs...), "event-av") {
			setCameraFixtureState(values, id, "inactive")
		}
		setCameraFixtureState(values, protected, "active")
		if stockDependencyReason("qmmf", "stop", values) == "" || stockDependencyReason("qmmf", "restart", values) == "" {
			t.Fatal("one remaining native client escaped protection", protected)
		}
	}
}

func TestCameraDependenciesRequireReverseStartupOrderAndKnownState(t *testing.T) {
	values := cameraDependencyFixture()
	for _, id := range append(append([]string{}, stockCameraClientIDs...), "event-av") {
		setCameraFixtureState(values, id, "inactive")
	}
	if reason := stockDependencyReason("qmmf", "stop", values); reason != "" {
		t.Fatal("known stopped clients blocked server stop", reason)
	}
	for index := range values {
		if values[index].ID == "event-av" {
			values[index].ProcessKnown = false
			if stockDependencyReason("qmmf", "stop", values) == "" {
				t.Fatal("missing PID metadata became proof of a stopped AV process")
			}
			values[index].ProcessKnown = true
		}
	}
	setCameraFixtureState(values, "qmmf", "inactive")
	if stockDependencyReason("event-av", "start", values) == "" {
		t.Fatal("AV can start without its server")
	}
	for _, client := range stockCameraClientIDs {
		if stockDependencyReason(client, "start", values) == "" {
			t.Fatal("native camera client can start without its parents", client)
		}
	}
	setCameraFixtureState(values, "event-av", "unknown")
	if stockDependencyReason("qmmf", "stop", values) == "" {
		t.Fatal("unknown AV state became permission to stop server")
	}
}

func TestCameraGuardRechecksBeforeAnyStopCommand(t *testing.T) {
	values := cameraDependencyFixture()
	for _, action := range []string{"stop", "restart"} {
		commands := 0
		d := stockServiceIO{now: time.Now, sleep: func(context.Context, time.Duration) error { return nil }, run: func(_ context.Context, args ...string) (string, error) {
			if args[0] != "show" {
				commands++
				return "", nil
			}
			out := ""
			for _, argument := range args {
				for _, value := range values {
					if argument == value.Name {
						out += fmt.Sprintf("Id=%s\nLoadState=loaded\nActiveState=active\nSubState=running\nMainPID=10\nInvocationID=%s\nUnitFileState=enabled\nCanStart=yes\nCanStop=yes\nJob=\n\n", value.Name, value.InvocationID)
					}
				}
			}
			return out, nil
		}}
		_, err := controlStockService(context.Background(), "qmmf", stockServiceRequest{action, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "active"}, d)
		problem, ok := err.(*stockServiceProblem)
		if !ok || problem.code != "SERVICE_DEPENDENCY_ACTIVE" || commands != 0 || !strings.Contains(problem.reason, "映像・音声イベント") {
			t.Fatal("stale UI request was not rejected before mutation", err, commands)
		}
	}
}
