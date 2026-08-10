package api

import (
	"errors"
	"testing"

	"github.com/abedegno/muesli/internal/store"
)

func TestAgentConfiguredFromLookup(t *testing.T) {
	configured, err := agentConfiguredFromLookup(nil)
	if err != nil || !configured {
		t.Fatalf("successful lookup = %v, %v; want true, nil", configured, err)
	}

	configured, err = agentConfiguredFromLookup(store.ErrNotFound)
	if err != nil || configured {
		t.Fatalf("not found = %v, %v; want false, nil", configured, err)
	}

	wantErr := errors.New("database unavailable")
	configured, err = agentConfiguredFromLookup(wantErr)
	if configured || !errors.Is(err, wantErr) {
		t.Fatalf("lookup failure = %v, %v; want false, original error", configured, err)
	}
}
