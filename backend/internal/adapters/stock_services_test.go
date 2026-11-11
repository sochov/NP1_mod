package adapters

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type stockServiceFixture struct {
	state, invocation  string
	pid                int
	commands           [][]string
	clock              time.Time
	apply, commandFail bool
}

func (f *stockServiceFixture) io() stockServiceIO {
	return stockServiceIO{now: func() time.Time { return f.clock }, sleep: func(_ context.Context, d time.Duration) error { f.clock = f.clock.Add(d); return nil }, run: func(_ context.Context, args ...string) (string, error) {
		if args[0] == "show" {
			unit := args[len(args)-1]
			return fmt.Sprintf("MainPID=%d\nId=%s\nLoadState=loaded\nActiveState=%s\nSubState=running\nInvocationID=%s\nUnitFileState=enabled\nCanStart=yes\nCanStop=yes\nRefuseManualStart=no\nRefuseManualStop=no\nJob=\n", f.pid, unit, f.state, f.invocation), nil
		}
		f.commands = append(f.commands, append([]string{}, args...))
		if f.commandFail {
			return "", errors.New("denied")
		}
		if f.apply {
			switch args[2] {
			case "stop":
				f.state, f.pid = "inactive", 0
			case "start", "restart":
				f.state, f.pid, f.invocation = "active", 43, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			}
		}
		return "", nil
	}}
}

func newStockServiceFixture() *stockServiceFixture {
	return &stockServiceFixture{state: "active", invocation: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", pid: 42, clock: time.Now(), apply: true}
}

func TestStockServiceIndividualActions(t *testing.T) {
	for _, action := range []string{"stop", "start", "restart"} {
		t.Run(action, func(t *testing.T) {
			f := newStockServiceFixture()
			if action == "start" {
				f.state, f.pid = "inactive", 0
			}
			result, err := controlStockService(context.Background(), "opening", stockServiceRequest{action, f.invocation, f.state}, f.io())
			if err != nil || result.Status != "applied" || result.Previous.ID != "opening" || result.Action != action {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			expected := []string{action}
			if action == "restart" {
				expected = []string{"stop", "start"}
			}
			if len(f.commands) != len(expected) {
				t.Fatalf("individual action must suppress dependency jobs: %v", f.commands)
			}
			for i, verb := range expected {
				if strings.Join(f.commands[i], " ") != "--no-block --job-mode=ignore-dependencies "+verb+" -- app-@Opening.service" {
					t.Fatalf("individual action must suppress dependency jobs: %v", f.commands)
				}
			}
			if action == "stop" && (result.Service.State != "inactive" || result.Service.MainPID != 0) {
				t.Fatal("stop accepted without inactive/PID0")
			}
			if action == "restart" && result.Service.InvocationID == "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
				t.Fatal("restart accepted without new invocation")
			}
		})
	}
}

func TestStockServiceRejectsStaleAndArbitraryCommands(t *testing.T) {
	for _, item := range []struct {
		id, action, state, invocation, code string
	}{
		{"../np1-control", "stop", "active", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "UNKNOWN_STOCK_SERVICE"},
		{"np1-control.service", "stop", "active", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "UNKNOWN_STOCK_SERVICE"},
		{"opening", "disable", "active", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "INVALID_SERVICE_ACTION"},
		{"opening", "restart; reboot", "active", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "INVALID_SERVICE_ACTION"},
		{"opening", "stop", "unknown", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "INVALID_SERVICE_STATE"},
		{"opening", "stop", "active", "old", "SERVICE_STATE_CHANGED"},
		{"opening", "stop", "inactive", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "SERVICE_STATE_CHANGED"},
	} {
		f := newStockServiceFixture()
		_, err := controlStockService(context.Background(), item.id, stockServiceRequest{item.action, item.invocation, item.state}, f.io())
		p, ok := err.(*stockServiceProblem)
		if !ok || p.code != item.code || len(f.commands) != 0 {
			t.Fatalf("%+v: error %v commands %v", item, err, f.commands)
		}
	}
}

func TestStockServiceNoFalseRestartSuccessOrForcedFallback(t *testing.T) {
	f := newStockServiceFixture()
	f.apply = false
	_, err := controlStockService(context.Background(), "opening", stockServiceRequest{"restart", f.invocation, "active"}, f.io())
	if err.(*stockServiceProblem).code != "SERVICE_NOT_CONFIRMED" || len(f.commands) != 1 {
		t.Fatalf("old invocation must not confirm restart or trigger forced fallback: %v %v", err, f.commands)
	}
	f = newStockServiceFixture()
	f.commandFail = true
	_, err = controlStockService(context.Background(), "opening", stockServiceRequest{"stop", f.invocation, "active"}, f.io())
	if err.(*stockServiceProblem).code != "SERVICE_COMMAND_FAILED" || len(f.commands) != 1 {
		t.Fatalf("command failure must remain a failure: %v", err)
	}
}

func TestStockServicePartialObservationNeverEnablesControl(t *testing.T) {
	f := newStockServiceFixture()
	d := f.io()
	d.run = func(context.Context, ...string) (string, error) {
		return "Id=app-@Opening.service\nLoadState=loaded\n", nil
	}
	def, _ := stockServiceDefinitionFor("opening")
	if _, err := readStockServices(context.Background(), []stockServiceDefinition{def}, d); err == nil {
		t.Fatal("incomplete observation was accepted")
	}
	d.run = func(context.Context, ...string) (string, error) {
		return "Id=app-@Opening.service\nLoadState=not-found\nActiveState=inactive\nCanStart=yes\nCanStop=yes\n", nil
	}
	list, err := readStockServices(context.Background(), []stockServiceDefinition{def}, d)
	if err != nil || list[0].CanStart || list[0].CanStop || list[0].CanRestart {
		t.Fatalf("missing unit was controllable: %+v %v", list, err)
	}
}

func TestStockServiceHandlerStrictBodyAndBusy(t *testing.T) {
	mux := http.NewServeMux()
	RegisterStockServiceHandlers(mux, func() {})
	for _, body := range []string{`{"action":"stop","unit":"foo"}`, `{} {}`, `null true`} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest("POST", "/system/services/opening/actions", strings.NewReader(body)))
		if recorder.Code != 400 {
			t.Fatalf("invalid body: %d %s", recorder.Code, recorder.Body.String())
		}
	}
	stockServiceMu.Lock()
	defer stockServiceMu.Unlock()
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest("POST", "/system/services/opening/actions", strings.NewReader(`{"action":"stop","expectedState":"active","expectedInvocationID":"id"}`)))
	if recorder.Code != 409 || !strings.Contains(recorder.Body.String(), "SERVICE_BUSY") {
		t.Fatal(recorder.Code, recorder.Body.String())
	}
}

