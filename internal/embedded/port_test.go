package embedded

import (
	"net"
	"strconv"
	"testing"
)

func TestFreeLoopbackPort(t *testing.T) {
	port1, err := FreeLoopbackPort()
	if err != nil {
		t.Fatalf("first FreeLoopbackPort() error: %v", err)
	}
	if port1 <= 0 {
		t.Fatalf("first FreeLoopbackPort() = %d, want > 0", port1)
	}

	ln1, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port1)))
	if err != nil {
		t.Fatalf("failed to rebind first port %d: %v", port1, err)
	}
	ln1.Close()

	port2, err := FreeLoopbackPort()
	if err != nil {
		t.Fatalf("second FreeLoopbackPort() error: %v", err)
	}
	if port2 <= 0 {
		t.Fatalf("second FreeLoopbackPort() = %d, want > 0", port2)
	}
	if port2 == port1 {
		t.Fatalf("ports should be distinct, got %d twice", port1)
	}

	ln2, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port2)))
	if err != nil {
		t.Fatalf("failed to rebind second port %d: %v", port2, err)
	}
	ln2.Close()
}
