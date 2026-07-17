package worker

import (
	"testing"

	"github.com/abedegno/muesli/internal/plugin"
)

func TestDefaultJobLeaseExceedsPluginRequestTimeout(t *testing.T) {
	// Lock this invariant so a future edit that lowers the lease or raises the
	// plugin timeout without adjusting the margin fails the build.
	if plugin.RequestTimeout <= 0 {
		t.Fatalf("plugin.RequestTimeout = %s, want > 0", plugin.RequestTimeout)
	}
	if defaultJobLease <= plugin.RequestTimeout {
		t.Fatalf("defaultJobLease = %s, want > plugin.RequestTimeout (%s)", defaultJobLease, plugin.RequestTimeout)
	}
}
