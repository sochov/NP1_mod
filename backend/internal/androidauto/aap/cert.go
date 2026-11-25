package aap

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// LoadOrCreateCert returns the phone certificate stored in dir. An owner may
// place their own phone.crt/phone.key there; otherwise a self-signed pair is
// created once. Head unit apps accept it; factory head units that check the
// certificate chain need an owner-supplied one.
func LoadOrCreateCert(dir string) (tls.Certificate, bool, error) {
	crt, key := filepath.Join(dir, "phone.crt"), filepath.Join(dir, "phone.key")
	if c, e := tls.LoadX509KeyPair(crt, key); e == nil {
		return c, !isSelfMade(c), nil
	} else if !errors.Is(e, os.ErrNotExist) {
		return tls.Certificate{}, false, e
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return tls.Certificate{}, false, e
	}
	priv, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		return tls.Certificate{}, false, e
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: selfMadeCN, Organization: []string{"NP1 Control Center"}},
		NotBefore:    time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2049, 12, 31, 0, 0, 0, 0, time.UTC),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, e := x509.CreateCertificate(rand.Reader, tpl, tpl, &priv.PublicKey, priv)
	if e != nil {
		return tls.Certificate{}, false, e
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	crtPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if e = writeAtomic(key, keyPEM, 0600); e != nil {
		return tls.Certificate{}, false, e
	}
	if e = writeAtomic(crt, crtPEM, 0644); e != nil {
		return tls.Certificate{}, false, e
	}
	c, e := tls.X509KeyPair(crtPEM, keyPEM)
	return c, false, e
}

const selfMadeCN = "NP1 Android Auto projection"

func isSelfMade(c tls.Certificate) bool {
	if len(c.Certificate) == 0 {
		return true
	}
	x, e := x509.ParseCertificate(c.Certificate[0])
	return e != nil || x.Subject.CommonName == selfMadeCN
}

func writeAtomic(path string, b []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	f, e := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if e != nil {
		return e
	}
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	if ce := f.Close(); e == nil {
		e = ce
	}
	if e != nil {
		os.Remove(tmp)
		return e
	}
	return os.Rename(tmp, path)
}
