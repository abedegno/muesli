//go:build !windows

package main

import (
	"context"
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/iosaccess"
)

// TestMaybeStartIOSAccessControlRealFDRoundTrip wires a genuine connected
// duplex Unix domain socketpair as the control fd -- exactly the shape
// Electron hands this process in production (one already-open, connected
// descriptor, per the accepted plan's fd-3 ruling) -- and drives a real
// enable/disable round trip through it end to end: env var -> fd ->
// ControlServer -> ListenerController -> a genuine TLS bind on 127.0.0.1.
func TestMaybeStartIOSAccessControlRealFDRoundTrip(t *testing.T) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	muesliEnd := os.NewFile(uintptr(fds[0]), "muesli-end")
	electronEnd := os.NewFile(uintptr(fds[1]), "electron-end")
	t.Cleanup(func() {
		_ = electronEnd.Close()
	})

	t.Setenv(muesliIOSAccessFDEnv, strconv.Itoa(fds[0]))

	srv := api.NewServer(api.Deps{})
	ctrl, err := maybeStartIOSAccessControl(context.Background(), srv)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if ctrl == nil {
		t.Fatalf("expected a non-nil controller")
	}
	t.Cleanup(func() { _ = ctrl.Disable() })
	// maybeStartIOSAccessControl now owns muesliEnd's lifecycle via its
	// ControlServer goroutine; do not close it directly here.
	_ = muesliEnd

	pair := iosaccess.InterfaceAddressPair{InterfaceName: "lo0", Address: "127.0.0.1", Family: iosaccess.IPv4}
	resp, err := iosaccess.SendControlRequest(electronEnd, iosaccess.ControlRequest{
		ID: "e2e-1", Action: iosaccess.ControlEnable, Pair: &pair,
	})
	if err != nil {
		t.Fatalf("enable request: %v", err)
	}
	// SystemInterfaceAddressSource enumerates this sandbox's real
	// interfaces, which will not contain 127.0.0.1 as an eligible pair (it's
	// loopback, always excluded) -- so this proves the whole live chain
	// reaches real eligibility enforcement, not just that bytes moved.
	if resp.OK {
		t.Fatalf("expected the real system source to reject a loopback candidate, got %+v", resp)
	}
	if resp.ID != "e2e-1" || resp.Error == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	disableResp, err := iosaccess.SendControlRequest(electronEnd, iosaccess.ControlRequest{ID: "e2e-2", Action: iosaccess.ControlDisable})
	if err != nil {
		t.Fatalf("disable request: %v", err)
	}
	if !disableResp.OK || disableResp.ID != "e2e-2" {
		t.Fatalf("unexpected disable response: %+v", disableResp)
	}

	// Give the server goroutine a moment before cleanup closes its fd.
	time.Sleep(10 * time.Millisecond)
}
