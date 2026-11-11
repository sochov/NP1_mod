package broker

import (
	"errors"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/np1-control-center/ncc/internal/adapters"
)

// Only the root broker can connect. The non-root Web process cannot reach this
// socket, and the worker accepts one pinned enum operation rather than commands.
func ServeNative() error {
	if os.Geteuid() != 0 {
		return errors.New("native worker requires root")
	}
	socket := adapters.NativeControlSocket
	if info, e := os.Lstat(socket); e == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return errors.New("native socket path occupied")
		}
		if e = os.Remove(socket); e != nil {
			return e
		}
	}
	l, e := net.Listen("unix", socket)
	if e != nil {
		return e
	}
	defer l.Close()
	if e = os.Chmod(socket, 0600); e != nil {
		return e
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /alexa-settings", adapters.HandleAlexaLocal)
	mux.HandleFunc("PUT /alexa-settings", adapters.HandleAlexaLocal)
	mux.HandleFunc("PUT /native-settings/{id}", adapters.HandleNativeControl)
	s := &http.Server{Handler: mux, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 4096}
	return s.Serve(listener{l, 0})
}
