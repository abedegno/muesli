package api

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/abedegno/muesli/internal/iosaccess"
)

// ListenerController owns the additional, opt-in, private iOS TLS listener
// (issue #767 Task 5): an ephemeral-port HTTPS listener bound to exactly one
// selected LAN address, serving the same router/handlers as the desktop
// loopback listener — never a separate implementation of the API. It
// implements iosaccess.ListenerControl so it can sit behind a
// iosaccess.ControlServer on fd 3 in the Electron-managed embedded process.
//
// State belongs to this one instance (issue #767 / AGENTS.md: no
// package-level session state) — the embedded process constructs exactly
// one ListenerController tied to its one Server.
type ListenerController struct {
	srv     *Server
	certDir string
	source  iosaccess.InterfaceAddressSource
	// watchInterval is the bounded re-check interval; overridable by tests.
	watchInterval time.Duration

	mu       sync.Mutex
	pair     *iosaccess.InterfaceAddressPair
	ln       net.Listener
	httpSrv  *http.Server
	cancel   context.CancelFunc
	unusable bool
	// unusableCh is closed exactly once by watch, at the moment it detects
	// the currently bound pair has disappeared and force-closes the
	// listener. It gives callers (tests, and any future Electron-facing
	// "became unusable" notification) a real event to wait on instead of
	// polling Status(). Reset to nil whenever the listener is (re)enabled or
	// disabled.
	unusableCh chan struct{}
}

// NewListenerController constructs a controller for srv's router. certDir is
// the Electron application-data directory where per-address key material is
// persisted (see iosaccess.LoadOrCreateCertificate). source supplies the
// live interface snapshot Enable revalidates against and the watchdog polls.
func NewListenerController(srv *Server, certDir string, source iosaccess.InterfaceAddressSource) *ListenerController {
	return NewListenerControllerWithWatchInterval(srv, certDir, source, iosaccess.WatchInterval)
}

// NewListenerControllerWithWatchInterval is NewListenerController with an
// explicit watchdog interval, for tests that need the address-disappearance
// watchdog to fire quickly rather than waiting on iosaccess.WatchInterval.
func NewListenerControllerWithWatchInterval(srv *Server, certDir string, source iosaccess.InterfaceAddressSource, watchInterval time.Duration) *ListenerController {
	return &ListenerController{srv: srv, certDir: certDir, source: source, watchInterval: watchInterval}
}

var errCandidateGone = errors.New("selected address is no longer available")

// Enable revalidates pair against a fresh source snapshot, then binds a TLS
// listener exactly on pair.Address with an OS-chosen ephemeral port — never
// a wildcard address, never any other address on the interface. Any
// previously running listener is closed first (Enable is also how the
// desktop switches to a newly selected pair).
func (c *ListenerController) Enable(ctx context.Context, pair iosaccess.InterfaceAddressPair) (iosaccess.ListenerInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.disableLocked()

	current, err := c.source.Snapshot(ctx)
	if err != nil {
		return iosaccess.ListenerInfo{}, fmt.Errorf("snapshot interfaces: %w", err)
	}
	if !containsPair(current, pair) {
		return iosaccess.ListenerInfo{}, errCandidateGone
	}

	bundle, err := iosaccess.LoadOrCreateCertificate(c.certDir, pair.Address)
	if err != nil {
		return iosaccess.ListenerInfo{}, fmt.Errorf("certificate: %w", err)
	}
	cert, err := tls.X509KeyPair(bundle.CertPEM, bundle.KeyPEM)
	if err != nil {
		return iosaccess.ListenerInfo{}, fmt.Errorf("load keypair: %w", err)
	}

	rawLn, err := net.Listen("tcp", net.JoinHostPort(pair.Address, "0"))
	if err != nil {
		return iosaccess.ListenerInfo{}, fmt.Errorf("bind %s: %w", pair.Address, err)
	}
	tlsLn := tls.NewListener(rawLn, &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	})

	httpSrv := &http.Server{Handler: c.srv.Handler()}
	go func() {
		_ = httpSrv.Serve(tlsLn)
	}()

	port, err := parsePort(rawLn.Addr())
	if err != nil {
		_ = tlsLn.Close()
		return iosaccess.ListenerInfo{}, fmt.Errorf("parse bound port: %w", err)
	}
	watchCtx, cancel := context.WithCancel(context.Background())
	info := iosaccess.ListenerInfo{
		Port:           port,
		Origin:         iosaccess.BuildOrigin(pair.Address, port),
		FingerprintHex: fmt.Sprintf("%x", bundle.SPKIFingerprint),
		Phrase:         iosaccess.VerificationPhrase(bundle.SPKIFingerprint),
	}

	p := pair
	uc := make(chan struct{})
	c.pair = &p
	c.ln = tlsLn
	c.httpSrv = httpSrv
	c.cancel = cancel
	c.unusable = false
	c.unusableCh = uc

	go c.watch(watchCtx, p, uc)

	return info, nil
}

