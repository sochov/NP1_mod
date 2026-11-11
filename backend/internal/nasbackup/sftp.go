package nasbackup

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type sftpRemote struct {
	config     Config
	connection net.Conn
	ssh        *ssh.Client
	client     *sftp.Client
	cancel     context.CancelFunc
	stop       func() bool
}

func (r *sftpRemote) String() string   { return "NAS SFTP session [redacted]" }
func (r *sftpRemote) GoString() string { return r.String() }
func connectSFTP(parent context.Context, c Config, options TransportOptions) (*sftpRemote, error) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	address := net.JoinHostPort(c.host, strconv.Itoa(c.port))
	connection, err := options.dial(ctx, "tcp", address)
	if err != nil {
		e := ctx.Err()
		cancel()
		return nil, safeError(e, "NAS_CONNECT_FAILED")
	}
	r := &sftpRemote{config: c, connection: connection, cancel: cancel}
	r.stop = context.AfterFunc(ctx, func() { _ = connection.Close() })
	_ = connection.SetDeadline(time.Now().Add(10 * time.Second))
	config := &ssh.ClientConfig{User: c.username, Auth: []ssh.AuthMethod{ssh.Password(c.password)}, Timeout: 10 * time.Second, HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if ssh.FingerprintSHA256(key) != c.hostKey {
			return errors.New("NAS_SFTP_HOSTKEY_MISMATCH")
		}
		return nil
	}}
	client, channels, requests, err := ssh.NewClientConn(connection, address, config)
	if err == nil {
		r.ssh = ssh.NewClient(client, channels, requests)
		r.client, err = sftp.NewClient(r.ssh, sftp.UseConcurrentReads(false), sftp.UseConcurrentWrites(false))
	}
	if err == nil {
		err = r.checkDirectory()
	}
	if err != nil {
		e := ctx.Err()
		r.Close()
		return nil, safeError(e, "NAS_SFTP_AUTH_OR_DIRECTORY_FAILED")
	}
	_ = connection.SetDeadline(time.Time{})
	return r, nil
}
func (r *sftpRemote) Close() error {
	if r.stop != nil {
		r.stop()
	}
	if r.cancel != nil {
		r.cancel()
	}
	if r.connection != nil {
		return r.connection.Close()
	}
	return nil
}
func (r *sftpRemote) TargetKey() string { return r.config.targetKey() }
func (r *sftpRemote) checkDirectory() error {
	current := ""
	if strings.HasPrefix(r.config.directory, "/") {
		current = "/"
	}
	for _, part := range strings.Split(strings.TrimPrefix(r.config.directory, "/"), "/") {
		current = path.Join(current, part)
		info, err := r.client.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("NAS_TARGET_DIRECTORY_UNSAFE")
		}
	}
	return nil
}
func (r *sftpRemote) filename(ctx context.Context, name string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if !generatedName.MatchString(name) {
		return "", errors.New("NAS_TARGET_PATH_INVALID")
	}
	if err := r.checkDirectory(); err != nil {
		return "", err
	}
	return path.Join(r.config.directory, name), nil
}
func (r *sftpRemote) Stat(ctx context.Context, name string) (RemoteInfo, error) {
	filename, err := r.filename(ctx, name)
	if err != nil {
		return RemoteInfo{}, err
	}
	info, err := r.client.Lstat(filename)
	if err != nil {
		return RemoteInfo{}, err
	}
	return RemoteInfo{info.Size(), info.Mode().IsRegular(), r.config.targetKey() + ":" + name}, nil
}
func (r *sftpRemote) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	filename, err := r.filename(ctx, name)
	if err != nil {
		return nil, err
	}
	info, err := r.client.Lstat(filename)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("NAS_REMOTE_NOT_REGULAR")
	}
	return r.client.Open(filename)
}

type sftpPart struct {
	*sftp.File
	identity string
}

func (f sftpPart) Stat() (RemoteInfo, error) {
	info, err := f.File.Stat()
	if err != nil {
		return RemoteInfo{}, err
	}
	return RemoteInfo{info.Size(), info.Mode().IsRegular(), f.identity}, nil
}
func (f sftpPart) Sync() error {
	err := f.File.Sync()
	var status *sftp.StatusError
	if errors.As(err, &status) && status.FxCode() == sftp.ErrSSHFxOpUnsupported {
		return nil
	}
	return err
}
func (r *sftpRemote) OpenPart(ctx context.Context, name string, exclusive bool) (Part, error) {
	filename, err := r.filename(ctx, name)
	if err != nil {
		return nil, err
	}
	flags := os.O_RDWR
	if exclusive {
		flags |= os.O_CREATE | os.O_EXCL
	} else {
		info, err := r.client.Lstat(filename)
		if err != nil || !info.Mode().IsRegular() {
			return nil, errors.New("NAS_PART_UNSAFE")
		}
	}
	file, err := r.client.OpenFile(filename, flags)
	if err != nil {
		return nil, err
	}
	return sftpPart{file, r.config.targetKey() + ":" + name}, nil
}
func (r *sftpRemote) RenameNoReplace(ctx context.Context, from, to string) error {
	a, err := r.filename(ctx, from)
	if err != nil {
		return err
	}
	b, err := r.filename(ctx, to)
	if err != nil {
		return err
	}
	return r.client.Rename(a, b)
}
