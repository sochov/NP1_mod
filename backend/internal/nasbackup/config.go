package nasbackup

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	coreconfig "github.com/np1-control-center/ncc/internal/config"
)

type ConfigInput struct {
	Revision      int    `json:"revision,omitempty"`
	Host          string `json:"host"`
	Share         string `json:"share"`
	Directory     string `json:"directory"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	Protocol      string `json:"protocol,omitempty"`
	Port          int    `json:"port,omitempty"`
	Scheme        string `json:"scheme,omitempty"`
	HostKeySHA256 string `json:"hostKeySHA256,omitempty"`
}

func (ConfigInput) String() string     { return "NAS configuration [redacted]" }
func (v ConfigInput) GoString() string { return v.String() }
func (ConfigInput) MarshalJSON() ([]byte, error) {
	return []byte(`{"configured":true,"credentials":"redacted"}`), nil
}

type Config struct {
	host, share, directory, username, password string
	revision                                   int
	protocol, scheme, hostKey                  string
	port                                       int
}
type ConfigView struct {
	Revision           int  `json:"revision"`
	Configured         bool `json:"configured"`
	UsernameConfigured bool `json:"usernameConfigured"`
	PasswordConfigured bool `json:"passwordConfigured"`
}

func (c Config) View() ConfigView {
	return ConfigView{c.revision, c.host != "", c.username != "", c.password != ""}
}

type TargetView struct {
	Host          string `json:"host"`
	Share         string `json:"share"`
	Directory     string `json:"directory"`
	Protocol      string `json:"protocol"`
	Port          int    `json:"port"`
	Scheme        string `json:"scheme,omitempty"`
	HostKeySHA256 string `json:"hostKeySHA256,omitempty"`
}

func (c Config) Target() TargetView {
	return TargetView{c.host, c.share, c.directory, c.protocol, c.port, c.scheme, c.hostKey}
}
func (c Config) Revision() int { return c.revision }
func (c Config) Changed(expected int, input ConfigInput) (Config, error) {
	if expected != c.revision {
		return Config{}, errors.New("NAS_REVISION_CONFLICT")
	}
	if expected < 0 || expected == int(^uint(0)>>1) {
		return Config{}, errors.New("NAS_CONFIG_INVALID")
	}
	if input.Host == "" {
		input.Host = c.host
	}
	if input.Share == "" {
		input.Share = c.share
	}
	if input.Directory == "" {
		input.Directory = c.directory
	}
	if input.Username == "" {
		input.Username = c.username
	}
	if input.Password == "" {
		input.Password = c.password
	}
	if input.Protocol == "" {
		input.Protocol = c.protocol
	}
	if input.Port == 0 && input.Protocol == c.protocol {
		input.Port = c.port
	}
	if input.Scheme == "" {
		input.Scheme = c.scheme
	}
	if input.HostKeySHA256 == "" {
		input.HostKeySHA256 = c.hostKey
	}
	input.Revision = expected + 1
	return NewConfig(input)
}
func (c Config) String() string               { return "NAS configuration [redacted]" }
func (c Config) GoString() string             { return c.String() }
func (c Config) MarshalJSON() ([]byte, error) { return json.Marshal(c.View()) }

var dnsHost = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9.-]{0,251}[a-zA-Z0-9])?$`)

