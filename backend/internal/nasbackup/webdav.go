package nasbackup

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

type webdavRemote struct {
	config    Config
	base      *url.URL
	client    *http.Client
	transport *http.Transport
	ctx       context.Context
	cancel    context.CancelFunc
}
type davProperties struct {
	Length string `xml:"getcontentlength"`
	ETag   string `xml:"getetag"`
	Type   struct {
		Collection *struct{} `xml:"collection"`
	} `xml:"resourcetype"`
}
type davMultiStatus struct {
	Responses []struct {
		Href       string `xml:"href"`
		Status     string `xml:"status"`
		Properties []struct {
			Status string        `xml:"status"`
			Value  davProperties `xml:"prop"`
		} `xml:"propstat"`
	} `xml:"response"`
}

func (r *webdavRemote) String() string   { return "NAS WebDAV session [redacted]" }
func (r *webdavRemote) GoString() string { return r.String() }
func connectWebDAV(parent context.Context, c Config, options TransportOptions) (Session, error) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	base := &url.URL{Scheme: c.scheme, Host: net.JoinHostPort(c.host, strconv.Itoa(c.port)), Path: "/" + strings.Trim(c.directory, "/") + "/"}
	transport := &http.Transport{Proxy: nil, DialContext: options.dial, DisableCompression: true, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 16384, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: options.RootCAs}}
	r := &webdavRemote{config: c, base: base, transport: transport, ctx: ctx, cancel: cancel}
	r.client = &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	_, _, collection, err := r.properties(ctx, base)
	if err != nil || !collection {
		r.Close()
		return nil, safeError(parent.Err(), "NAS_WEBDAV_DIRECTORY_FAILED")
	}
	return r, nil
}
func (r *webdavRemote) Close() error                   { r.cancel(); r.transport.CloseIdleConnections(); return nil }
func (r *webdavRemote) TargetKey() string              { return r.config.targetKey() }
func (r *webdavRemote) RestartOwnedPartFromZero() bool { return true }
func (r *webdavRemote) resource(name string) (*url.URL, error) {
	if !generatedName.MatchString(name) {
		return nil, errors.New("NAS_TARGET_PATH_INVALID")
	}
	out := *r.base
	out.Path = path.Join(r.base.Path, name)
	return &out, nil
}
func (r *webdavRemote) request(ctx context.Context, method string, target *url.URL, body io.Reader, headers map[string]string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, errors.New("NAS_WEBDAV_REQUEST_INVALID")
	}
	request.SetBasicAuth(r.config.username, r.config.password)
	for k, v := range headers {
		request.Header.Set(k, v)
	}
	response, err := r.client.Do(request)
	if err != nil {
		return nil, safeError(ctx.Err(), "NAS_WEBDAV_REQUEST_FAILED")
	}
	return response, nil
}
func (r *webdavRemote) properties(ctx context.Context, target *url.URL) (RemoteInfo, string, bool, error) {
	query := `<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:getcontentlength/><d:getetag/><d:resourcetype/></d:prop></d:propfind>`
	response, err := r.request(ctx, "PROPFIND", target, strings.NewReader(query), map[string]string{"Depth": "0", "Content-Type": "application/xml; charset=utf-8"})
	if err != nil {
		return RemoteInfo{}, "", false, err
	}
	defer response.Body.Close()
	if response.StatusCode == 404 {
		return RemoteInfo{}, "", false, os.ErrNotExist
	}
	if response.StatusCode != 207 {
		return RemoteInfo{}, "", false, errors.New("NAS_WEBDAV_STAT_FAILED")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 32769))
	if err != nil || len(raw) > 32768 {
		return RemoteInfo{}, "", false, errors.New("NAS_WEBDAV_RESPONSE_INVALID")
	}
	var multi davMultiStatus
	if xml.Unmarshal(raw, &multi) != nil || len(multi.Responses) > 16 {
		return RemoteInfo{}, "", false, errors.New("NAS_WEBDAV_RESPONSE_INVALID")
	}
	for _, entry := range multi.Responses {
		href, err := url.Parse(entry.Href)
		if err != nil || href.Host != "" && !strings.EqualFold(href.Host, target.Host) || path.Clean(href.Path) != path.Clean(target.Path) {
			continue
		}
		for _, prop := range entry.Properties {
			parts := strings.Fields(prop.Status)
			if len(parts) < 2 || !strings.HasPrefix(parts[1], "2") {
				continue
			}
			collection := prop.Value.Type.Collection != nil
			size := int64(0)
			if !collection {
				size, err = strconv.ParseInt(prop.Value.Length, 10, 64)
				if err != nil || size < 0 {
					return RemoteInfo{}, "", false, errors.New("NAS_WEBDAV_SIZE_INVALID")
				}
			}
			return RemoteInfo{size, !collection, r.config.targetKey() + ":" + path.Base(target.Path)}, prop.Value.ETag, collection, nil
		}
	}
	return RemoteInfo{}, "", false, errors.New("NAS_WEBDAV_RESPONSE_INVALID")
}
func (r *webdavRemote) Stat(ctx context.Context, name string) (RemoteInfo, error) {
	resource, err := r.resource(name)
	if err != nil {
		return RemoteInfo{}, err
	}
	info, _, _, err := r.properties(ctx, resource)
	return info, err
}
func (r *webdavRemote) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	resource, err := r.resource(name)
	if err != nil {
		return nil, err
	}
	response, err := r.request(ctx, http.MethodGet, resource, nil, nil)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != 200 {
		response.Body.Close()
		return nil, errors.New("NAS_WEBDAV_READ_FAILED")
	}
	return response.Body, nil
}

