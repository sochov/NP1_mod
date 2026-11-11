package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/np1-control-center/ncc/internal/adapters"
	"github.com/np1-control-center/ncc/internal/api"
	"github.com/np1-control-center/ncc/internal/broker"
	"github.com/np1-control-center/ncc/internal/config"
	"github.com/np1-control-center/ncc/internal/led"
	"github.com/np1-control-center/ncc/internal/modem"
	"github.com/np1-control-center/ncc/internal/otapolicy"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

func main() {
	mode := flag.String("mode", "server", "server, broker, setup, self-test")
	conf := flag.String("config", "/overlay/owner-apps/np1-control/config/config.json", "configuration")
	dir := flag.String("config-dir", "/overlay/owner-apps/np1-control/config", "setup directory")
	socket := flag.String("socket", "/run/np1-control/broker.sock", "broker socket")
	uid := flag.Uint("worker-uid", 65534, "permitted peer UID")
	otaAction := flag.String("ota-action", "status", "fixed OTA policy action")
	apnRevision := flag.Int("apn-revision", 0, "private APN editor revision for fixed worker child")
	flag.Parse()
	switch *mode {
	case "owned-cellular-session-probe":
		if e := runOwnedCellularSessionProbe(); e != nil {
			log.Fatal(e)
		}
		return
	case "owned-cellular-preflight":
		if e := runOwnedCellularPreflight(); e != nil {
			log.Fatal(e)
		}
		return
	case "cellular-internet-probe":
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		value, e := adapters.ProbeCellularInternet(ctx)
		if encodeErr := json.NewEncoder(os.Stdout).Encode(value); encodeErr != nil {
			log.Fatal("CELLULAR_PROBE_OUTPUT_FAILED")
		}
		if e != nil {
			log.Fatal(e)
		}
		return
	case "stock-cm-nas-readonly":
		if e := runStockCMNASReadOnly(); e != nil {
			log.Fatal(e)
		}
		return
	case "stock-cm-profiles-readonly":
		if e := runStockCMProfilesReadOnly(); e != nil {
			log.Fatal(e)
		}
		return
	case "stock-cm-ownership-readonly":
		if e := runStockCMOwnershipReadOnly(); e != nil {
			log.Fatal(e)
		}
		return
	case "apn-apply-revision":
		if e := runAPNApplyRevision(*apnRevision); e != nil {
			log.Fatal("APN_HELPER_OUTPUT_FAILED")
		}
		return
	case "native-apn-route-recover":
		if e := runNativeAPNRouteRecovery(); e != nil {
			log.Fatal(e)
		}
		return
	case "apn-worker":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if e := broker.ServeAPN(ctx); e != nil {
			log.Fatal(e)
		}
		return
	case "native-apn-network-check":
		if e := runNativeAPNNetworkCheck(); e != nil {
			log.Fatal(e)
		}
		return
	case "native-apn-same-profile-trial":
		if e := runNativeAPNSameProfileTrial(); e != nil {
			log.Fatal(e)
		}
		return
	case "native-apn-status-probe":
		if e := runNativeAPNStatusProbe(); e != nil {
			log.Fatal(e)
		}
		return
	case "apn-editor-seed":
		if e := runAPNEditorSeed(); e != nil {
			log.Fatal(e)
		}
		return
	case "gobinet-profile-list-probe":
		if e := runGobiNetProfileInventoryProbe(); e != nil {
			log.Fatal(e)
		}
		return
	case "ota-policy":
		value, e := otapolicy.Run(*otaAction)
		if e != nil {
			log.Fatal(e)
		}
		if e = json.NewEncoder(os.Stdout).Encode(value); e != nil {
			log.Fatal(e)
		}
		return
	case "profiles-observer-check":
		if e := runProfilesObserverCheck(); e != nil {
			log.Fatal(e)
		}
		return
	case "gobinet-profiles-probe":
		if e := runGobiNetProfilesProbe(); e != nil {
			log.Fatal(e)
		}
		return
	case "native-worker":
		log.Fatal(broker.ServeNative())
	case "ap-worker", "ap-smoke", "ap-cleanup":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		var e error
		switch *mode {
		case "ap-worker":
			e = adapters.RunAP(ctx)
		case "ap-smoke":
			e = adapters.RunAPSmoke(ctx)
		case "ap-cleanup":
			e = adapters.RunAPCleanup()
		}
		if e != nil {
			log.Fatal(e)
		}
		return
	case "self-test":
		fmt.Printf("ncc self-test ok: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		return
	case "at-probe":
		ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
		defer cancel()
		v, e := modem.Probe(ctx)
		if e != nil {
			log.Fatal(e)
		}
		if e = json.NewEncoder(os.Stdout).Encode(v); e != nil {
			log.Fatal(e)
		}
		return
	case "gobinet-probe":
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		v, e := modem.ReadGobiNetSignal(ctx)
		if e != nil {
			log.Fatal(e)
		}
		if e = json.NewEncoder(os.Stdout).Encode(v); e != nil {
			log.Fatal(e)
		}
		return
	case "gobinet-status-probe":
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		v, e := modem.ReadGobiNetStatus(ctx)
		if e != nil {
			log.Fatal(e)
		}
		if e = json.NewEncoder(os.Stdout).Encode(v); e != nil {
			log.Fatal(e)
		}
		return
	case "modem-worker", "modem-worker-status":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		workerMode := modem.SignalOnly
		if *mode == "modem-worker-status" {
			workerMode = modem.WithStatus
		}
		if e := modem.RunWorker(ctx, workerMode); e != nil {
			log.Fatal(e)
		}
	case "setup":
		fp, e := config.Setup(*dir, "")
		if e != nil {
			log.Fatal(e)
		}
		fmt.Printf("TLS SHA256: %s\n", fp)
		return
	case "network-trust":
		c, e := config.Load(*conf)
		if e != nil {
			log.Fatal(e)
		}
		c.NetworkTrust = true
		c.HTTPListen = "127.0.0.1:80"
		c.Listen = "127.0.0.1:443"
		c.ManagementInterfaces = []string{"wlan0", "bridge0", "nccap0", "tailscale0"}
		c.Username = ""
		c.Salt = nil
		c.PasswordHash = nil
		if e = config.Atomic(*conf, c); e != nil {
			log.Fatal(e)
		}
		fmt.Println("Network authentication enabled; Web password removed; HTTP management enabled")
	case "broker":
		log.Fatal(broker.Serve(*socket, uint32(*uid)))
	case "led":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if e := led.RunManaged(ctx, led.Sysfs); e != nil {
			log.Fatal(e)
		}
	case "server":
		if os.Geteuid() == 0 {
			log.Fatal("refusing to run Web/API as root")
		}
		c, e := config.Load(*conf)
		if e != nil {
			log.Fatal(e)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		go watchdog(ctx)
		app := api.New(c)
		go app.Collect(ctx)
		defer app.Close()
		if e = api.ServeManagement(ctx, c, app.Handler()); e != nil {
			log.Fatal(e)
		}
	default:
		log.Fatal("invalid mode")
	}
}
func watchdog(ctx context.Context) {
	socket := os.Getenv("NOTIFY_SOCKET")
	if socket == "" {
		return
	}
	send := func(msg string) {
		c, e := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: socket, Net: "unixgram"})
		if e == nil {
			_, _ = c.Write([]byte(msg))
			_ = c.Close()
		}
	}
	send("READY=1")
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			send("WATCHDOG=1")
		}
	}
}