func safeComponent(v string) bool {
	if v == "" || v == "." || v == ".." || len(v) > 255 || !utf8.ValidString(v) || strings.TrimSpace(v) != v || strings.HasSuffix(v, ".") || strings.ContainsAny(v, `<>:"/\|?*`) {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func NewConfig(v ConfigInput) (Config, error) {
	fail := func() (Config, error) { return Config{}, errors.New("NAS_CONFIG_INVALID") }
	if v.Revision < 0 {
		return fail()
	}
	if v.Protocol == "" {
		v.Protocol = "smb"
	}
	if v.Protocol != "smb" && v.Protocol != "sftp" && v.Protocol != "webdav" {
		return fail()
	}
	if v.Port == 0 {
		switch v.Protocol {
		case "smb":
			v.Port = 445
		case "sftp":
			v.Port = 22
		case "webdav":
			if v.Scheme == "http" {
				v.Port = 80
			} else {
				v.Port = 443
			}
		}
	}
	if v.Port < 1 || v.Port > 65535 {
		return fail()
	}
	if v.Protocol == "webdav" {
		if v.Scheme == "" {
			v.Scheme = "https"
		}
		if v.Scheme != "http" && v.Scheme != "https" {
			return fail()
		}
	}
	if v.Protocol == "sftp" {
		fingerprint, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(v.HostKeySHA256, "SHA256:"))
		if !strings.HasPrefix(v.HostKeySHA256, "SHA256:") || err != nil || len(fingerprint) != 32 {
			return Config{}, errors.New("NAS_SFTP_HOSTKEY_REQUIRED")
		}
	}
	if ip := net.ParseIP(v.Host); ip != nil {
		if !ip.IsGlobalUnicast() {
			return fail()
		}
	} else if !dnsHost.MatchString(v.Host) || strings.Contains(v.Host, "..") {
		return fail()
	}
	if v.Protocol == "smb" && (!safeComponent(v.Share) || len(v.Share) > 80) {
		return fail()
	}
	if v.Username == "" || len(v.Username) > 256 || strings.ContainsAny(v.Username, "\x00\r\n") || v.Password == "" || len(v.Password) > 1024 || !utf8.ValidString(v.Password) || strings.ContainsRune(v.Password, 0) {
		return fail()
	}
	directory := strings.ReplaceAll(v.Directory, `\`, "/")
	shareRoot := v.Protocol == "smb" && (directory == "" || directory == ".")
	if shareRoot {
		directory = "."
	}
	if directory == "" || len(directory) > 1024 || v.Protocol == "smb" && path.IsAbs(directory) || path.Clean(directory) != directory || strings.HasPrefix(directory, "//") {
		return fail()
	}
	for _, segment := range strings.Split(strings.TrimPrefix(directory, "/"), "/") {
		if shareRoot {
			break
		}
		if !safeComponent(segment) {
			return fail()
		}
	}
	return Config{host: strings.ToLower(v.Host), share: v.Share, directory: directory, username: v.Username, password: v.Password, revision: v.Revision, protocol: v.Protocol, port: v.Port, scheme: v.Scheme, hostKey: v.HostKeySHA256}, nil
}
func (c Config) targetKey() string {
	sum := sha256.Sum256([]byte(c.host + "\x00" + c.share + "\x00" + c.directory + "\x00" + c.username + "\x00" + c.protocol + "\x00" + strconv.Itoa(c.port) + "\x00" + c.scheme + "\x00" + c.hostKey))
	return hex.EncodeToString(sum[:])
}
func (c Config) TargetKey() string { return c.targetKey() }

// Disk credentials are serialized explicitly, never through redacted Config.
func SaveConfig(filename string, c Config) error {
	if c.host == "" {
		return errors.New("NAS_CONFIG_INVALID")
	}
	if err := prepareDirectory(filepath.Dir(filename)); err != nil {
		return err
	}
	if info, err := os.Lstat(filename); err == nil {
		if err = privateRegular(info); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return errors.New("NAS_CONFIG_UNAVAILABLE")
	}
	type disk ConfigInput
	return coreconfig.Atomic(filename, disk{Revision: c.revision, Host: c.host, Share: c.share, Directory: c.directory, Username: c.username, Password: c.password, Protocol: c.protocol, Port: c.port, Scheme: c.scheme, HostKeySHA256: c.hostKey})
}
func LoadConfig(filename string) (Config, error) {
	info, err := os.Lstat(filename)
	if err != nil || privateRegular(info) != nil {
		return Config{}, errors.New("NAS_CONFIG_UNAVAILABLE")
	}
	file, err := os.Open(filename)
	if err != nil {
		return Config{}, errors.New("NAS_CONFIG_UNAVAILABLE")
	}
	defer file.Close()
	current, err := file.Stat()
	if err != nil || !os.SameFile(info, current) {
		return Config{}, errors.New("NAS_CONFIG_CHANGED")
	}
	raw, err := io.ReadAll(io.LimitReader(file, 16385))
	if err != nil || len(raw) > 16384 {
		return Config{}, errors.New("NAS_CONFIG_INVALID")
	}
	var input ConfigInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		return Config{}, errors.New("NAS_CONFIG_INVALID")
	}
	return NewConfig(input)
}

func UpdateConfig(filename string, expected int, input ConfigInput) (Config, error) {
	if err := prepareDirectory(filepath.Dir(filename)); err != nil {
		return Config{}, err
	}
	unlock, err := lockState(filepath.Dir(filename))
	if err != nil {
		return Config{}, err
	}
	defer unlock()
	var previous Config
	if _, err = os.Lstat(filename); err == nil {
		previous, err = LoadConfig(filename)
		if err != nil {
			return Config{}, err
		}
	} else if !os.IsNotExist(err) {
		return Config{}, errors.New("NAS_CONFIG_UNAVAILABLE")
	}
	next, err := previous.Changed(expected, input)
	if err != nil {
		return Config{}, err
	}
	if err = SaveConfig(filename, next); err != nil {
		return Config{}, err
	}
	return next, nil
}

// ClaimController serializes config changes, probes and jobs across CLI/broker
// processes. Transfer journals have a separate lock in their state directory.
func ClaimController(directory string) (func(), error) {
	if err := prepareDirectory(directory); err != nil {
		return nil, err
	}
	return lockState(directory)
}

func prepareDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return errors.New("NAS_STATE_UNAVAILABLE")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("NAS_STATE_UNSAFE")
	}
	return privateDirectory(info)
}
func safeError(ctxError error, code string) error {
	if ctxError != nil {
		return ctxError
	}
	return errors.New(code)
}