// Reset invalidates the persisted certificate for pair.Address (issue
// #767's "Reset & rotate certificate": Enable alone reuses a still-valid
// persisted certificate via iosaccess.LoadOrCreateCertificate, so calling
// Disable then Enable never actually rotates anything) and then re-enables
// on pair — which, with the old certificate/key files gone, regenerates and
// persists a brand new certificate/key pair before rebinding, exactly like
// a first-ever Enable for that address.
func (c *ListenerController) Reset(ctx context.Context, pair iosaccess.InterfaceAddressPair) (iosaccess.ListenerInfo, error) {
	if err := iosaccess.ResetCertificate(c.certDir, pair.Address); err != nil {
		return iosaccess.ListenerInfo{}, fmt.Errorf("reset certificate: %w", err)
	}
	return c.Enable(ctx, pair)
}

// Disable closes the listener if one is running. Safe to call repeatedly.
func (c *ListenerController) Disable() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.disableLocked()
	c.unusable = false
	return nil
}

// disableLocked must be called with c.mu held.
func (c *ListenerController) disableLocked() {
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	if c.httpSrv != nil {
		_ = c.httpSrv.Close() // hard close: this is a LAN control-plane action, not a graceful desktop shutdown.
		c.httpSrv = nil
	}
	c.ln = nil
	c.pair = nil
	c.unusableCh = nil
}

// UnusableCh returns the channel for the most recent Enable call, which
// watch closes exactly once if and when it detects the bound pair has
// disappeared and force-closes the listener. It returns nil if no listener
// is currently enabled (never called, or the most recent call was Disable).
// Callers must fetch it after Enable succeeds; a new Enable replaces it with
// a fresh channel for the new generation.
func (c *ListenerController) UnusableCh() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.unusableCh
}

// Status reports whether a listener is currently running and whether the
// last one was force-closed because its selected address disappeared
// ("unusable" — the accepted spec requires this state to persist until an
// explicit Stop/Enable, never an automatic rebind).
func (c *ListenerController) Status() (enabled bool, unusable bool, pair *iosaccess.InterfaceAddressPair) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pair == nil {
		return false, c.unusable, nil
	}
	p := *c.pair
	return true, false, &p
}

// watch polls c.source on a bounded interval and closes the listener the
// moment the bound pair's exact (interfaceName, address, family) triple is
// no longer present — an address change, a moved interface, or an
// enumeration error are all treated the same: stop, mark unusable, and make
// no further bind attempt until a fresh Enable call.
func (c *ListenerController) watch(ctx context.Context, bound iosaccess.InterfaceAddressPair, unusableCh chan struct{}) {
	ticker := time.NewTicker(c.watchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current, err := c.source.Snapshot(ctx)
			stillPresent := err == nil && containsPair(current, bound)
			if stillPresent {
				continue
			}
			c.mu.Lock()
			// Only act if we're still watching the same generation (Enable
			// or Disable may have already superseded this goroutine).
			// Address-pair equality alone is not enough: a re-enable on the
			// exact same pair produces a new generation with an equal pair,
			// so a stale watcher whose cancellation raced with this ticker
			// branch (already past the select, mid-iteration, when Enable
			// cancelled it) would otherwise still match and tear down the
			// new listener. unusableCh is generation-specific -- compare it
			// by identity against the controller's CURRENT one, under the
			// same lock, so only the watcher that actually owns the live
			// generation can act.
			if c.pair != nil && c.pair.Equal(bound) && c.unusableCh == unusableCh {
				c.disableLocked()
				c.unusable = true
			}
			c.mu.Unlock()
			close(unusableCh)
			return
		}
	}
}

func containsPair(pairs []iosaccess.InterfaceAddressPair, target iosaccess.InterfaceAddressPair) bool {
	for _, p := range pairs {
		if p.Equal(target) {
			return true
		}
	}
	return false
}

// parsePort is a small helper kept local to this file for tests that need to
// assert on the bound port independent of net.Listener's concrete type.
func parsePort(addr net.Addr) (int, error) {
	_, portStr, err := net.SplitHostPort(addr.String())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(portStr)
}
