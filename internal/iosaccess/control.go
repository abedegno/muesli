package iosaccess

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// ListenerInfo is what a successful Enable reports back over the control
// protocol: everything the pairing UI needs to display (port/origin/
// fingerprint/phrase), and nothing secret.
type ListenerInfo struct {
	Port           int
	Origin         string
	FingerprintHex string
	Phrase         string
}

// ListenerControl is implemented by the thing that actually owns the
// private iOS TLS listener (internal/api.ListenerController in production).
// Defined here, not there, so this package never imports internal/api.
type ListenerControl interface {
	// Enable revalidates pair against a fresh interface snapshot and binds
	// the listener exactly on pair.Address with an ephemeral port. It must
	// return an error, not a partial bind, if pair is no longer present.
	Enable(ctx context.Context, pair InterfaceAddressPair) (ListenerInfo, error)
	// Disable closes the listener if one is running. Safe to call when
	// already disabled.
	Disable() error
	// Reset invalidates the persisted certificate for pair.Address (Enable
	// alone reuses a still-valid persisted certificate, so it never actually
	// rotates anything) and then re-enables on pair, exactly like Enable
	// otherwise -- same revalidation-against-a-fresh-snapshot requirement,
	// same "no partial bind on failure" requirement.
	Reset(ctx context.Context, pair InterfaceAddressPair) (ListenerInfo, error)
}

// InterfaceAddressSource abstracts OS interface enumeration so callers can
// inject a fake snapshot in tests instead of depending on the real host's
// network configuration.
type InterfaceAddressSource interface {
	Snapshot(ctx context.Context) ([]InterfaceAddressPair, error)
}

// SystemInterfaceAddressSource is the production InterfaceAddressSource: it
// enumerates real, currently-up, non-loopback OS interfaces and returns
// their eligible candidate pairs.
type SystemInterfaceAddressSource struct{}

func (SystemInterfaceAddressSource) Snapshot(context.Context) ([]InterfaceAddressPair, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var raw []RawInterface
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			// One interface failing to report addresses (e.g. a transient
			// permission or race with it going down) must not abort
			// enumeration of every other interface.
			continue
		}
		var textAddrs []string
		for _, a := range addrs {
			textAddrs = append(textAddrs, a.String())
		}
		raw = append(raw, RawInterface{Name: iface.Name, Addresses: textAddrs})
	}
	return EligiblePairs(raw), nil
}

// --- fd-3 control protocol -------------------------------------------------
//
// Electron and Go communicate with capped, newline-delimited, request-ID
// JSON on a duplex pipe (a connected socket/pipe passed to the Go child as
// fd 3 in production; any io.ReadWriter in tests). Every response echoes
// its request's id so a caller with several requests in flight can match
// them up.

const (
	// maxControlLineBytes bounds a single control-protocol line so a
	// misbehaving or compromised peer cannot force unbounded buffering.
	maxControlLineBytes = 64 * 1024
	// maxControlIDLen bounds the request id field specifically.
	maxControlIDLen = 128
)

// ControlAction is the verb of a control-protocol request.
type ControlAction string

const (
	ControlEnable  ControlAction = "enable"
	ControlDisable ControlAction = "disable"
	// ControlReset invalidates the persisted certificate for the given pair
	// and re-enables on it, so a client can force a brand new certificate
	// without waiting for the persisted one to expire ("Reset & rotate
	// certificate" in the desktop Settings UI).
	ControlReset ControlAction = "reset"
)

// ControlRequest is one line of the fd-3 protocol, Electron -> Go.
type ControlRequest struct {
	ID     string                `json:"id"`
	Action ControlAction         `json:"action"`
	Pair   *InterfaceAddressPair `json:"pair,omitempty"`
}

// ControlResponse is one line of the fd-3 protocol, Go -> Electron.
type ControlResponse struct {
	ID             string `json:"id"`
	OK             bool   `json:"ok"`
	Error          string `json:"error,omitempty"`
	Port           int    `json:"port,omitempty"`
	Origin         string `json:"origin,omitempty"`
	FingerprintHex string `json:"fingerprint_sha256,omitempty"`
	Phrase         string `json:"phrase,omitempty"`
}

