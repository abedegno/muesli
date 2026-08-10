package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/embedded"
)

func TestEmbeddedStartup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cfg      config.Config
		progress *embedded.Reporter
		want     embedded.Progress
	}{
		{
			name: "hosted mode is always ready",
			cfg:  config.Config{Embedded: false},
			progress: func() *embedded.Reporter {
				r := embedded.NewReporter()
				r.Advance(embedded.PhaseMigrate, "running migrations")
				r.SetPercent(17)
				r.SetDegraded(true)
				return r
			}(),
			want: embedded.Progress{
				Phase:    embedded.PhaseReady,
				Detail:   "",
				Percent:  100,
				Ready:    true,
				Degraded: true,
			},
		},
		{
			name: "embedded mode reports injected progress",
			cfg:  config.Config{Embedded: true},
			progress: func() *embedded.Reporter {
				r := embedded.NewReporter()
				r.Advance(embedded.PhaseModelPull, "pulling Ollama models")
				r.SetPercent(64)
				r.SetDegraded(true)
				return r
			}(),
			want: embedded.Progress{
				Phase:    embedded.PhaseModelPull,
				Detail:   "pulling Ollama models",
				Percent:  64,
				Ready:    false,
				Degraded: true,
			},
		},
		{
			name: "embedded db-init snapshot",
			cfg:  config.Config{Embedded: true},
			progress: func() *embedded.Reporter {
				r := embedded.NewReporter()
				r.Advance(embedded.PhaseDBInit, "starting embedded postgres")
				return r
			}(),
			want: embedded.Progress{
				Phase:    embedded.PhaseDBInit,
				Detail:   "starting embedded postgres",
				Percent:  0,
				Ready:    false,
				Degraded: false,
			},
		},
		{
			name: "embedded ready snapshot",
			cfg:  config.Config{Embedded: true},
			progress: func() *embedded.Reporter {
				r := embedded.NewReporter()
				r.Advance(embedded.PhaseReady, "ready")
				return r
			}(),
			want: embedded.Progress{
				Phase:    embedded.PhaseReady,
				Detail:   "ready",
				Percent:  100,
				Ready:    true,
				Degraded: false,
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := NewServer(Deps{Config: tc.cfg, EmbeddedProgress: tc.progress})
			req := httptest.NewRequest(http.MethodGet, "/api/embedded/startup", nil)
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("want 200, got %d", rec.Code)
			}

			var got embedded.Progress
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got != tc.want {
				t.Fatalf("response = %#v, want %#v", got, tc.want)
			}
		})
	}
}
