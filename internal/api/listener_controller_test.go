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
	// onSnapshot, if set, is invoked once by the next Snapshot call and then
	// cleared (one-shot) -- a test-only hook that lets a test pause a
	// watcher goroutine synchronously inside its ticker-branch Snapshot
	// call, deterministically reproducing the exact stale-watcher race
	// window (already past the select, mid-iteration, when a concurrent
	// Enable cancels it) instead of relying on real goroutine timing.
	onSnapshot func()
}

func (f *fakeSource) set(pairs []iosaccess.InterfaceAddressPair) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pairs = pairs
}

func (f *fakeSource) setOnSnapshot(hook func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onSnapshot = hook
}

func (f *fakeSource) Snapshot(context.Context) ([]iosaccess.InterfaceAddressPair, error) {
	f.mu.Lock()
	hook := f.onSnapshot
	f.onSnapshot = nil
	f.mu.Unlock()

	if hook != nil {
		hook()
	}

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
	// UnusableCh is a real production signal (internal/api.ListenerController),
	// closed by the watchdog goroutine itself exactly when it force-closes the
	// listener -- waiting on it, rather than polling Status() on a wall-clock
	// deadline, makes this test's synchronization deterministic.
	unusableCh := ctrl.UnusableCh()
	source.set(nil)

	select {
	case <-unusableCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for the watchdog to detect the disappeared address")
	}
	enabled, unusable, _ := ctrl.Status()
	if enabled || !unusable {
		t.Fatalf("expected watchdog to close and mark unusable, got enabled=%v unusable=%v", enabled, unusable)
	}

	client := pinnedHTTPSClient(t, info.FingerprintHex)
	if _, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/healthz", info.Port)); err == nil {
		t.Fatalf("expected the watchdog-closed listener to reject new connections")
	}

	// The address reappearing must NOT auto-rebind -- only an explicit Enable
	// does. The watchdog goroutine that just detected the disappearance has
	// already returned (closing unusableCh was its final act before that
	// return), so there is no live watcher left to react to this change --
	// Status is checked immediately, with nothing to wait on.
	source.set([]iosaccess.InterfaceAddressPair{loopbackPair})
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

