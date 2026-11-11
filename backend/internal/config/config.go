package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/argon2"
)

type Config struct {
	Listen               string   `json:"listen"`
	HTTPListen           string   `json:"httpListen"`
	NetworkTrust         bool     `json:"networkTrust"`
	ManagementInterfaces []string `json:"managementInterfaces"`
	TrustedHosts         []string `json:"trustedHosts,omitempty"`
	Broker               string   `json:"broker"`
	StateDir             string   `json:"stateDir"`
	Username             string   `json:"username,omitempty"`
	Salt                 []byte   `json:"salt,omitempty"`
	PasswordHash         []byte   `json:"passwordHash,omitempty"`
	Certificate          string   `json:"certificate"`
	Key                  string   `json:"key"`
}

func Hash(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 2, 32*1024, 2, 32)
}
func Atomic(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".ncc-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Rename(name, path); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(path))
	if e == nil {
		defer d.Close()
		_ = d.Sync()
	}
	return nil
}
func Load(path string) (Config, error) {
	var c Config
	b, e := os.ReadFile(path)
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(b, &c)
	if e != nil {
		return c, e
	}
	h, _, e := net.SplitHostPort(c.Listen)
	if e != nil || net.ParseIP(h) == nil || net.ParseIP(h).IsUnspecified() {
		return c, errors.New("listen must be an explicit management IP")
	}
	if c.HTTPListen != "" {
		h, _, e = net.SplitHostPort(c.HTTPListen)
		if e != nil || net.ParseIP(h) == nil || net.ParseIP(h).IsUnspecified() {
			return c, errors.New("httpListen must be an explicit management IP")
		}
	}
	if !c.NetworkTrust && (len(c.PasswordHash) != 32 || len(c.Salt) != 16) {
		return c, errors.New("invalid credentials")
	}
	return c, nil
}
func Setup(dir, password string) (string, error) {
	if _, e := os.Stat(filepath.Join(dir, "config.json")); !os.IsNotExist(e) {
		return "", errors.New("configuration already exists or cannot be checked")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		return "", e
	}
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return "", e
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if e != nil {
		return "", e
	}
	tmpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "NP1 Control Center"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}}
	der, e := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if e != nil {
		return "", e
	}
	kb, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		return "", e
	}
	certPath := filepath.Join(dir, "tls.pem")
	keyPath := filepath.Join(dir, "tls.key")
	if e = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
		return "", e
	}
	if e = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb}), 0600); e != nil {
		return "", e
	}
	c := Config{Listen: "127.0.0.1:443", HTTPListen: "127.0.0.1:80", NetworkTrust: true, ManagementInterfaces: []string{"wlan0", "bridge0", "tailscale0"}, Broker: "/run/np1-control/broker.sock", StateDir: filepath.Join(dir, "..", "state"), Certificate: certPath, Key: keyPath}
	if e = Atomic(filepath.Join(dir, "config.json"), c); e != nil {
		return "", e
	}
	fp := sha256.Sum256(der)
	return hex.EncodeToString(fp[:]), nil
}
