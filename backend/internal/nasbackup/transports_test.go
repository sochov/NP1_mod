package nasbackup

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func TestSFTPTransportUsesPinnedHostKeyAndNoOverwriteRename(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.Mkdir(filepath.Join(dir, "target"), 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverConfig := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if c.User() != "fixture-user" || string(password) != "fixture-secret" {
			return nil, fmt.Errorf("fixture auth")
		}
		return nil, nil
	}}
	serverConfig.AddHostKey(signer)
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				_, channels, requests, err := ssh.NewServerConn(connection, serverConfig)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(requests)
				for channel := range channels {
					if channel.ChannelType() != "session" {
						_ = channel.Reject(ssh.UnknownChannelType, "unsupported")
						continue
					}
					stream, requests, err := channel.Accept()
					if err != nil {
						return
					}
					go func() {
						for request := range requests {
							if request.Type == "subsystem" {
								_ = request.Reply(true, nil)
							}
						}
					}()
					server, err := sftp.NewServer(stream, sftp.WithServerWorkingDirectory(dir))
					if err == nil {
						_ = server.Serve()
						_ = server.Close()
					}
				}
			}()
		}
	}()
	input := testInput()
	input.Protocol = "sftp"
	input.Share = ""
	input.Directory = "target"
	input.HostKeySHA256 = ssh.FingerprintSHA256(signer.PublicKey())
	cfg, err := NewConfig(input)
	if err != nil {
		t.Fatal(err)
	}
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "nas.example.test:22" {
			t.Error("unbound endpoint changed")
		}
		return (&net.Dialer{}).DialContext(ctx, network, listener.Addr().String())
	}
	remote, err := ConnectWithTransport(context.Background(), cfg, TransportOptions{DialContext: dial})
	if err != nil {
		t.Fatal("SFTP connect", err)
	}
	defer remote.Close()
	engine, err := NewEngine(t.TempDir(), remote)
	if err != nil {
		t.Fatal(err)
	}
	source := sourceFixture(t, originalTestBytes())
	result, err := engine.Backup(context.Background(), source)
	if err != nil || result.Status != "complete" {
		t.Fatalf("SFTP result=%+v err=%v", result, err)
	}
	result, err = engine.Backup(context.Background(), source)
	if err != nil || result.Status != "already-complete" {
		t.Fatal("SFTP duplicate", err)
	}
	input.HostKeySHA256 = "SHA256:" + strings.Repeat("A", 43)
	bad, err := NewConfig(input)
	if err != nil {
		t.Fatal(err)
	}
	if failed, err := ConnectWithTransport(context.Background(), bad, TransportOptions{DialContext: dial}); err == nil {
		failed.Close()
		t.Fatal("untrusted host key accepted")
	}
}

func TestWebDAVTransportConditionalStreamingMoveAndDuplicate(t *testing.T) {
	var mu sync.Mutex
	files := map[string][]byte{}
	versions := map[string]int{}
	moves := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "fixture-user" || password != "fixture-secret" {
			w.WriteHeader(401)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/private/ncc/") {
			t.Error("request escaped configured directory")
			w.WriteHeader(403)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		data, exists := files[r.URL.Path]
		etag := fmt.Sprintf(`"v%d"`, versions[r.URL.Path])
		switch r.Method {
		case "PROPFIND":
			if r.Header.Get("Depth") != "0" {
				t.Error("directory listing not bounded")
			}
			directory := r.URL.Path == "/private/ncc/"
			if !exists && !directory {
				w.WriteHeader(404)
				return
			}
			resourceType := ""
			if directory {
				resourceType = "<d:collection/>"
			}
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(207)
			href := new(strings.Builder)
			_ = xml.EscapeText(href, []byte(r.URL.Path))
			fmt.Fprintf(w, `<d:multistatus xmlns:d="DAV:"><d:response><d:href>%s</d:href><d:propstat><d:prop><d:getcontentlength>%d</d:getcontentlength><d:getetag>%s</d:getetag><d:resourcetype>%s</d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`, href, len(data), etag, resourceType)
		case http.MethodPut:
			if r.Header.Get("If-None-Match") == "*" && exists || r.Header.Get("If-Match") != "" && r.Header.Get("If-Match") != etag {
				w.WriteHeader(412)
				return
			}
			value, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
			if err != nil {
				w.WriteHeader(400)
				return
			}
			files[r.URL.Path] = value
			versions[r.URL.Path]++
			if exists {
				w.WriteHeader(204)
			} else {
				w.WriteHeader(201)
			}
		case http.MethodGet:
			if !exists {
				w.WriteHeader(404)
				return
			}
			_, _ = w.Write(data)
		case "MOVE":
			if r.Header.Get("Overwrite") != "F" {
				t.Error("MOVE could overwrite")
			}
			target, err := url.Parse(r.Header.Get("Destination"))
			if err != nil || !strings.HasPrefix(target.Path, "/private/ncc/") {
				w.WriteHeader(400)
				return
			}
			if _, ok := files[target.Path]; ok {
				w.WriteHeader(412)
				return
			}
			files[target.Path] = data
			versions[target.Path] = versions[r.URL.Path]
			delete(files, r.URL.Path)
			delete(versions, r.URL.Path)
			moves++
			w.WriteHeader(201)
		default:
			w.WriteHeader(405)
		}
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	input := testInput()
	input.Protocol = "webdav"
	input.Scheme = "http"
	cfg, err := NewConfig(input)
	if err != nil {
		t.Fatal(err)
	}
	dials := 0
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		dials++
		if address != "nas.example.test:80" {
			t.Error("HTTP dial did not use configured endpoint")
		}
		return (&net.Dialer{}).DialContext(ctx, network, endpoint.Host)
	}
	remote, err := ConnectWithTransport(context.Background(), cfg, TransportOptions{DialContext: dial})
	if err != nil {
		t.Fatal("WebDAV connect", err)
	}
	defer remote.Close()
	engine, err := NewEngine(t.TempDir(), remote)
	if err != nil {
		t.Fatal(err)
	}
	source := sourceFixture(t, originalTestBytes())
	result, err := engine.Backup(context.Background(), source)
	if err != nil || result.Status != "complete" {
		t.Fatalf("WebDAV result=%+v error=%v", result, err)
	}
	result, err = engine.Backup(context.Background(), source)
	if err != nil || result.Status != "already-complete" {
		t.Fatal("WebDAV duplicate", err)
	}
	if moves != 1 || dials == 0 {
		t.Fatal("transport did not use conditional MOVE/injected dialer")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(files) != 1 {
		t.Fatal("unexpected file count")
	}
	for _, data := range files {
		if strconv.Itoa(len(data)) != result.Bytes {
			t.Fatal("byte count changed")
		}
	}
}