// ControlServer reads ControlRequests from r and writes ControlResponses to
// w, dispatching each to target. It never holds request state across calls
// beyond what target itself owns (issue #767: no package-level session
// state).
type ControlServer struct {
	target ListenerControl
	r      io.Reader
	w      io.Writer
}

func NewControlServer(target ListenerControl, r io.Reader, w io.Writer) *ControlServer {
	return &ControlServer{target: target, r: r, w: w}
}

// Serve processes requests until r reaches EOF, ctx is cancelled, or a
// non-recoverable I/O error occurs (returned). Malformed individual lines
// produce an error response for that line's id (or empty id if unparseable)
// and do not stop the loop.
func (c *ControlServer) Serve(ctx context.Context) error {
	scanner := bufio.NewScanner(c.r)
	scanner.Buffer(make([]byte, 0, 4096), maxControlLineBytes)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Bytes()
		resp := c.handleLine(ctx, line)
		if err := c.writeResponse(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (c *ControlServer) handleLine(ctx context.Context, line []byte) ControlResponse {
	var req ControlRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return ControlResponse{Error: "malformed request"}
	}
	if len(req.ID) > maxControlIDLen {
		return ControlResponse{Error: "request id too long"}
	}
	switch req.Action {
	case ControlEnable:
		if req.Pair == nil {
			return ControlResponse{ID: req.ID, Error: "missing pair"}
		}
		info, err := c.target.Enable(ctx, *req.Pair)
		if err != nil {
			return ControlResponse{ID: req.ID, Error: err.Error()}
		}
		return ControlResponse{
			ID: req.ID, OK: true,
			Port: info.Port, Origin: info.Origin,
			FingerprintHex: info.FingerprintHex, Phrase: info.Phrase,
		}
	case ControlDisable:
		if err := c.target.Disable(); err != nil {
			return ControlResponse{ID: req.ID, Error: err.Error()}
		}
		return ControlResponse{ID: req.ID, OK: true}
	case ControlReset:
		if req.Pair == nil {
			return ControlResponse{ID: req.ID, Error: "missing pair"}
		}
		info, err := c.target.Reset(ctx, *req.Pair)
		if err != nil {
			return ControlResponse{ID: req.ID, Error: err.Error()}
		}
		return ControlResponse{
			ID: req.ID, OK: true,
			Port: info.Port, Origin: info.Origin,
			FingerprintHex: info.FingerprintHex, Phrase: info.Phrase,
		}
	default:
		return ControlResponse{ID: req.ID, Error: fmt.Sprintf("unknown action %q", req.Action)}
	}
}

func (c *ControlServer) writeResponse(resp ControlResponse) error {
	raw, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	_, err = c.w.Write(raw)
	return err
}

// SendControlRequest is the Electron-side (test/reference) helper: it writes
// one request and reads back exactly one matching response line. Production
// Electron code is TypeScript and implements this protocol independently
// (src/main/iosAccess/controller.ts); this exists so Go-side tests can drive
// ControlServer end-to-end without a second process.
func SendControlRequest(rw io.ReadWriter, req ControlRequest) (ControlResponse, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return ControlResponse{}, err
	}
	raw = append(raw, '\n')
	if _, err := rw.Write(raw); err != nil {
		return ControlResponse{}, err
	}
	scanner := bufio.NewScanner(rw)
	scanner.Buffer(make([]byte, 0, 4096), maxControlLineBytes)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return ControlResponse{}, err
		}
		return ControlResponse{}, errors.New("no response")
	}
	var resp ControlResponse
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return ControlResponse{}, err
	}
	return resp, nil
}

// WatchInterval is the default bounded interval on which a live listener
// re-checks that its bound pair is still present in the OS's interface
// snapshot (Task 5: "Compare the complete normalized triple on a bounded
// interval; mismatch or enumeration error closes atomically").
const WatchInterval = 5 * time.Second
