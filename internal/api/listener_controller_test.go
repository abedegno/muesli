package api_test

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/iosaccess"
)

// fakeSource is an injectable iosaccess.InterfaceAddressSource whose
// snapshot can be swapped mid-test to simulate an address disappearing or
// moving interfaces.
type fakeSource struct {
	mu    sync.Mutex
	pairs []iosaccess.InterfaceAddressPair
	err   error
}

func (f *fakeSource) set(pairs []iosaccess.InterfaceAddressPair) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pairs = pairs
}

func (f *fakeSource) Snapshot(context.Context) ([]iosaccess.InterfaceAddressPair, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	out := make([]iosaccess.InterfaceAddressPair, len(f.pairs))
	copy(out, f.pairs)
	return out, nil
}

// loopbackPair uses 127.0.0.1 — not itself an eligible RFC1918/ULA address,
// but ListenerController only checks snapshot membership, not eligibility
// (production only ever feeds it real eligible pairs via
// iosaccess.SystemInterfaceAddressSource) — this lets the test bind a real
// socket without depending on the sandbox having a routable private LAN
// interface.
var loopbackPair = iosaccess.InterfaceAddressPair{InterfaceName: "lo0", Address: "127.0.0.1", Family: iosaccess.IPv4}

func pinnedHTTPSClient(t *testing.T, fingerprintHex string) *http.Client {
	t.Helper()
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // pinned via VerifyPeerCertificate below, exactly like the iOS client's delegate.
				VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
					if len(rawCerts) == 0 {
						return fmt.Errorf("no certificate presented")
					}
					cert, err := x509.ParseCertificate(rawCerts[0])
					if err != nil {
						return err
					}
					got := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
					if fmt.Sprintf("%x", got) != fingerprintHex {
						return fmt.Errorf("SPKI fingerprint mismatch")
					}
					return nil
				},
			},
		},
	}
}

func TestListenerControllerEnableBindsExactAddressServesRouterAndTLSPins(t *testing.T) {
	t.Parallel()
	srv := api.NewServer(api.Deps{})
	source := &fakeSource{pairs: []iosaccess.InterfaceAddressPair{loopbackPair}}
	ctrl := api.NewListenerController(srv, t.TempDir(), source)
	t.Cleanup(func() { _ = ctrl.Disable() })

	info, err := ctrl.Enable(context.Background(), loopbackPair)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if info.Port == 0 {
		t.Fatalf("expected a nonzero ephemeral port")
	}
	if info.Origin != fmt.Sprintf("https://127.0.0.1:%d", info.Port) {
		t.Fatalf("unexpected origin %q", info.Origin)
	}
	if info.FingerprintHex == "" || info.Phrase == "" {
		t.Fatalf("expected fingerprint/phrase, got %+v", info)
	}

	client := pinnedHTTPSClient(t, info.FingerprintHex)
	resp, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/healthz", info.Port))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var body map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Fatalf("unexpected body %+v", body)
	}

	// A mismatched fingerprint must be rejected (pinning is real, not decorative).
	badClient := pinnedHTTPSClient(t, "0000000000000000000000000000000000000000000000000000000000000000")
	if _, err := badClient.Get(fmt.Sprintf("https://127.0.0.1:%d/healthz", info.Port)); err == nil {
		t.Fatalf("expected TLS pin mismatch to be rejected")
	}
}

func TestListenerControllerRejectsGoneCandidate(t *testing.T) {
	t.Parallel()
	srv := api.NewServer(api.Deps{})
	source := &fakeSource{pairs: nil} // the candidate is not present
	ctrl := api.NewListenerController(srv, t.TempDir(), source)

	if _, err := ctrl.Enable(context.Background(), loopbackPair); err == nil {
		t.Fatalf("expected enable to fail when the candidate is not in the snapshot")
	}
	if enabled, _, _ := ctrl.Status(); enabled {
		t.Fatalf("must not report enabled after a rejected enable")
	}
}

func TestListenerControllerDisableClosesListener(t *testing.T) {
	t.Parallel()
	srv := api.NewServer(api.Deps{})
	source := &fakeSource{pairs: []iosaccess.InterfaceAddressPair{loopbackPair}}
	ctrl := api.NewListenerController(srv, t.TempDir(), source)

	info, err := ctrl.Enable(context.Background(), loopbackPair)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := ctrl.Disable(); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if enabled, _, _ := ctrl.Status(); enabled {
		t.Fatalf("expected disabled")
	}

	client := pinnedHTTPSClient(t, info.FingerprintHex)
	if _, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/healthz", info.Port)); err == nil {
		t.Fatalf("expected connection to a disabled listener to fail")
	}

	// Disable is idempotent.
	if err := ctrl.Disable(); err != nil {
		t.Fatalf("second disable: %v", err)
	}
}

func TestListenerControllerWatchdogClosesOnAddressDisappearance(t *testing.T) {
	t.Parallel()
	srv := api.NewServer(api.Deps{})
	source := &fakeSource{pairs: []iosaccess.InterfaceAddressPair{loopbackPair}}
	ctrl := api.NewListenerControllerWithWatchInterval(srv, t.TempDir(), source, 20*time.Millisecond)

	info, err := ctrl.Enable(context.Background(), loopbackPair)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}

	// The candidate disappears from the live snapshot (e.g. Wi-Fi dropped).
	source.set(nil)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if enabled, unusable, _ := ctrl.Status(); !enabled && unusable {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	enabled, unusable, _ := ctrl.Status()
	if enabled || !unusable {
		t.Fatalf("expected watchdog to close and mark unusable, got enabled=%v unusable=%v", enabled, unusable)
	}

	client := pinnedHTTPSClient(t, info.FingerprintHex)
	if _, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/healthz", info.Port)); err == nil {
		t.Fatalf("expected the watchdog-closed listener to reject new connections")
	}

	// The address reappearing must NOT auto-rebind — only an explicit Enable does.
	source.set([]iosaccess.InterfaceAddressPair{loopbackPair})
	time.Sleep(100 * time.Millisecond)
	if enabled, _, _ := ctrl.Status(); enabled {
		t.Fatalf("must not auto-rebind without an explicit Enable")
	}

	// A fresh Enable succeeds and clears the unusable flag.
	if _, err := ctrl.Enable(context.Background(), loopbackPair); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if enabled, unusable, _ := ctrl.Status(); !enabled || unusable {
		t.Fatalf("expected re-enable to succeed cleanly, got enabled=%v unusable=%v", enabled, unusable)
	}
	_ = ctrl.Disable()
}

// PEM sanity: the certificate served really is the one LoadOrCreateCertificate
// persisted (i.e. the controller isn't generating an unrelated ad hoc cert).
func TestListenerControllerServesPersistedCertificate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pre, err := iosaccess.LoadOrCreateCertificate(dir, loopbackPair.Address)
	if err != nil {
		t.Fatalf("pre-create cert: %v", err)
	}

	srv := api.NewServer(api.Deps{})
	source := &fakeSource{pairs: []iosaccess.InterfaceAddressPair{loopbackPair}}
	ctrl := api.NewListenerController(srv, dir, source)
	info, err := ctrl.Enable(context.Background(), loopbackPair)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	t.Cleanup(func() { _ = ctrl.Disable() })

	if info.FingerprintHex != fmt.Sprintf("%x", pre.SPKIFingerprint) {
		t.Fatalf("controller generated a different certificate than the one already persisted for this address")
	}

	block, _ := pem.Decode(pre.CertPEM)
	if block == nil {
		t.Fatalf("bad PEM")
	}
}
