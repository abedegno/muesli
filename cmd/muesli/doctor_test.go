package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/config"
)

type fakeDBProbe struct {
	err    error
	gotURL string
}

func (f *fakeDBProbe) CheckPgvector(ctx context.Context, url string) error {
	f.gotURL = url
	return f.err
}

type fakePluginProbe struct {
	err error
}

func (f *fakePluginProbe) CheckInfo(ctx context.Context, endpointURL, token string) error {
	return f.err
}

type fakeURLProbe struct {
	err error
}

func (f *fakeURLProbe) CheckReachable(ctx context.Context, url string) error {
	return f.err
}

type fakeWritableProbe struct {
	err error
}

func (f *fakeWritableProbe) CheckWritable(path string) error {
	return f.err
}

func TestCheckDatabase(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		cfg    config.Config
		probe  *fakeDBProbe
		status doctorStatus
		detail string
	}{
		{
			name:   "pass when reachable and vector exists",
			cfg:    config.Config{DatabaseURL: "postgres://db"},
			probe:  &fakeDBProbe{},
			status: doctorPass,
			detail: "DATABASE_URL reachable; pgvector extension present",
		},
		{
			name:   "fail when unset",
			cfg:    config.Config{},
			probe:  &fakeDBProbe{},
			status: doctorFail,
			detail: "DATABASE_URL is not set",
		},
		{
			name:   "fail when pgvector missing",
			cfg:    config.Config{DatabaseURL: "postgres://db"},
			probe:  &fakeDBProbe{err: errPgvectorMissing},
			status: doctorFail,
			detail: "DATABASE_URL reachable but pgvector extension is missing",
		},
		{
			name:   "fail when unreachable",
			cfg:    config.Config{DatabaseURL: "postgres://db"},
			probe:  &fakeDBProbe{err: errors.New("dial tcp: connection refused")},
			status: doctorFail,
			detail: "DATABASE_URL unreachable: dial tcp: connection refused",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := checkDatabase(context.Background(), tc.cfg, tc.probe)
			if got.Status != tc.status {
				t.Fatalf("status = %s, want %s", got.Status, tc.status)
			}
			if got.Detail != tc.detail {
				t.Fatalf("detail = %q, want %q", got.Detail, tc.detail)
			}
		})
	}
}

func TestCheckPlugin(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		endpoint   string
		token      string
		probe      *fakePluginProbe
		status     doctorStatus
		wantDetail string
	}{
		{
			name:       "pass when configured and healthy",
			endpoint:   "http://plugin",
			token:      "tok",
			probe:      &fakePluginProbe{},
			status:     doctorPass,
			wantDetail: "healthy at http://plugin",
		},
		{
			name:       "warn when not configured",
			status:     doctorWarn,
			wantDetail: "not configured",
		},
		{
			name:       "fail when token missing",
			endpoint:   "http://plugin",
			status:     doctorFail,
			wantDetail: "configured but token is missing",
		},
		{
			name:       "fail when unreachable",
			endpoint:   "http://plugin",
			token:      "tok",
			probe:      &fakePluginProbe{err: errors.New("timeout")},
			status:     doctorFail,
			wantDetail: "configured but unhealthy/unreachable: timeout",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := checkPlugin(context.Background(), "default plugin", tc.endpoint, tc.token, tc.probe)
			if got.Status != tc.status {
				t.Fatalf("status = %s, want %s", got.Status, tc.status)
			}
			if got.Detail != tc.wantDetail {
				t.Fatalf("detail = %q, want %q", got.Detail, tc.wantDetail)
			}
		})
	}
}

func TestCheckEmbeddings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		url        string
		probe      *fakeURLProbe
		status     doctorStatus
		wantDetail string
	}{
		{
			name:       "warn when disabled",
			status:     doctorWarn,
			wantDetail: "not configured (disabled)",
		},
		{
			name:       "pass when reachable",
			url:        "http://ollama:11434",
			probe:      &fakeURLProbe{},
			status:     doctorPass,
			wantDetail: "reachable at http://ollama:11434",
		},
		{
			name:       "fail when unreachable",
			url:        "http://ollama:11434",
			probe:      &fakeURLProbe{err: errors.New("connection refused")},
			status:     doctorFail,
			wantDetail: "configured but unreachable: connection refused",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := checkEmbeddings(context.Background(), config.Config{EmbeddingsURL: tc.url}, tc.probe)
			if got.Status != tc.status {
				t.Fatalf("status = %s, want %s", got.Status, tc.status)
			}
			if got.Detail != tc.wantDetail {
				t.Fatalf("detail = %q, want %q", got.Detail, tc.wantDetail)
			}
		})
	}
}

