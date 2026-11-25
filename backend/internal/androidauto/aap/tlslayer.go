package aap

import (
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// The protocol carries TLS records inside its own frames: handshake records in
// ENCAPSULATED_SSL messages, application records as encrypted frame payloads.
// recordConn is the in-memory transport crypto/tls runs on.
type recordConn struct {
	mu      sync.Mutex
	cond    *sync.Cond
	in      bytes.Buffer
	out     bytes.Buffer
	closed  bool
	appMode bool
	need    chan struct{}
	flush   func([]byte) error
}

func newRecordConn() *recordConn {
	c := &recordConn{need: make(chan struct{}, 1)}
	c.cond = sync.NewCond(&c.mu)
	return c
}

// errAgain tells crypto/tls that no more input is buffered. It is temporary, so
// the TLS connection stays usable for the next frame.
type errAgain struct{}

func (errAgain) Error() string   { return "aap: no buffered record" }
func (errAgain) Timeout() bool   { return true }
func (errAgain) Temporary() bool { return true }

func (c *recordConn) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for c.in.Len() == 0 && !c.closed {
		if c.appMode {
			return 0, errAgain{}
		}
		if c.out.Len() > 0 && c.flush != nil {
			data := append([]byte(nil), c.out.Bytes()...)
			c.out.Reset()
			f := c.flush
			c.mu.Unlock()
			e := f(data)
			c.mu.Lock()
			if e != nil {
				return 0, e
			}
			continue
		}
		select {
		case c.need <- struct{}{}:
		default:
		}
		c.cond.Wait()
	}
	if c.in.Len() == 0 {
		return 0, io.EOF
	}
	return c.in.Read(p)
}

func (c *recordConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, net.ErrClosed
	}
	return c.out.Write(p)
}

func (c *recordConn) feed(b []byte) {
	c.mu.Lock()
	c.in.Write(b)
	c.cond.Broadcast()
	c.mu.Unlock()
}

func (c *recordConn) takeOut() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	b := append([]byte(nil), c.out.Bytes()...)
	c.out.Reset()
	return b
}

func (c *recordConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.cond.Broadcast()
	c.mu.Unlock()
	return nil
}

type dummyAddr struct{}

func (dummyAddr) Network() string { return "aap" }
func (dummyAddr) String() string  { return "aap" }

func (c *recordConn) LocalAddr() net.Addr                { return dummyAddr{} }
func (c *recordConn) RemoteAddr() net.Addr               { return dummyAddr{} }
func (c *recordConn) SetDeadline(t time.Time) error      { return nil }
func (c *recordConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *recordConn) SetWriteDeadline(t time.Time) error { return nil }

// tlsLayer is the phone's TLS server toward the head unit (the head unit is the
// TLS client in this protocol).
type tlsLayer struct {
	rc  *recordConn
	tc  *tls.Conn
	wmu sync.Mutex
	rmu sync.Mutex
}

// ServerTLSConfig returns the TLS settings the head unit side expects: TLS 1.2
// with one record per frame.
func ServerTLSConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates:                []tls.Certificate{cert},
		MinVersion:                  tls.VersionTLS12,
		MaxVersion:                  tls.VersionTLS12,
		SessionTicketsDisabled:      true,
		DynamicRecordSizingDisabled: true,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
			tls.TLS_RSA_WITH_AES_128_CBC_SHA,
		},
	}
}

// handshake runs the TLS server handshake. next returns the next handshake
// payload from the peer; send delivers our handshake bytes to it.
func handshakeTLS(wrap func(net.Conn) *tls.Conn, next func() ([]byte, error), send func([]byte) error) (*tlsLayer, error) {
	rc := newRecordConn()
	rc.flush = send
	tc := wrap(rc)
	done := make(chan error, 1)
	go func() { done <- tc.Handshake() }()
	for {
		select {
		case e := <-done:
			if e != nil {
				return nil, e
			}
			if out := rc.takeOut(); len(out) > 0 {
				if e = send(out); e != nil {
					return nil, e
				}
			}
			rc.mu.Lock()
			rc.flush = nil
			rc.appMode = true
			rc.mu.Unlock()
			return &tlsLayer{rc: rc, tc: tc}, nil
		case <-rc.need:
			b, e := next()
			if e != nil {
				rc.Close()
				<-done
				return nil, e
			}
			rc.feed(b)
		}
	}
}

// encrypt returns the TLS records carrying plain (at most maxFragment bytes,
// so exactly one record).
func (l *tlsLayer) encrypt(plain []byte) ([]byte, error) {
	l.wmu.Lock()
	defer l.wmu.Unlock()
	if _, e := l.tc.Write(plain); e != nil {
		return nil, e
	}
	return l.rc.takeOut(), nil
}

// decrypt returns the plaintext of the TLS records in one frame payload.
func (l *tlsLayer) decrypt(records []byte) ([]byte, error) {
	l.rmu.Lock()
	defer l.rmu.Unlock()
	l.rc.feed(records)
	var out []byte
	buf := make([]byte, 32*1024)
	for {
		n, e := l.tc.Read(buf)
		out = append(out, buf[:n]...)
		if e != nil {
			var again errAgain
			if errors.As(e, &again) {
				return out, nil
			}
			return out, e
		}
	}
}
