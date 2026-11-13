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
	"sync"
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
		// An interface that is briefly down (the hotspot restarting, the station switching to 4G)
		// keeps its address; closing and rebinding on every flap left windows in which hotspot
		// clients were refused. Only addresses that are gone are dropped.
		iface, e := net.InterfaceByName(name)
		if e != nil {
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
	var mu sync.Mutex
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for _, s := range active {
			_ = s.Close()
		}
	}()
	reconcile := func() error {
		mu.Lock()
		defer mu.Unlock()
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
			go func(addr string, s *http.Server, l net.Listener) {
				if e := s.Serve(l); e != nil && e != http.ErrServerClosed {
					// Forget a listener that died so the next reconcile binds it again; before,
					// it stayed "active" and the address refused connections until a restart.
					log.Printf("listener stopped: %s: %v", addr, e)
					mu.Lock()
					if active[addr] == s {
						delete(active, addr)
					}
					mu.Unlock()
				}
			}(addr, server, l)
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