func TestCheckSecrets(t *testing.T) {
	t.Parallel()

	validKey := "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

	cases := []struct {
		name       string
		cfg        config.Config
		status     doctorStatus
		wantDetail string
	}{
		{
			name: "pass when secrets are set",
			cfg: config.Config{
				MasterKey:         validKey,
				StorageSigningKey: "real-storage-signing-key",
			},
			status:     doctorPass,
			wantDetail: "master key and storage signing key are set",
		},
		{
			name: "warn when dev defaults are used outside production",
			cfg: config.Config{
				MasterKey:               validKey,
				StorageSigningKey:       "dev-storage-signing-key-change-me",
				DefaultAgentToken:       "dev-agent-token",
				DefaultTranscriberToken: "real",
			},
			status:     doctorWarn,
			wantDetail: "dev-only defaults in use: MUESLI_STORAGE_SIGNING_KEY, MUESLI_DEFAULT_AGENT_TOKEN",
		},
		{
			name: "fail when dev defaults are used in production",
			cfg: config.Config{
				Production:        true,
				MasterKey:         validKey,
				StorageSigningKey: "dev-storage-signing-key-change-me",
			},
			status:     doctorFail,
			wantDetail: "dev-only defaults in use: MUESLI_STORAGE_SIGNING_KEY",
		},
		{
			name:       "fail when missing",
			cfg:        config.Config{},
			status:     doctorFail,
			wantDetail: "MUESLI_MASTER_KEY is required; generate one with: openssl rand -base64 32; MUESLI_STORAGE_SIGNING_KEY is required",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := checkSecrets(tc.cfg)
			if got.Status != tc.status {
				t.Fatalf("status = %s, want %s", got.Status, tc.status)
			}
			if got.Detail != tc.wantDetail {
				t.Fatalf("detail = %q, want %q", got.Detail, tc.wantDetail)
			}
		})
	}
}

func TestCheckWritableDir(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		path       string
		missing    doctorStatus
		probe      *fakeWritableProbe
		status     doctorStatus
		wantDetail string
	}{
		{
			name:       "pass when writable",
			path:       "/var/lib/muesli/audio",
			missing:    doctorFail,
			probe:      &fakeWritableProbe{},
			status:     doctorPass,
			wantDetail: "writable: /var/lib/muesli/audio",
		},
		{
			name:       "warn when backup dir missing",
			missing:    doctorWarn,
			status:     doctorWarn,
			wantDetail: "not configured",
		},
		{
			name:       "fail when not writable",
			path:       "/var/lib/muesli/audio",
			missing:    doctorFail,
			probe:      &fakeWritableProbe{err: errors.New("permission denied")},
			status:     doctorFail,
			wantDetail: "not writable: permission denied",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := checkWritableDir("audio dir", tc.path, tc.probe, tc.missing)
			if got.Status != tc.status {
				t.Fatalf("status = %s, want %s", got.Status, tc.status)
			}
			if got.Detail != tc.wantDetail {
				t.Fatalf("detail = %q, want %q", got.Detail, tc.wantDetail)
			}
		})
	}
}

func TestBuildDoctorChecks(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		DatabaseURL:                      "postgres://db",
		MasterKey:                        "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
		StorageSigningKey:                "real-storage-signing-key",
		StorageDir:                       "/var/lib/muesli/audio",
		BackupDir:                        "",
		DefaultTranscriberURL:            "http://transcriber",
		DefaultTranscriberToken:          "tok",
		DefaultAgentURL:                  "",
		DefaultStreamingTranscriberURL:   "http://streaming",
		DefaultStreamingTranscriberToken: "tok",
	}

	checks := buildDoctorChecks(context.Background(), cfg, doctorDeps{
		db:       &fakeDBProbe{},
		plugin:   &fakePluginProbe{},
		url:      &fakeURLProbe{},
		writable: &fakeWritableProbe{},
	})

	if len(checks) != 8 {
		t.Fatalf("len(checks) = %d, want 8", len(checks))
	}
	if checks[0].Name != "database" || checks[0].Status != doctorPass {
		t.Fatalf("database check = %+v", checks[0])
	}
	if checks[3].Name != "default agent plugin" || checks[3].Status != doctorWarn {
		t.Fatalf("agent check = %+v", checks[3])
	}
	if checks[4].Name != "embeddings" || checks[4].Status != doctorWarn {
		t.Fatalf("embeddings check = %+v", checks[4])
	}
	if checks[7].Name != "backup dir" || checks[7].Status != doctorWarn {
		t.Fatalf("backup check = %+v", checks[7])
	}
	for _, check := range checks {
		line := check.line()
		if !strings.HasPrefix(line, "[") || !strings.Contains(line, ": ") {
			t.Fatalf("unexpected line format: %q", line)
		}
	}
}
