package adapters

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const stockStartHelper = "/run/np1-control/stock-start-helper"

// Set to the reproducibly built helper fingerprint after its standalone trial.
const stockStartHelperSHA = "d1f491a8fe0841610d26fe005fad1e97a200be8f32e9dd8e2e9f5aa851fa3a30"

func stockStartBarrierSupported(id string) bool {
	switch id {
	case "drive-rec", "image-recog", "system", "remote-monitoring", "virtual-passenger":
		return true
	}
	return false
}

type stockStartOutput struct {
	data     []byte
	overflow bool
}

func (b *stockStartOutput) Write(data []byte) (int, error) {
	if len(b.data)+len(data) <= 16384 {
		b.data = append(b.data, data...)
	} else {
		b.overflow = true
	}
	// Never abort a vendor critical section because its diagnostic pipe fills.
	return len(data), nil
}
func stockStartOutputReady(data string) bool {
	line := strings.TrimSpace(data)
	return line == "start-barrier=notified; owner=matched; threads=multiple; cleanup=matched" ||
		line == "start-barrier=already-running; owner=matched; threads=multiple; vendor-not-loaded"
}

func completeStockServiceStart(ctx context.Context, def stockServiceDefinition, service StockService) error {
	if !stockStartBarrierSupported(def.id) {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if service.ID != def.id || service.Name != def.unit || service.State != "active" || service.MainPID <= 1 || !stockInvocationPattern.MatchString(service.InvocationID) {
		return errors.New("stock startup identity unavailable")
	}
	if !nativeFirmwareMatches(stockStartHelper, stockStartHelperSHA) {
		return errors.New("stock startup helper unavailable")
	}
	if !nativeControlMu.TryLock() {
		return errors.New("native settings operation in progress")
	}
	defer nativeControlMu.Unlock()
	release, err := lockNativeControlExisting()
	if err != nil {
		return err
	}
	defer release()
	// The pinned helper bounds its vendor waits and must leave its shared mutex
	// before exiting. Caller cancellation must never SIGKILL it inside Notify.
	command := exec.Command(stockStartHelper, "--notify", def.id, strconv.FormatUint(service.MainPID, 10), service.InvocationID)
	command.Env = []string{"PATH=/usr/bin:/bin", "LD_LIBRARY_PATH=/usr/local/lib", "NCC_STOCK_START_NOTIFY=1"}
	var output stockStartOutput
	var diagnostic stockStartOutput
	command.Stdout, command.Stderr = &output, &diagnostic
	if command.Run() != nil || output.overflow || !stockStartOutputReady(string(output.data)) {
		_ = os.WriteFile("/run/np1-control/last-stock-start-error.log", diagnostic.data, 0600)
		return errors.New("stock startup notification unverified")
	}
	return nil
}
