package embedded

import (
	"fmt"
	"net"
)

// FreeLoopbackPort asks the OS for an available TCP port on 127.0.0.1 and
// returns it after closing the listener.
func FreeLoopbackPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener address type %T", ln.Addr())
	}
	return addr.Port, nil
}
