package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/embedded"
	"github.com/abedegno/muesli/internal/iosaccess"
)

// muesliIOSAccessFDEnv names the environment variable Electron sets to the
// fd number of the duplex pipe it passed this process for local iOS access
// control (issue #767's "capped request-ID JSON on child fd 3" — the fd
// number itself is configurable rather than hardcoded so the Electron side
// stays free to place it wherever its stdio wiring finds convenient).
//
// This is deliberately opt-in and additive: when unset (every existing
// desktop launch, and every launch until Electron's "Allow iOS access" is
// turned on), maybeStartIOSAccessControl is a no-op and none of this code
// path executes.
const muesliIOSAccessFDEnv = "MUESLI_IOS_ACCESS_FD"

// maybeStartIOSAccessControl starts the fd-3 control server for the
// additional private iOS TLS listener when MUESLI_IOS_ACCESS_FD names a
// valid, already-open file descriptor Electron has connected to itself. It
// returns (nil, nil) when the feature isn't requested for this launch.
//
// The returned ListenerController is not enabled here — Enable happens only
// in response to a control-protocol "enable" request, carrying the
// desktop-selected InterfaceAddressPair, exactly like every other opt-in
// enablement in the accepted spec.
func maybeStartIOSAccessControl(ctx context.Context, srv *api.Server) (*api.ListenerController, error) {
	raw := os.Getenv(muesliIOSAccessFDEnv)
	if raw == "" {
		return nil, nil
	}
	fdNum, err := strconv.Atoi(raw)
	if err != nil || fdNum < 0 {
		return nil, fmt.Errorf("invalid %s=%q", muesliIOSAccessFDEnv, raw)
	}
	channel := os.NewFile(uintptr(fdNum), "ios-access-control")
	if channel == nil {
		return nil, fmt.Errorf("%s=%d is not an open file descriptor", muesliIOSAccessFDEnv, fdNum)
	}

	appDataDir, err := embedded.AppDataDir()
	if err != nil {
		return nil, fmt.Errorf("resolve app data dir: %w", err)
	}
	certDir := filepath.Join(appDataDir, "ios-access")

	ctrl := api.NewListenerController(srv, certDir, iosaccess.SystemInterfaceAddressSource{})
	control := iosaccess.NewControlServer(ctrl, channel, channel)
	go func() {
		if err := control.Serve(ctx); err != nil {
			slog.Info("ios access control channel closed", "error", err)
		}
		_ = ctrl.Disable()
	}()
	slog.Info("ios access control channel started", "fd", fdNum, "cert_dir", certDir)
	return ctrl, nil
}
