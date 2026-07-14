package pluginkit

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"
)

// NewServer creates an http.Server and loopback listener for the provided
// config. The caller owns both objects and must call Shutdown on cancellation.
func NewServer(cfg Config, handler http.Handler) (*http.Server, net.Listener, error) {
	ln, err := net.Listen("tcp", normalizeLoopbackAddr(cfg.Addr))
	if err != nil {
		return nil, nil, err
	}
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}, ln, nil
}

func serve(ctx context.Context, srv *http.Server, ln net.Listener) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdownErr := srv.Shutdown(shutdownCtx)
		err := <-errCh
		if shutdownErr != nil {
			return shutdownErr
		}
		if errors.Is(err, http.ErrServerClosed) || err == nil {
			return nil
		}
		return err
	}
}

func normalizeLoopbackAddr(addr string) string {
	if addr == "" {
		return "127.0.0.1:0"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		if strings.HasPrefix(addr, ":") {
			return "127.0.0.1" + addr
		}
		return addr
	}
	switch host {
	case "", "0.0.0.0", "::", "*":
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}