func TestListenerControllerStaleWatchdogDoesNotCloseNewGeneration(t *testing.T) {
	t.Parallel()
	srv := api.NewServer(api.Deps{})
	source := &fakeSource{pairs: []iosaccess.InterfaceAddressPair{loopbackPair}}
	ctrl := api.NewListenerControllerWithWatchInterval(srv, t.TempDir(), source, 20*time.Millisecond)
	t.Cleanup(func() { _ = ctrl.Disable() })

	if _, err := ctrl.Enable(context.Background(), loopbackPair); err != nil {
		t.Fatalf("enable (first generation): %v", err)
	}
	firstUnusableCh := ctrl.UnusableCh()

	// Pause the first generation's watchdog goroutine synchronously inside
	// its next Snapshot call -- i.e. already past the ticker-branch select,
	// committed to processing this tick -- exactly the moment the reviewer
	// described: a concurrent Enable's cancellation cannot un-commit a
	// watcher that's already this far into an iteration.
	entered := make(chan struct{})
	release := make(chan struct{})
	source.setOnSnapshot(func() {
		close(entered)
		<-release
	})

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for the first generation's watchdog to enter Snapshot")
	}

	// While the stale watcher is blocked mid-iteration, re-enable on the
	// SAME address pair -- a new generation, with a new unusableCh, but an
	// address pair that Equal()s the stale watcher's captured one.
	second, err := ctrl.Enable(context.Background(), loopbackPair)
	if err != nil {
		t.Fatalf("enable (second generation): %v", err)
	}
	secondUnusableCh := ctrl.UnusableCh()
	if secondUnusableCh == firstUnusableCh {
		t.Fatalf("expected the second Enable to produce a distinct unusableCh")
	}

	// Now make the address look gone, and let the stale watcher's blocked
	// Snapshot call return -- it proceeds to its failure path believing
	// itself current, since its captured address pair still Equal()s
	// ctrl's (new) current pair.
	source.set(nil)
	close(release)

	select {
	case <-firstUnusableCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for the stale watchdog to finish its failure path")
	}

	// The stale watcher must not have torn down the second generation.
	if enabled, unusable, pair := ctrl.Status(); !enabled || unusable || pair == nil {
		t.Fatalf("stale first-generation watchdog closed the second generation: enabled=%v unusable=%v pair=%v", enabled, unusable, pair)
	}

	// And the second generation's listener is still actually serving.
	client := pinnedHTTPSClient(t, second.FingerprintHex)
	resp, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/healthz", second.Port))
	if err != nil {
		t.Fatalf("expected the second generation's listener to still accept connections: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestListenerControllerResetRotatesCertificate(t *testing.T) {
	t.Parallel()
	srv := api.NewServer(api.Deps{})
	source := &fakeSource{pairs: []iosaccess.InterfaceAddressPair{loopbackPair}}
	ctrl := api.NewListenerController(srv, t.TempDir(), source)
	t.Cleanup(func() { _ = ctrl.Disable() })

	first, err := ctrl.Enable(context.Background(), loopbackPair)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}

	// Regression guard for the bug this Reset fixes: Disable then Enable
	// alone reuses the still-valid persisted certificate, so it must NOT be
	// mistaken for a real rotation.
	_ = ctrl.Disable()
	reEnabled, err := ctrl.Enable(context.Background(), loopbackPair)
	if err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if reEnabled.FingerprintHex != first.FingerprintHex {
		t.Fatalf("test assumption violated: plain disable+enable already rotates the certificate")
	}

	reset, err := ctrl.Reset(context.Background(), loopbackPair)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if reset.FingerprintHex == "" {
		t.Fatalf("expected a fingerprint after reset")
	}
	if reset.FingerprintHex == first.FingerprintHex {
		t.Fatalf("expected Reset to rotate to a different certificate, got the same fingerprint %q", reset.FingerprintHex)
	}
	if reset.Port == 0 {
		t.Fatalf("expected a nonzero ephemeral port after reset")
	}

	// The listener is actually re-bound and serving with the new certificate.
	client := pinnedHTTPSClient(t, reset.FingerprintHex)
	resp, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/healthz", reset.Port))
	if err != nil {
		t.Fatalf("get after reset: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d after reset", resp.StatusCode)
	}

	// The old certificate is no longer accepted by a client pinned to it.
	oldPinnedClient := pinnedHTTPSClient(t, first.FingerprintHex)
	if _, err := oldPinnedClient.Get(fmt.Sprintf("https://127.0.0.1:%d/healthz", reset.Port)); err == nil {
		t.Fatalf("expected the pre-reset fingerprint to be rejected by the post-reset listener")
	}

	if enabled, unusable, _ := ctrl.Status(); !enabled || unusable {
		t.Fatalf("expected reset to leave the listener enabled and usable, got enabled=%v unusable=%v", enabled, unusable)
	}
}

func TestListenerControllerResetRejectsGoneCandidate(t *testing.T) {
	t.Parallel()
	srv := api.NewServer(api.Deps{})
	source := &fakeSource{pairs: []iosaccess.InterfaceAddressPair{loopbackPair}}
	ctrl := api.NewListenerController(srv, t.TempDir(), source)
	t.Cleanup(func() { _ = ctrl.Disable() })

	if _, err := ctrl.Enable(context.Background(), loopbackPair); err != nil {
		t.Fatalf("enable: %v", err)
	}

	// The candidate disappears before Reset is called -- Reset must revalidate
	// exactly like Enable, not blindly rebind.
	source.set(nil)
	if _, err := ctrl.Reset(context.Background(), loopbackPair); err == nil {
		t.Fatalf("expected reset to fail when the candidate is no longer in the snapshot")
	}
	if enabled, _, _ := ctrl.Status(); enabled {
		t.Fatalf("must not report enabled after a rejected reset")
	}
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
