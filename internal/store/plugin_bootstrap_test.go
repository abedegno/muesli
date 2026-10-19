package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func TestBootstrapPluginPreservesTranscriberSelection(t *testing.T) {
	for _, kind := range []string{model.PluginTranscriber, model.PluginStreamingTranscriber} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			st := store.New(testutil.NewPool(t))
			cr, ctx := testCrypto(t), context.Background()
			if err := st.EnsureBootstrapPlugin(ctx, cr, kind, "Bundled", "http://localhost:8001", "old-token", "{}"); err != nil {
				t.Fatal(err)
			}
			bundled, err := st.DefaultPlugin(ctx, cr, kind)
			if err != nil {
				t.Fatal(err)
			}
			alternative, err := st.CreatePlugin(ctx, cr, model.Plugin{Kind: kind, Name: "Alternative", EndpointURL: "http://alternative:9000", Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			if err := st.SetDefaultPlugin(ctx, alternative.ID); err != nil {
				t.Fatal(err)
			}
			if err := st.EnsureBootstrapPlugin(ctx, cr, kind, "Bundled", "http://localhost:8002", "new-token", "{}"); err != nil {
				t.Fatal(err)
			}
			selected, err := st.DefaultPlugin(ctx, cr, kind)
			if err != nil || selected.ID != alternative.ID {
				t.Fatalf("startup replaced selected plugin: got %s, want %s (%v)", selected.ID, alternative.ID, err)
			}
			refreshed, err := st.GetPlugin(ctx, cr, bundled.ID)
			if err != nil || refreshed.EndpointURL != "http://localhost:8002" || refreshed.Token != "new-token" {
				t.Fatalf("bundled connection was not refreshed: %+v (%v)", refreshed, err)
			}
		})
	}
}

func TestBootstrapPluginPreservesDisabledStreaming(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	cr, ctx := testCrypto(t), context.Background()
	kind := model.PluginStreamingTranscriber
	if err := st.EnsureBootstrapPlugin(ctx, cr, kind, "Bundled", "http://localhost:8001", "token", "{}"); err != nil {
		t.Fatal(err)
	}
	p, err := st.DefaultPlugin(ctx, cr, kind)
	if err != nil {
		t.Fatal(err)
	}
	p.Enabled = false
	if err := st.PatchPlugin(ctx, cr, p, false, true); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureBootstrapPlugin(ctx, cr, kind, "Bundled", "http://localhost:8002", "new-token", "{}"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DefaultPlugin(ctx, cr, kind); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("streaming re-enabled on startup: %v", err)
	}
	p, err = st.GetPlugin(ctx, cr, p.ID)
	if err != nil || p.Enabled || !p.IsDefault {
		t.Fatalf("disabled selection was not retained: %+v (%v)", p, err)
	}
}

func TestBootstrapPluginConcurrentRegistration(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	cr, ctx := testCrypto(t), context.Background()
	var wg sync.WaitGroup
	errors := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errors <- st.EnsureBootstrapPlugin(ctx, cr, model.PluginTranscriber, "Bundled", "http://localhost:8001", "token", "{}")
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	plugins, err := st.ListPlugins(ctx, cr)
	if err != nil || len(plugins) != 1 || !plugins[0].IsDefault {
		t.Fatalf("concurrent boot created duplicate/missing defaults: %+v (%v)", plugins, err)
	}
}
