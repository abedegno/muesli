package main

import (
	"context"
	"testing"

	"github.com/abedegno/muesli/internal/api"
)

func TestMaybeStartIOSAccessControlNoOpWhenUnset(t *testing.T) {
	t.Setenv(muesliIOSAccessFDEnv, "")
	srv := api.NewServer(api.Deps{})
	ctrl, err := maybeStartIOSAccessControl(context.Background(), srv)
	if err != nil || ctrl != nil {
		t.Fatalf("expected a no-op, got ctrl=%v err=%v", ctrl, err)
	}
}

func TestMaybeStartIOSAccessControlRejectsInvalidFD(t *testing.T) {
	t.Setenv(muesliIOSAccessFDEnv, "not-a-number")
	srv := api.NewServer(api.Deps{})
	if _, err := maybeStartIOSAccessControl(context.Background(), srv); err == nil {
		t.Fatalf("expected an error for a non-numeric fd")
	}
}
