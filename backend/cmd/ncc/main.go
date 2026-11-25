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
	"github.com/np1-control-center/ncc/internal/numbershare"
	"github.com/np1-control-center/ncc/internal/otaguard"
	"github.com/np1-control-center/ncc/internal/otapolicy"
	"github.com/np1-control-center/ncc/internal/radio"
	"github.com/np1-control-center/ncc/internal/trips"
	"github.com/np1-control-center/ncc/internal/update"
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
	trialSeconds := flag.Int("trial-seconds", 90, "sta-outage-trial: seconds without Wi-Fi")
	numberShareConfig := flag.String("numbershare-config", "/overlay/owner-apps/np1-control/private/numbershare.json", "NumberShare sync configuration")
	numberShareStateDir := flag.String("numbershare-state-dir", "/overlay/owner-apps/np1-control/state", "NumberShare sync state directory")
	numberShareManagedConfig := flag.String("numbershare-managed-config", "", "owner UI NumberShare configuration; defaults below the NumberShare state directory")
	numberShareRecovery := flag.String("numbershare-recovery", "/overlay/owner-apps/np1-control/private/numbershare-recovery.json", "NumberShare recovery output")
	numberShareBootstrapKey := flag.String("numbershare-bootstrap-key", "/overlay/owner-apps/np1-control/private/numbershare-bootstrap.key", "NumberShare user registration key file")
	numberShareUserKey := flag.String("numbershare-user-key", "/overlay/owner-apps/np1-control/private/numbershare-user.key", "existing NumberShare user device key for adding this NP1")
	numberShareInternalUserID := flag.String("numbershare-internal-user-id", "", "optional internal user ID file; defaults to local Tailscale UserID")
	numberShareDeviceName := flag.String("numbershare-device-name", "", "NumberShare device display name; defaults to hostname")
	numberShareUserHandle := flag.String("numbershare-user-handle", "", "explicit anonymous user handle; initial registration otherwise waits for the owner UI")
	platePositionURL := flag.String("plate-position-url", "http://127.0.0.1/api/status", "local dashboard position endpoint")
	radioRepairApply := flag.Bool("radio-repair-apply", false, "apply measured GPS associations; stop the NCC logger and back up history first")
	radioRepairStateDir := flag.Bool("radio-repair-state-dir", false, "print the configured radio history directory without reading GPS or writing files")
	radioRepairCheck := flag.Bool("radio-repair-check", false, "fail if any GPS associations remain to be applied")
	radioRepairTrack := flag.String("radio-repair-track", "", "private measured recording-track export to supplement retained SD fixes")
	updateKey := flag.String("update-key", "", "Ed25519 public key file for updater enrollment")
	updateURL := flag.String("update-url", "", "internal updater fetch URL")
	updateLimit := flag.Int64("update-limit", 0, "internal updater fetch byte limit")
	updateWiFi := flag.Bool("update-wifi", true, "bind internal updater fetch to the NP1 Wi-Fi interface")
	flag.Parse()
	if *numberShareManagedConfig == "" {
		*numberShareManagedConfig = numbershare.ManagedConfigPath(*numberShareStateDir)
	}
	switch *mode {
	case "update-version":
		fmt.Println(update.Version)
		return
	case "update-recorder-contract":
		identity, ok := adapters.RecorderArtifactIdentity()
		if !ok {
			log.Fatal("recorder artifact build contract is missing")
		}
		fmt.Println(identity)
		return
	case "update-serve", "update-recover", "update-key-check", "update-fetch":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		var e error
		switch *mode {
		case "update-serve":
			e = update.Serve(ctx)
		case "update-recover":
			e = update.Recover(ctx)
		case "update-key-check":
			e = update.CheckKey(*updateKey)
		case "update-fetch":
			e = update.Fetch(ctx, *updateWiFi, *updateURL, *updateLimit, os.Stdout)
		}
		if e != nil {
			log.Fatal(e)
		}
		return
	case "numbershare-register":
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		options := numbershare.RegistrationOptions{
			ConfigPath: *numberShareConfig, ManagedConfigPath: *numberShareManagedConfig, RecoveryPath: *numberShareRecovery,
			BootstrapKeyPath: *numberShareBootstrapKey, ExistingUserKeyPath: *numberShareUserKey,
			InternalUserIDPath: *numberShareInternalUserID, DeviceName: *numberShareDeviceName, UserHandle: *numberShareUserHandle,
		}
		if e := runNumberShareRegister(ctx, options); e != nil {
			log.Fatal(e)
		}
		return
	case "numbershare-check":
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if e := runNumberShareCheck(ctx, *numberShareStateDir); e != nil {
			log.Fatal(e)
		}
		return
	case "numbershare-sync":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if e := runNumberShareSync(ctx, *numberShareStateDir, *numberShareConfig, *platePositionURL); e != nil {
			log.Fatal(e)
		}
		return
	case "radio-history-repair":
		c, err := config.Load(*conf)
		if err != nil {
			log.Fatal(err)
		}
		if *radioRepairStateDir {
			fmt.Println(c.StateDir)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		positions, truncated, err := adapters.RadioHistoryPositions(ctx)
		if err != nil {
			log.Fatal(err)
		}
		importedCount := 0
		var importedTrack trips.TrackReport
		if *radioRepairTrack != "" {
			file, openErr := os.Open(*radioRepairTrack)
			if openErr != nil {
				log.Fatal(openErr)
			}
			imported, partial, readErr := radio.ReadRecordedPositions(file, time.Now())
			file.Close()
			if readErr != nil {
				log.Fatal(readErr)
			}
			raw, readErr := os.ReadFile(*radioRepairTrack)
			var export struct {
				Data trips.TrackReport `json:"data"`
			}
			if readErr != nil || json.Unmarshal(raw, &export) != nil {
				log.Fatal("invalid measured track export")
			}
			importedTrack = export.Data
			positions = append(positions, imported...)
			importedCount = len(imported)
			truncated = truncated || partial
		}
		report, err := radio.NewHistory(c.StateDir).RepairPositions(positions, *radioRepairApply)
		if err != nil {
			log.Fatal(err)
		}
		if *radioRepairApply && importedCount > 0 {
			if _, err = trips.NewTrackStore(c.StateDir).Ingest(importedTrack, time.Now()); err != nil {
				log.Fatal(err)
			}
		}
		report.PositionCount, report.SourceTruncated = len(positions), truncated
		report.ImportedPositions = importedCount
		if *radioRepairCheck && report.Matched != 0 {
			log.Fatal("radio GPS associations still pending")
		}
		if err = json.NewEncoder(os.Stdout).Encode(report); err != nil {
			log.Fatal(err)
		}
		return
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
	case "uplink-supervisor":
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer stop()
		if e := adapters.RunUplinkSupervisor(ctx); e != nil {
			log.Fatal(e)
		}
		return
	case "wifi-scan-probe":
		adapters.WiFiScanProbe(os.Stdout)
		return
	case "sta-outage-trial":
		// Field trial: disconnect the saved Wi-Fi for --trial-seconds, watch the uplinks, reconnect.
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer stop()
		if e := adapters.RunSTAOutageTrial(ctx, time.Duration(*trialSeconds)*time.Second, os.Stdout); e != nil {
			log.Fatal(e)
		}
		return
	case "ota-guard":
		// run: keep checking (unit np1-control-ota-guard); check: one round; release: remove the network layer.
		guard := otaguard.Device(func() (otaguard.FlagState, error) {
			v, e := otapolicy.Enforce()
			return otaguard.FlagState{Disabled: v.Disabled, Managed: v.Managed, Protected: v.Protected, Heals: v.Heals, LastHeal: v.LastHeal}, e
		}, otapolicy.FirmwareMatches)
		switch *otaAction {
		case "run":
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
			defer stop()
			guard.Loop(ctx, 30*time.Second)
		case "check":
			report := guard.Check(context.Background())
			if e := guard.Save(report); e != nil {
				log.Fatal(e)
			}
			if e := json.NewEncoder(os.Stdout).Encode(report); e != nil {
				log.Fatal(e)
			}
		case "release":
			if e := guard.Release(context.Background()); e != nil {
				log.Fatal(e)
			}
		default:
			log.Fatal("unknown OTA guard action")
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
		go func() {
			verified := false
			failures := 0
			for {
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				if err := adapters.EnsureRecorderHookAtBoot(ctx); err != nil {
					failures++
					if verified || failures == 1 || failures%12 == 0 {
						log.Printf("recorder runtime reconciliation pending: %v", err)
					}
					verified = false
				} else {
					if !verified {
						log.Printf("recorder runtime settings and recording verified")
					}
					verified = true
					failures = 0
				}
				cancel()
				time.Sleep(5 * time.Second)
			}
		}()
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
		if !adapters.RecorderArtifactContractReady() {
			log.Fatal("recorder artifact build contract is missing")
		}
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
	case "plate-sensor":
		if e := runPlateSensor(*conf); e != nil {
			log.Fatal(e)
		}
	case "android-auto":
		if e := runAndroidAuto(*conf); e != nil {
			log.Fatal(e)
		}
	case "plate-replay":
		if e := runPlateReplay(*conf); e != nil {
			log.Fatal(e)
		}
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
