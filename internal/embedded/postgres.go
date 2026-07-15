package embedded

import (
	"context"
	crand "crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

const (
	embeddedPGBinaryEnv = "MUESLI_EMBEDDED_PG_BINARIES"
	embeddedPGPassFile  = "pgpass"
	postmasterPIDFile   = "postmaster.pid"
	pgStartTimeout      = 60 * time.Second
	pgStopTimeout       = 5 * time.Second
	pgKillWait          = 2 * time.Second
	passwordEntropy     = 32
)

// PG wraps a running embedded Postgres instance.
type PG struct {
	ep         *embeddedpostgres.EmbeddedPostgres
	dataDir    string
	runtimeDir string
	port       int
	password   string
	pid        int
	mu         sync.Mutex
}

// StartPostgres starts an embedded Postgres instance rooted at dataDir.
func StartPostgres(ctx context.Context, dataDir string, port int) (*PG, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create postgres data dir %q: %w", dataDir, err)
	}

	password, err := loadOrCreatePassword(dataDir)
	if err != nil {
		return nil, err
	}

	pg := &PG{
		dataDir:    dataDir,
		runtimeDir: runtimePathForDataDir(dataDir),
		port:       port,
		password:   password,
	}

	if err := pg.start(ctx); err != nil {
		return nil, err
	}

	return pg, nil
}

// URL returns the PostgreSQL connection string for the running instance.
func (p *PG) URL() string {
	return formatURL(p.password, p.port)
}

func (p *PG) runtimePath() string {
	if p == nil {
		return ""
	}
	return p.runtimeDir
}

// installRoot returns the directory that actually backs the running server's
// lib/share tree.
func (p *PG) installRoot() string {
	if p == nil {
		return ""
	}
	if binaries := getenv(embeddedPGBinaryEnv); binaries != "" {
		return binaries
	}
	return p.runtimePath()
}

// Stop shuts the server down gracefully, then force-kills it if needed.
func (p *PG) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if p == nil {
		return nil
	}

	p.mu.Lock()
	ep := p.ep
	p.mu.Unlock()
	if ep == nil {
		return nil
	}

	done := make(chan error, 1)
	go func() {
		done <- ep.Stop()
	}()

	stopCtx, cancel := context.WithTimeout(ctx, pgStopTimeout)
	defer cancel()

	select {
	case err := <-done:
		p.mu.Lock()
		p.ep = nil
		p.pid = 0
		p.mu.Unlock()
		return err
	case <-stopCtx.Done():
		if err := p.forceKill(); err != nil {
			return err
		}
		waitCtx, cancelWait := context.WithTimeout(context.Background(), pgKillWait)
		defer cancelWait()
		select {
		case <-done:
		case <-waitCtx.Done():
		}
		p.mu.Lock()
		p.ep = nil
		p.pid = 0
		p.mu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return nil
	}
}

func (p *PG) start(ctx context.Context) error {
	var staleRecovered bool
	var portRetried bool

	for attempts := 0; attempts < 3; attempts++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		if !staleRecovered {
			reaped, err := p.reapOrphanPostgres(ctx)
			if err != nil {
				return err
			}
			removed, err := removeStalePostmasterPID(p.dataDir)
			if err != nil {
				return err
			}
			if reaped || removed {
				staleRecovered = true
			}
		}

		ep := newEmbeddedPostgres(p.dataDir, p.port, p.password)
		if err := ep.Start(); err == nil {
			pid, err := readPostmasterPID(p.dataDir)
			if err != nil {
				_ = ep.Stop()
				return err
			}
			p.mu.Lock()
			p.ep = ep
			p.pid = pid
			p.mu.Unlock()
			return nil
		} else {
			if !staleRecovered && isStalePostmasterPIDError(err) {
				reaped, reapErr := p.reapOrphanPostgres(ctx)
				if reapErr != nil {
					return reapErr
				}
				removed, rmErr := removeStalePostmasterPID(p.dataDir)
				if rmErr != nil {
					return rmErr
				}
				if reaped || removed {
					staleRecovered = true
					continue
				}
			}

			if !portRetried && isPortTakenError(err) {
				newPort, portErr := FreeLoopbackPort()
				if portErr != nil {
					return portErr
				}
				p.port = newPort
				portRetried = true
				continue
			}

			return err
		}
	}

	return fmt.Errorf("start postgres: exceeded recovery attempts")
}

func newEmbeddedPostgres(dataDir string, port int, password string) *embeddedpostgres.EmbeddedPostgres {
	cfg := embeddedpostgres.DefaultConfig().
		DataPath(dataDir).
		RuntimePath(runtimePathForDataDir(dataDir)).
		Port(uint32(port)).
		Password(password).
		Version(embeddedpostgres.V17).
		StartTimeout(pgStartTimeout)

	if binaries := getenv(embeddedPGBinaryEnv); binaries != "" {
		cfg = cfg.BinariesPath(binaries)
	}

	return embeddedpostgres.NewDatabase(cfg)
}

func loadOrCreatePassword(dataDir string) (string, error) {
	passPath := passwordFilePath(dataDir)
	if data, err := os.ReadFile(passPath); err == nil {
		password := strings.TrimSpace(string(data))
		if password == "" {
			return "", fmt.Errorf("password file %q is empty", passPath)
		}
		return password, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read password file %q: %w", passPath, err)
	}

	password, err := generatePassword()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(passPath, []byte(password), 0o600); err != nil {
		return "", fmt.Errorf("write password file %q: %w", passPath, err)
	}
	return password, nil
}

