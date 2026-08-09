//go:build !windows

package embedded

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

func TestAgentStopHonorsAlreadyCancelledContextAfterKill(t *testing.T) {
	cmd := exec.Command("sh", "-c", "trap '' INT; sleep 60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	handle := &AgentHandle{
		cmd:  cmd,
		done: make(chan error, 1),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	returned := make(chan error, 1)
	go func() {
		returned <- handle.Stop(ctx)
	}()

	select {
	case err := <-returned:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Stop() error = %v, want context.Canceled", err)
		}
	case <-time.After(500 * time.Millisecond):
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		t.Fatal("Stop blocked after the caller's deadline expired")
	}

	_, _ = cmd.Process.Wait()
}
