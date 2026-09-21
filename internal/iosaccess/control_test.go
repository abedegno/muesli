package iosaccess_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/iosaccess"
)

type fakeControlTarget struct {
	enableCalls  []iosaccess.InterfaceAddressPair
	enableErr    error
	enableResult iosaccess.ListenerInfo
	disableCalls int
	disableErr   error
}

func (f *fakeControlTarget) Enable(_ context.Context, pair iosaccess.InterfaceAddressPair) (iosaccess.ListenerInfo, error) {
	f.enableCalls = append(f.enableCalls, pair)
	if f.enableErr != nil {
		return iosaccess.ListenerInfo{}, f.enableErr
	}
	return f.enableResult, nil
}

func (f *fakeControlTarget) Disable() error {
	f.disableCalls++
	return f.disableErr
}

func runOneLine(t *testing.T, target iosaccess.ListenerControl, requestJSON string) iosaccess.ControlResponse {
	t.Helper()
	in := strings.NewReader(requestJSON + "\n")
	var out bytes.Buffer
	server := iosaccess.NewControlServer(target, in, &out)
	if err := server.Serve(context.Background()); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var resp iosaccess.ControlResponse
	line := strings.TrimSpace(out.String())
	if line == "" {
		t.Fatalf("no response written")
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("decode response: %v (raw=%s)", err, line)
	}
	return resp
}

func TestControlServerEnableSuccess(t *testing.T) {
	target := &fakeControlTarget{enableResult: iosaccess.ListenerInfo{
		Port: 54321, Origin: "https://192.168.1.20:54321", FingerprintHex: "ab", Phrase: "AB-CD",
	}}
	resp := runOneLine(t, target, `{"id":"req-1","action":"enable","pair":{"InterfaceName":"en0","Address":"192.168.1.20","Family":"ipv4"}}`)
	if !resp.OK || resp.ID != "req-1" || resp.Port != 54321 || resp.Origin != "https://192.168.1.20:54321" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if len(target.enableCalls) != 1 || target.enableCalls[0].Address != "192.168.1.20" {
		t.Fatalf("enable not dispatched with the right pair: %+v", target.enableCalls)
	}
}

func TestControlServerEnableFailure(t *testing.T) {
	target := &fakeControlTarget{enableErr: errors.New("candidate gone")}
	resp := runOneLine(t, target, `{"id":"req-2","action":"enable","pair":{"InterfaceName":"en0","Address":"192.168.1.20","Family":"ipv4"}}`)
	if resp.OK || resp.Error != "candidate gone" || resp.ID != "req-2" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestControlServerEnableMissingPair(t *testing.T) {
	target := &fakeControlTarget{}
	resp := runOneLine(t, target, `{"id":"req-3","action":"enable"}`)
	if resp.OK || resp.Error == "" {
		t.Fatalf("expected an error for a missing pair: %+v", resp)
	}
	if len(target.enableCalls) != 0 {
		t.Fatalf("must not dispatch Enable without a pair")
	}
}

func TestControlServerDisable(t *testing.T) {
	target := &fakeControlTarget{}
	resp := runOneLine(t, target, `{"id":"req-4","action":"disable"}`)
	if !resp.OK || resp.ID != "req-4" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if target.disableCalls != 1 {
		t.Fatalf("disable not dispatched")
	}
}

func TestControlServerUnknownAction(t *testing.T) {
	target := &fakeControlTarget{}
	resp := runOneLine(t, target, `{"id":"req-5","action":"reboot"}`)
	if resp.OK || resp.Error == "" {
		t.Fatalf("expected rejection of an unknown action: %+v", resp)
	}
}

func TestControlServerMalformedLine(t *testing.T) {
	target := &fakeControlTarget{}
	resp := runOneLine(t, target, `not json`)
	if resp.OK || resp.Error == "" {
		t.Fatalf("expected rejection of a malformed line: %+v", resp)
	}
}

func TestControlServerOversizedIDRejected(t *testing.T) {
	target := &fakeControlTarget{}
	longID := strings.Repeat("x", 500)
	resp := runOneLine(t, target, `{"id":"`+longID+`","action":"disable"}`)
	if resp.OK {
		t.Fatalf("expected an oversized id to be rejected")
	}
}

func TestControlServerProcessesMultipleLinesSequentially(t *testing.T) {
	target := &fakeControlTarget{}
	in := strings.NewReader(
		`{"id":"a","action":"disable"}` + "\n" +
			`{"id":"b","action":"disable"}` + "\n" +
			`{"id":"c","action":"disable"}` + "\n")
	var out bytes.Buffer
	server := iosaccess.NewControlServer(target, in, &out)
	if err := server.Serve(context.Background()); err != nil {
		t.Fatalf("serve: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 response lines, got %d: %v", len(lines), lines)
	}
	if target.disableCalls != 3 {
		t.Fatalf("expected 3 dispatched disable calls, got %d", target.disableCalls)
	}
}

// TestSendControlRequestRoundTrip exercises the full duplex helper over a
// real net.Pipe, end to end, mirroring how the Electron-managed process
// would drive ControlServer over a real fd-3 connection.
func TestSendControlRequestRoundTrip(t *testing.T) {
	clientConn, serverConn := netPipe()
	target := &fakeControlTarget{enableResult: iosaccess.ListenerInfo{Port: 1, Origin: "https://10.0.0.5:1", FingerprintHex: "cd", Phrase: "EF-GH"}}
	server := iosaccess.NewControlServer(target, serverConn, serverConn)

	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background()) }()

	resp, err := iosaccess.SendControlRequest(clientConn, iosaccess.ControlRequest{
		ID:     "roundtrip-1",
		Action: iosaccess.ControlEnable,
		Pair:   &iosaccess.InterfaceAddressPair{InterfaceName: "en0", Address: "10.0.0.5", Family: iosaccess.IPv4},
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if !resp.OK || resp.ID != "roundtrip-1" || resp.Origin != "https://10.0.0.5:1" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	_ = clientConn.Close()
	_ = serverConn.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("server did not exit after pipe close")
	}
}

func netPipe() (net.Conn, net.Conn) {
	return net.Pipe()
}