func generatePassword() (string, error) {
	buf := make([]byte, passwordEntropy)
	if _, err := crand.Read(buf); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func formatURL(password string, port int) string {
	return fmt.Sprintf("postgres://postgres:%s@127.0.0.1:%d/postgres?sslmode=disable", password, port)
}

func passwordFilePath(dataDir string) string {
	return filepath.Join(filepath.Dir(dataDir), embeddedPGPassFile)
}

func runtimePathForDataDir(dataDir string) string {
	return filepath.Join(filepath.Dir(dataDir), filepath.Base(dataDir)+"-runtime")
}

func readPostmasterPID(dataDir string) (int, error) {
	data, err := os.ReadFile(filepath.Join(dataDir, postmasterPIDFile))
	if err != nil {
		return 0, fmt.Errorf("read postmaster pid: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return 0, fmt.Errorf("parse postmaster pid: empty pid file")
	}

	pid, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return 0, fmt.Errorf("parse postmaster pid: %w", err)
	}
	if pid <= 0 {
		return 0, fmt.Errorf("parse postmaster pid: invalid pid %d", pid)
	}
	return pid, nil
}

// readPostmasterInfo returns the pid (line 1) and the data directory the
// postmaster recorded (line 2) from a postmaster.pid file.
func readPostmasterInfo(dataDir string) (int, string, error) {
	data, err := os.ReadFile(filepath.Join(dataDir, postmasterPIDFile))
	if err != nil {
		return 0, "", err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return 0, "", fmt.Errorf("postmaster pid: too few lines")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || pid <= 0 {
		return 0, "", fmt.Errorf("postmaster pid: invalid pid")
	}
	return pid, strings.TrimSpace(lines[1]), nil
}

func (p *PG) pgCtlPath() string {
	root := p.installRoot()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "bin", "pg_ctl")
}

// reapOrphanPostgres detects a LIVE postmaster still holding p.dataDir -- an
// orphan left by a previous app instance that exited without stopping Postgres --
// and shuts it down so a fresh instance can start. It only acts on a postmaster
// that records OUR data dir (guarding against pid reuse). Returns true if it
// stopped one. Dead/parse-error pid files are left to removeStalePostmasterPID.
func (p *PG) reapOrphanPostgres(ctx context.Context) (bool, error) {
	pid, ownerDir, err := readPostmasterInfo(p.dataDir)
	if err != nil {
		return false, nil
	}
	if !processAlive(pid) {
		return false, nil
	}
	if filepath.Clean(ownerDir) != filepath.Clean(p.dataDir) {
		return false, nil
	}

	// Prefer pg_ctl: it verifies the postmaster owns the data dir and shuts it
	// down cleanly (fast mode).
	if ctl := p.pgCtlPath(); ctl != "" {
		if _, statErr := os.Stat(ctl); statErr == nil {
			cmd := exec.CommandContext(ctx, ctl, "stop", "-D", p.dataDir, "-m", "fast", "-w", "-t", "10")
			if runErr := cmd.Run(); runErr == nil {
				_ = os.Remove(filepath.Join(p.dataDir, postmasterPIDFile))
				return true, nil
			}
		}
	}

	// Fallback: SIGINT is Postgres's fast-shutdown signal. Wait for exit.
	var waitCh <-chan error
	if proc, ferr := os.FindProcess(pid); ferr == nil {
		_ = proc.Signal(syscall.SIGINT)
		ch := make(chan error, 1)
		waitCh = ch
		go func() {
			_, err := proc.Wait()
			ch <- err
		}()
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if waitCh != nil {
			select {
			case err := <-waitCh:
				if err == nil {
					_ = os.Remove(filepath.Join(p.dataDir, postmasterPIDFile))
					return true, nil
				}
				waitCh = nil
			default:
			}
		}
		if !processAlive(pid) {
			_ = os.Remove(filepath.Join(p.dataDir, postmasterPIDFile))
			return true, nil
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false, fmt.Errorf("orphan postgres (pid %d) did not exit", pid)
}

func removeStalePostmasterPID(dataDir string) (bool, error) {
	pid, err := readPostmasterPID(dataDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file") {
			return false, nil
		}
		if strings.Contains(err.Error(), "parse postmaster pid") {
			if rmErr := os.Remove(filepath.Join(dataDir, postmasterPIDFile)); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				return false, fmt.Errorf("remove stale postmaster pid: %w", rmErr)
			}
			return true, nil
		}
		return false, err
	}

	if processAlive(pid) {
		return false, nil
	}

	if rmErr := os.Remove(filepath.Join(dataDir, postmasterPIDFile)); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		return false, fmt.Errorf("remove stale postmaster pid: %w", rmErr)
	}
	return true, nil
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	return true
}

func isStalePostmasterPIDError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "postmaster.pid") || strings.Contains(msg, "postmaster pid")
}

func isPortTakenError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already listening on port")
}

func (p *PG) forceKill() error {
	p.mu.Lock()
	pid := p.pid
	if pid == 0 {
		var err error
		pid, err = readPostmasterPID(p.dataDir)
		p.mu.Unlock()
		if err != nil {
			return err
		}
	} else {
		p.mu.Unlock()
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}