type webdavPart struct {
	remote     *webdavRemote
	ctx        context.Context
	name       string
	initial    RemoteInfo
	etag       string
	pipe       *io.PipeWriter
	done       chan error
	cancel     context.CancelFunc
	finished   bool
	completion error
}

func (f *webdavPart) Stat() (RemoteInfo, error) { return f.initial, nil }
func (f *webdavPart) Seek(offset int64, whence int) (int64, error) {
	if f.pipe != nil || whence != io.SeekStart || offset != 0 {
		return 0, errors.New("NAS_WEBDAV_RESTART_REQUIRED")
	}
	return 0, nil
}
func (f *webdavPart) start() error {
	if f.pipe != nil {
		return nil
	}
	resource, err := f.remote.resource(f.name)
	if err != nil {
		return err
	}
	reader, writer := io.Pipe()
	f.pipe = writer
	f.done = make(chan error, 1)
	ctx, cancel := context.WithCancel(f.ctx)
	f.cancel = cancel
	go func() {
		defer reader.Close()
		response, err := f.remote.request(ctx, http.MethodPut, resource, reader, map[string]string{"If-Match": f.etag, "Content-Type": "video/mp4"})
		if err == nil {
			if response.StatusCode != 200 && response.StatusCode != 201 && response.StatusCode != 204 {
				err = errors.New("NAS_WEBDAV_WRITE_CONFLICT")
			}
			response.Body.Close()
		}
		if err != nil {
			_ = reader.CloseWithError(err)
		}
		f.done <- err
	}()
	return nil
}
func (f *webdavPart) Write(data []byte) (int, error) {
	if err := f.start(); err != nil {
		return 0, err
	}
	return f.pipe.Write(data)
}
func (f *webdavPart) Sync() error {
	if f.finished {
		return f.completion
	}
	if err := f.start(); err != nil {
		return err
	}
	_ = f.pipe.Close()
	f.completion = <-f.done
	f.finished = true
	return f.completion
}
func (f *webdavPart) Close() error {
	if f.cancel != nil {
		f.cancel()
	}
	if f.pipe != nil && !f.finished {
		_ = f.pipe.CloseWithError(io.ErrClosedPipe)
		f.completion = <-f.done
		f.finished = true
	}
	return f.completion
}
func (r *webdavRemote) OpenPart(ctx context.Context, name string, exclusive bool) (Part, error) {
	resource, err := r.resource(name)
	if err != nil {
		return nil, err
	}
	if exclusive {
		response, err := r.request(ctx, http.MethodPut, resource, bytes.NewReader(nil), map[string]string{"If-None-Match": "*"})
		if err != nil {
			return nil, err
		}
		response.Body.Close()
		if response.StatusCode != 201 {
			return nil, errors.New("NAS_WEBDAV_CREATE_CONFLICT")
		}
	}
	info, etag, collection, err := r.properties(ctx, resource)
	if err != nil {
		return nil, err
	}
	if collection || !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) || strings.HasPrefix(etag, "W/") {
		return nil, errors.New("NAS_WEBDAV_STRONG_ETAG_REQUIRED")
	}
	return &webdavPart{remote: r, ctx: ctx, name: name, initial: info, etag: etag}, nil
}
func (r *webdavRemote) RenameNoReplace(ctx context.Context, from, to string) error {
	a, err := r.resource(from)
	if err != nil {
		return err
	}
	b, err := r.resource(to)
	if err != nil {
		return err
	}
	response, err := r.request(ctx, "MOVE", a, nil, map[string]string{"Destination": b.String(), "Overwrite": "F"})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 201 {
		return errors.New("NAS_WEBDAV_MOVE_CONFLICT")
	}
	return nil
}
