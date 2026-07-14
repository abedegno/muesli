package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/db"
	"github.com/abedegno/muesli/internal/plugin"
)

type doctorStatus string

const (
	doctorPass doctorStatus = "PASS"
	doctorWarn doctorStatus = "WARN"
	doctorFail doctorStatus = "FAIL"
)

var errPgvectorMissing = errors.New("pgvector extension missing")

type doctorCheck struct {
	Name   string
	Status doctorStatus
	Detail string
}

func (c doctorCheck) line() string {
	return fmt.Sprintf("[%s] %s: %s", c.Status, c.Name, c.Detail)
}

func (c doctorCheck) failed() bool { return c.Status == doctorFail }

type dbProbe interface {
	CheckPgvector(ctx context.Context, url string) error
}

type pluginProbe interface {
	CheckInfo(ctx context.Context, endpointURL, token string) error
}

type urlProbe interface {
	CheckReachable(ctx context.Context, url string) error
}

type writableProbe interface {
	CheckWritable(path string) error
}

type realDBProbe struct{}

func (realDBProbe) CheckPgvector(ctx context.Context, url string) error {
	pool, err := db.Connect(ctx, url)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}

	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'vector')`).Scan(&exists); err != nil {
		return fmt.Errorf("check extension: %w", err)
	}
	if !exists {
		return errPgvectorMissing
	}
	return nil
}

type realPluginProbe struct{}

func (realPluginProbe) CheckInfo(ctx context.Context, endpointURL, token string) error {
	if strings.TrimSpace(token) == "" {
		return errors.New("plugin token missing")
	}
	_, err := plugin.New(endpointURL, token).Info(ctx)
	return err
}

type realURLProbe struct{}

func (realURLProbe) CheckReachable(ctx context.Context, rawURL string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

type realWritableProbe struct{}

func (realWritableProbe) CheckWritable(path string) error {
	f, err := os.CreateTemp(path, "muesli-doctor-*")
	if err != nil {
		return err
	}
	name := f.Name()
	if cerr := f.Close(); cerr != nil {
		_ = os.Remove(name)
		return cerr
	}
	return os.Remove(name)
}

func passCheck(name, detail string) doctorCheck {
	return doctorCheck{Name: name, Status: doctorPass, Detail: detail}
}

func warnCheck(name, detail string) doctorCheck {
	return doctorCheck{Name: name, Status: doctorWarn, Detail: detail}
}

func failCheck(name, detail string) doctorCheck {
	return doctorCheck{Name: name, Status: doctorFail, Detail: detail}
}

func checkDatabase(ctx context.Context, cfg config.Config, probe dbProbe) doctorCheck {
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return failCheck("database", "DATABASE_URL is not set")
	}
	if probe == nil {
		probe = realDBProbe{}
	}
	if err := probe.CheckPgvector(ctx, cfg.DatabaseURL); err != nil {
		if errors.Is(err, errPgvectorMissing) {
			return failCheck("database", "DATABASE_URL reachable but pgvector extension is missing")
		}
		return failCheck("database", fmt.Sprintf("DATABASE_URL unreachable: %v", err))
	}
	return passCheck("database", "DATABASE_URL reachable; pgvector extension present")
}

func checkPlugin(ctx context.Context, name, endpointURL, token string, probe pluginProbe) doctorCheck {
	if strings.TrimSpace(endpointURL) == "" {
		return warnCheck(name, "not configured")
	}
	if strings.TrimSpace(token) == "" {
		return failCheck(name, "configured but token is missing")
	}
	if probe == nil {
		probe = realPluginProbe{}
	}
	if err := probe.CheckInfo(ctx, endpointURL, token); err != nil {
		return failCheck(name, fmt.Sprintf("configured but unhealthy/unreachable: %v", err))
	}
	return passCheck(name, fmt.Sprintf("healthy at %s", endpointURL))
}

func checkEmbeddings(ctx context.Context, cfg config.Config, probe urlProbe) doctorCheck {
	if strings.TrimSpace(cfg.EmbeddingsURL) == "" {
		return warnCheck("embeddings", "not configured (disabled)")
	}
	if probe == nil {
		probe = realURLProbe{}
	}
	if err := probe.CheckReachable(ctx, cfg.EmbeddingsURL); err != nil {
		return failCheck("embeddings", fmt.Sprintf("configured but unreachable: %v", err))
	}
	return passCheck("embeddings", fmt.Sprintf("reachable at %s", cfg.EmbeddingsURL))
}

func checkSecrets(cfg config.Config) doctorCheck {
	var problems []string
	if err := config.RequireMasterKey(cfg); err != nil {
		problems = append(problems, err.Error())
	}
	if strings.TrimSpace(cfg.StorageSigningKey) == "" {
		problems = append(problems, "MUESLI_STORAGE_SIGNING_KEY is required")
	}
	if warnings := config.DevSecretWarnings(cfg); len(warnings) > 0 {
		msg := "dev-only defaults in use: " + strings.Join(warnings, ", ")
		if cfg.Production {
			problems = append(problems, msg)
		} else {
			return warnCheck("secrets", msg)
		}
	}
	if len(problems) > 0 {
		return failCheck("secrets", strings.Join(problems, "; "))
	}
	return passCheck("secrets", "master key and storage signing key are set")
}

func checkWritableDir(name, path string, probe writableProbe, missingStatus doctorStatus) doctorCheck {
	if strings.TrimSpace(path) == "" {
		switch missingStatus {
		case doctorWarn:
			return warnCheck(name, "not configured")
		default:
			return failCheck(name, "not configured")
		}
	}
	if probe == nil {
		probe = realWritableProbe{}
	}
	if err := probe.CheckWritable(path); err != nil {
		return failCheck(name, fmt.Sprintf("not writable: %v", err))
	}
	return passCheck(name, fmt.Sprintf("writable: %s", path))
}

type doctorDeps struct {
	db       dbProbe
	plugin   pluginProbe
	url      urlProbe
	writable writableProbe
}

func buildDoctorChecks(ctx context.Context, cfg config.Config, deps doctorDeps) []doctorCheck {
	return []doctorCheck{
		checkDatabase(ctx, cfg, deps.db),
		checkPlugin(ctx, "default transcriber plugin", cfg.DefaultTranscriberURL, cfg.DefaultTranscriberToken, deps.plugin),
		checkPlugin(ctx, "default streaming transcriber plugin", cfg.DefaultStreamingTranscriberURL, cfg.DefaultStreamingTranscriberToken, deps.plugin),
		checkPlugin(ctx, "default agent plugin", cfg.DefaultAgentURL, cfg.DefaultAgentToken, deps.plugin),
		checkEmbeddings(ctx, cfg, deps.url),
		checkSecrets(cfg),
		checkWritableDir("audio dir", cfg.StorageDir, deps.writable, doctorFail),
		checkWritableDir("backup dir", cfg.BackupDir, deps.writable, doctorWarn),
	}
}

func runDoctor(ctx context.Context, cfg config.Config, args []string, stdout io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintf(stdout, "[FAIL] doctor: unexpected arguments: %s\n", strings.Join(args, " "))
		fmt.Fprintln(stdout, "Summary: 0 PASS, 0 WARN, 1 FAIL")
		return 1
	}

	checks := buildDoctorChecks(ctx, cfg, doctorDeps{
		db:       realDBProbe{},
		plugin:   realPluginProbe{},
		url:      realURLProbe{},
		writable: realWritableProbe{},
	})
	passCount, warnCount, failCount := 0, 0, 0
	for _, check := range checks {
		fmt.Fprintln(stdout, check.line())
		switch check.Status {
		case doctorPass:
			passCount++
		case doctorWarn:
			warnCount++
		case doctorFail:
			failCount++
		}
	}
	fmt.Fprintf(stdout, "Summary: %d PASS, %d WARN, %d FAIL\n", passCount, warnCount, failCount)
	if failCount > 0 {
		return 1
	}
	return 0
}