func TestStockRestartPreparesSingletonOnlyAfterConfirmedStop(t *testing.T) {
	f := newStockServiceFixture()
	d := f.io()
	prepared := 0
	d.prepareStart = func(_ context.Context, def stockServiceDefinition) error {
		if def.id != "opening" || f.state != "inactive" || f.pid != 0 || len(f.commands) != 1 || f.commands[0][2] != "stop" {
			t.Fatal("singleton prepared before confirmed stop")
		}
		prepared++
		return nil
	}
	_, err := controlStockService(context.Background(), "opening", stockServiceRequest{"restart", f.invocation, f.state}, d)
	if err != nil || prepared != 1 || len(f.commands) != 2 {
		t.Fatal("restart did not prepare once between stop and start", err, f.commands)
	}
	f = newStockServiceFixture()
	d = f.io()
	d.prepareStart = func(context.Context, stockServiceDefinition) error { return errors.New("live map reference") }
	_, err = controlStockService(context.Background(), "opening", stockServiceRequest{"restart", f.invocation, f.state}, d)
	if err.(*stockServiceProblem).code != "SERVICE_START_RESOURCE_BUSY" || len(f.commands) != 1 {
		t.Fatal("start proceeded after failed resource validation", err, f.commands)
	}
}

func TestStockSystemEventRestartRestoresPreviouslyActiveHeartbeat(t *testing.T) {
	f := newStockServiceFixture()
	result, err := controlStockService(context.Background(), "event-sys", stockServiceRequest{"restart", f.invocation, f.state}, f.io())
	if err != nil || result.Status != "applied" || len(f.commands) != 3 || strings.Join(f.commands[2], " ") != "--no-block --job-mode=ignore-dependencies start -- socketserver.service" {
		t.Fatal("native ExecStop's heartbeat side effect was not restored", err, f.commands)
	}
}
