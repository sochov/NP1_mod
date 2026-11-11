package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/np1-control-center/ncc/internal/config"
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"time"
)

func managementAddresses(c config.Config) map[string]bool {
	out := map[string]bool{}
	for _, base := range []string{c.HTTPListen, c.Listen} {
		if base != "" {
			out[base] = true
		}
	}
	_, httpPort, _ := net.SplitHostPort(c.HTTPListen)
	_, tlsPort, _ := net.SplitHostPort(c.Listen)
	for _, name := range c.ManagementInterfaces {
		iface, e := net.InterfaceByName(name)
		if e != nil || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addresses, _ := iface.Addrs()
		for _, a := range addresses {
			ip, _, e := net.ParseCIDR(a.String())
			if e != nil || ip.To4() == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			for _, port := range []string{httpPort, tlsPort} {
				if port != "" {
					out[net.JoinHostPort(ip.String(), port)] = true
				}
			}
		}
	}
	return out
}

// ServeManagement binds only explicit management interface addresses, and follows address changes.
// Never bind 0.0.0.0 or expose the web listener on WWAN interfaces.
func ServeManagement(ctx context.Context, c config.Config, h http.Handler) error {
	cert, e := tls.LoadX509KeyPair(c.Certificate, c.Key)
	if e != nil {
		return e
	}
	_, tlsPort, _ := net.SplitHostPort(c.Listen)
	active := map[string]*http.Server{}
	defer func() {
		for _, s := range active {
			_ = s.Close()
		}
	}()
	reconcile := func() error {
		desired := managementAddresses(c)
		for addr, s := range active {
			if !desired[addr] {
				_ = s.Close()
				delete(active, addr)
			}
		}
		keys := []string{}
		for addr := range desired {
			keys = append(keys, addr)
		}
		sort.Strings(keys)
		for _, addr := range keys {
			if active[addr] != nil {
				continue
			}
			l, e := net.Listen("tcp", addr)
			if e != nil {
				if addr == c.HTTPListen {
					return fmt.Errorf("HTTP listen: %w", e)
				}
				log.Printf("management listener unavailable: %s", addr)
				continue
			}
			_, port, _ := net.SplitHostPort(addr)
			secure := port == tlsPort
			server := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
			active[addr] = server
			scheme := "http"
			if secure {
				scheme = "https"
				l = tls.NewListener(l, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}})
			}
			log.Printf("NCC network-trust interface: %s://%s", scheme, addr)
			go func(s *http.Server, l net.Listener) {
				if e := s.Serve(l); e != nil && e != http.ErrServerClosed {
					log.Printf("listener stopped: %v", e)
				}
			}(server, l)
		}
		return nil
	}
	if c.HTTPListen != "" {
		_, p, e := net.SplitHostPort(c.HTTPListen)
		port, _ := strconv.Atoi(p)
		if e != nil || port < 1 || port > 65535 {
			return fmt.Errorf("invalid HTTP port")
		}
	}
	if e = reconcile(); e != nil {
		return e
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if e = reconcile(); e != nil {
				return e
			}
		}
	}
}
