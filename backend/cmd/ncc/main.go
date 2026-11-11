package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/np1-control-center/ncc/internal/api"
	"github.com/np1-control-center/ncc/internal/broker"
	"github.com/np1-control-center/ncc/internal/config"
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
	flag.Parse()
	switch *mode {
	case "self-test":
		fmt.Printf("ncc self-test ok: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		return
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
		c.ManagementInterfaces = []string{"wlan0", "bridge0", "tailscale0"}
		c.Username = ""
		c.Salt = nil
		c.PasswordHash = nil
		if e = config.Atomic(*conf, c); e != nil {
			log.Fatal(e)
		}
		fmt.Println("Network authentication enabled; Web password removed; HTTP management enabled")
	case "broker":
		log.Fatal(broker.Serve(*socket, uint32(*uid)))
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
		if e = api.ServeManagement(ctx, c, api.New(c).Handler()); e != nil {
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
