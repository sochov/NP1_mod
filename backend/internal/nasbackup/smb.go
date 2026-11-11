package nasbackup

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	smb2 "github.com/hirochachacha/go-smb2"
)

type SMBRemote struct {
	config     Config
	connection net.Conn
	session    *smb2.Session
	share      *smb2.Share
	stop       func() bool
	cancel     context.CancelFunc
}

var generatedName = regexp.MustCompile(`(?i)^(?:ncc-[0-9a-f]{64}\.part|FILE[ME]_[0-9]{6}_[0-9]{6}_[0-9]+-[0-9a-f]{64}\.MP4)$`)

// Connect authenticates and checks an already-existing target directory. It
// does not mount a filesystem, create directories, or save OS credentials.
func connectSMB(parent context.Context, c Config, options TransportOptions) (*SMBRemote, error) {
	if c.host == "" {
		return nil, errors.New("NAS_CONFIG_INVALID")
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	connection, err := options.dial(ctx, "tcp", net.JoinHostPort(c.host, strconv.Itoa(c.port)))
	if err != nil {
		contextError := ctx.Err()
		cancel()
		return nil, safeError(contextError, "NAS_CONNECT_FAILED")
	}
	r := &SMBRemote{config: c, connection: connection, cancel: cancel}
	r.stop = context.AfterFunc(ctx, func() { _ = connection.Close() })
	_ = connection.SetDeadline(time.Now().Add(10 * time.Second))
	domain, user := "", c.username
	if d, u, ok := strings.Cut(user, `\`); ok {
		domain, user = d, u
	}
	dialer := smb2.Dialer{Negotiator: smb2.Negotiator{RequireMessageSigning: true}, Initiator: &smb2.NTLMInitiator{User: user, Password: c.password, Domain: domain}}
	r.session, err = dialer.DialContext(ctx, connection)
	if err == nil {
		r.share, err = r.session.WithContext(ctx).Mount(c.share)
	}
	if err == nil {
		err = r.checkDirectory(ctx)
	}
	if err != nil {
		contextError := ctx.Err()
		r.Close()
		return nil, safeError(contextError, "NAS_AUTH_OR_DIRECTORY_FAILED")
	}
	_ = connection.SetDeadline(time.Time{})
	return r, nil
}
func (r *SMBRemote) TargetKey() string { return r.config.targetKey() }
func (r *SMBRemote) String() string    { return "NAS SMB session [redacted]" }
func (r *SMBRemote) GoString() string  { return r.String() }
func (r *SMBRemote) Close() error {
	if r.stop != nil {
		r.stop()
	}
	if r.cancel != nil {
		r.cancel()
	}
	// Closing TCP first bounds cleanup even when a server ignores LOGOFF.
	if r.connection != nil {
		return r.connection.Close()
	}
	return nil
}
func (r *SMBRemote) checkDirectory(ctx context.Context) error {
	current := ""
	for _, component := range strings.Split(r.config.directory, "/") {
		current = path.Join(current, component)
		info, err := r.share.WithContext(ctx).Lstat(strings.ReplaceAll(current, "/", `\`))
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("NAS_TARGET_DIRECTORY_UNSAFE")
		}
	}
	return nil
}
func (r *SMBRemote) filename(ctx context.Context, name string) (string, error) {
	if !generatedName.MatchString(name) {
		return "", errors.New("NAS_TARGET_PATH_INVALID")
	}
	if err := r.checkDirectory(ctx); err != nil {
		return "", err
	}
	return strings.ReplaceAll(path.Join(r.config.directory, name), "/", `\`), nil
}
func smbInfo(info os.FileInfo) RemoteInfo {
	identity := ""
	if value, ok := info.Sys().(*smb2.FileStat); ok && !value.CreationTime.IsZero() {
		identity = value.CreationTime.UTC().Format(time.RFC3339Nano)
	}
	return RemoteInfo{info.Size(), info.Mode().IsRegular(), identity}
}
func (r *SMBRemote) Stat(ctx context.Context, name string) (RemoteInfo, error) {
	filename, err := r.filename(ctx, name)
	if err != nil {
		return RemoteInfo{}, err
	}
	info, err := r.share.WithContext(ctx).Lstat(filename)
	if err != nil {
		return RemoteInfo{}, err
	}
	return smbInfo(info), nil
}
func (r *SMBRemote) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	filename, err := r.filename(ctx, name)
	if err != nil {
		return nil, err
	}
	info, err := r.share.WithContext(ctx).Lstat(filename)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("NAS_REMOTE_NOT_REGULAR")
	}
	return r.share.WithContext(ctx).Open(filename)
}

type smbPart struct{ *smb2.File }

func (f smbPart) Stat() (RemoteInfo, error) {
	info, err := f.File.Stat()
	if err != nil {
		return RemoteInfo{}, err
	}
	return smbInfo(info), nil
}
func (r *SMBRemote) OpenPart(ctx context.Context, name string, exclusive bool) (Part, error) {
	filename, err := r.filename(ctx, name)
	if err != nil {
		return nil, err
	}
	flags := os.O_RDWR
	if exclusive {
		flags |= os.O_CREATE | os.O_EXCL
	} else {
		if info, e := r.share.WithContext(ctx).Lstat(filename); e != nil || !info.Mode().IsRegular() {
			return nil, errors.New("NAS_PART_UNSAFE")
		}
	}
	file, err := r.share.WithContext(ctx).OpenFile(filename, flags, 0600)
	if err != nil {
		return nil, err
	}
	return smbPart{file}, nil
}
func (r *SMBRemote) RenameNoReplace(ctx context.Context, from, to string) error {
	a, err := r.filename(ctx, from)
	if err != nil {
		return err
	}
	b, err := r.filename(ctx, to)
	if err != nil {
		return err
	}
	// v1.1.0 Share.Rename sends FileRenameInformation.ReplaceIfExists=0.
	return r.share.WithContext(ctx).Rename(a, b)
}
