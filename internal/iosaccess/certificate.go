package iosaccess

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// certValidity is how long a generated self-signed certificate is valid for.
// It is regenerated automatically once expired on next enable, and can
// always be forced early via Reset.
const certValidity = 397 * 24 * time.Hour // just under the common 398-day CA/B ceiling

// keyFileMode restricts the private key to owner read/write only.
const keyFileMode = 0o600

// CertificateBundle is a generated (or loaded) ECDSA P-256 self-signed
// certificate whose SAN is exactly one address, plus the data derived from
// it that pairing needs.
type CertificateBundle struct {
	CertPEM         []byte
	KeyPEM          []byte
	Address         string
	NotAfter        time.Time
	SPKIFingerprint [32]byte
}

// certificatePaths returns the deterministic cert/key file paths for one
// address inside dir (the Electron app's application-data directory). One
// address gets one persistent key/cert pair so re-selecting a previously
// used address does not require regenerating trust from scratch.
func certificatePaths(dir, address string) (certPath, keyPath string) {
	safe := strings.NewReplacer(":", "_", "%", "_", "/", "_").Replace(address)
	return filepath.Join(dir, "ios-access-"+safe+".crt"),
		filepath.Join(dir, "ios-access-"+safe+".key")
}

// LoadOrCreateCertificate returns the persisted certificate for address if
// one exists and is still valid, generating and persisting a fresh ECDSA
// P-256 self-signed certificate (SAN = address only) otherwise. The private
// key file is written with owner-only permissions and is never returned to
// any caller that would log or transmit it wholesale — callers needing the
// fingerprint/phrase should use CertificateBundle's derived fields, not
// KeyPEM.
func LoadOrCreateCertificate(dir, address string) (CertificateBundle, error) {
	certPath, keyPath := certificatePaths(dir, address)

	if bundle, err := loadCertificate(certPath, keyPath, address); err == nil {
		return bundle, nil
	}

	bundle, err := generateCertificate(address)
	if err != nil {
		return CertificateBundle{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return CertificateBundle{}, fmt.Errorf("create cert dir: %w", err)
	}
	if err := os.WriteFile(certPath, bundle.CertPEM, 0o644); err != nil {
		return CertificateBundle{}, fmt.Errorf("write cert: %w", err)
	}
	if err := os.WriteFile(keyPath, bundle.KeyPEM, keyFileMode); err != nil {
		return CertificateBundle{}, fmt.Errorf("write key: %w", err)
	}
	return bundle, nil
}

// ResetCertificate deletes any persisted certificate/key for address so the
// next LoadOrCreateCertificate call generates and persists a fresh one,
// invalidating every existing pairing for that address ("Reset iOS access"
// in the accepted spec).
func ResetCertificate(dir, address string) error {
	certPath, keyPath := certificatePaths(dir, address)
	if err := os.Remove(certPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(keyPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func loadCertificate(certPath, keyPath, address string) (CertificateBundle, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return CertificateBundle{}, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return CertificateBundle{}, err
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return CertificateBundle{}, errors.New("invalid cert PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return CertificateBundle{}, err
	}
	if time.Now().After(cert.NotAfter) {
		return CertificateBundle{}, errors.New("certificate expired")
	}
	if !certCoversAddress(cert, address) {
		return CertificateBundle{}, errors.New("certificate SAN does not match address")
	}
	return CertificateBundle{
		CertPEM:         certPEM,
		KeyPEM:          keyPEM,
		Address:         address,
		NotAfter:        cert.NotAfter,
		SPKIFingerprint: spkiFingerprint(cert),
	}, nil
}

func certCoversAddress(cert *x509.Certificate, address string) bool {
	ip := net.ParseIP(address)
	for _, candidate := range cert.IPAddresses {
		if ip != nil && candidate.Equal(ip) {
			return true
		}
	}
	for _, dns := range cert.DNSNames {
		if dns == address {
			return true
		}
	}
	return false
}

// generateCertificate creates a fresh ECDSA P-256 key and a self-signed
// certificate whose only SAN is address (an IP SAN when address parses as an
// IP, matching every use in this package since candidates are always literal
// addresses, never hostnames).
func generateCertificate(address string) (CertificateBundle, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return CertificateBundle{}, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return CertificateBundle{}, err
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: address},
		NotBefore:    time.Now().Add(-5 * time.Minute),
		NotAfter:     time.Now().Add(certValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(address); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{address}
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return CertificateBundle{}, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return CertificateBundle{}, err
	}

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return CertificateBundle{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return CertificateBundle{
		CertPEM:         certPEM,
		KeyPEM:          keyPEM,
		Address:         address,
		NotAfter:        cert.NotAfter,
		SPKIFingerprint: spkiFingerprint(cert),
	}, nil
}

// spkiFingerprint is the SHA-256 digest of the certificate's
// SubjectPublicKeyInfo — the value pairing exposes for pinning, distinct
// from (and much more useful than) a whole-certificate hash because it
// survives a same-key certificate renewal.
func spkiFingerprint(cert *x509.Certificate) [32]byte {
	return sha256.Sum256(cert.RawSubjectPublicKeyInfo)
}
