package iosaccess_test

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/iosaccess"
)

func TestLoadOrCreateCertificateGeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()

	b1, err := iosaccess.LoadOrCreateCertificate(dir, "192.168.1.20")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	block, _ := pem.Decode(b1.CertPEM)
	if block == nil {
		t.Fatalf("cert not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	if cert.PublicKeyAlgorithm != x509.ECDSA {
		t.Fatalf("want ECDSA key, got %v", cert.PublicKeyAlgorithm)
	}
	found := false
	for _, ip := range cert.IPAddresses {
		if ip.String() == "192.168.1.20" {
			found = true
		}
	}
	if !found {
		t.Fatalf("SAN missing address, got IPs=%v DNS=%v", cert.IPAddresses, cert.DNSNames)
	}
	if len(cert.IPAddresses) != 1 || len(cert.DNSNames) != 0 {
		t.Fatalf("SAN must contain exactly the selected address, got IPs=%v DNS=%v", cert.IPAddresses, cert.DNSNames)
	}

	// Key file permissions are owner-only.
	_, keyPath := certPathsForTest(dir, "192.168.1.20")
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode = %v, want 0600", info.Mode().Perm())
	}

	// Re-calling reuses the persisted certificate (same fingerprint) rather
	// than silently rotating it.
	b2, err := iosaccess.LoadOrCreateCertificate(dir, "192.168.1.20")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if b1.SPKIFingerprint != b2.SPKIFingerprint {
		t.Fatalf("certificate was regenerated on reload instead of reused")
	}

	// A different address gets its own certificate.
	b3, err := iosaccess.LoadOrCreateCertificate(dir, "192.168.1.21")
	if err != nil {
		t.Fatalf("create for second address: %v", err)
	}
	if b3.SPKIFingerprint == b1.SPKIFingerprint {
		t.Fatalf("different addresses must not share a certificate")
	}
}

func TestResetCertificateRotates(t *testing.T) {
	dir := t.TempDir()
	before, err := iosaccess.LoadOrCreateCertificate(dir, "10.0.0.5")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := iosaccess.ResetCertificate(dir, "10.0.0.5"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	after, err := iosaccess.LoadOrCreateCertificate(dir, "10.0.0.5")
	if err != nil {
		t.Fatalf("recreate: %v", err)
	}
	if before.SPKIFingerprint == after.SPKIFingerprint {
		t.Fatalf("reset must rotate the certificate/fingerprint")
	}
}

func TestResetCertificateOnAbsentFilesIsNotAnError(t *testing.T) {
	if err := iosaccess.ResetCertificate(t.TempDir(), "10.0.0.5"); err != nil {
		t.Fatalf("reset on absent files: %v", err)
	}
}

// certPathsForTest mirrors the package's unexported certificatePaths
// derivation closely enough for this white-box-adjacent assertion (key file
// permissions) without exporting internal layout.
func certPathsForTest(dir, address string) (string, string) {
	safe := strings.NewReplacer(":", "_", "%", "_", "/", "_").Replace(address)
	return filepath.Join(dir, "ios-access-"+safe+".crt"), filepath.Join(dir, "ios-access-"+safe+".key")
}
