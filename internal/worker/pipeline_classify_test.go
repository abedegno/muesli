package worker_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/abedegno/muesli/internal/worker"
)

func TestErrPluginNotConfiguredClassification(t *testing.T) {
	wrapped := fmt.Errorf("no default agent plugin configured: %w", worker.ErrPluginNotConfigured)
	if !errors.Is(wrapped, worker.ErrPluginNotConfigured) {
		t.Fatal("wrapped error should match ErrPluginNotConfigured")
	}

	if errors.Is(errors.New("boom"), worker.ErrPluginNotConfigured) {
		t.Fatal("unrelated error should not match ErrPluginNotConfigured")
	}
}
