package embedded

import (
	"context"
	crand "crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
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
	passwordEntropy     = 32
)

// ownerLockTimeout bounds how long a launch waits for a previous instance to
// release the data directory. A var, not a const, so tests need not spend it.
var ownerLockTimeout = 30 * time.Second

// Shutdown waits are vars so tests can exercise both timeout paths without
// spending the production durations.
var (
	pgStopTimeout = 5 * time.Second
	killWait      = 2 * time.Second

	waitForPostmasterExit = waitForExit
)

// ErrPostmasterNotOwned reports that the postmaster recorded in the data
// directory was started by a different instance, so this one refused to act on
// it. Callers should treat it as "shutdown did not happen", not as a failure to
// be retried against the same directory.
var ErrPostmasterNotOwned = errors.New("embedded postgres: postmaster not owned by this instance")

// PG wraps a running embedded Postgres instance.
type PG struct {
	ep         *embeddedpostgres.EmbeddedPostgres
	dataDir    string
	runtimeDir string
	port       int
	password   string
	pid        int
	lock       *ownerLock
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
	if !embeddedPostgresSupported {
		return nil, errors.New("embedded postgres is not supported on this platform")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create postgres data dir %q: %w", dataDir, err)
	}

	// Serialize every instance that touches this directory (#585) BEFORE touching
	// any shared state in it. The previous instance holds this until its shutdown
	// completes, so our reap-and-start cannot interleave with its stop; if it was
	// SIGKILLed the kernel has already released the lock for us.
	//
	// Ordering matters: the password file lives in this directory, so two cold
	// starts racing here would each generate a password and overwrite the other's,
	// leaving a cluster initialized with one and a file holding the other.
	lock, err := acquireOwnerLock(dataDir, ownerLockTimeout)
	if err != nil {
		return nil, err
	}

	password, err := loadOrCreatePassword(dataDir)
	if err != nil {
		lock.release()
		return nil, err
	}

	pg := &PG{
		dataDir:    dataDir,
		runtimeDir: runtimePathForDataDir(dataDir),
		port:       port,
		password:   password,
		lock:       lock,
	}

	if err := pg.start(ctx); err != nil {
		// A failed start can still have left a postmaster running -- the embedded
		// library has failure paths after launch. Confirm it is gone before handing
		// the directory on; passing a nil error here would short-circuit the check
		// and release regardless.
		if !postmasterStillRunning(dataDir, pg.startedPID()) {
			lock.release()
		} else {
			// pg is about to become unreachable; retain the lock explicitly or the
			// finalizer will close its descriptor and release it.
			retainOwnerLock(lock)
			slog.Error("embedded postgres: start failed with a postmaster still running; keeping the data directory locked",
				"data_dir", dataDir, "error", err)
		}
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

// installRootForDataDir mirrors (*PG).installRoot() without needing a PG
// instance -- both resolve the same way from dataDir alone, since runtimeDir is
// always runtimePathForDataDir(dataDir). Used by the Windows identity check
// (processidentity_windows.go), which only has a dataDir to work with, not a PG.
func installRootForDataDir(dataDir string) string {
	if binaries := getenv(embeddedPGBinaryEnv); binaries != "" {
		return binaries
	}
	return runtimePathForDataDir(dataDir)
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

	// Only stop the postmaster we started (#585). ep.Stop() shuts down whatever
	// postmaster.pid currently names, which is not necessarily ours: when the
	// desktop app is SIGKILLed, this server keeps running until the parent-death
	// watch fires up to a second later, and in that window the relaunched app can
	// reap our postmaster and start its own in the same data directory. Stopping
	// then kills the NEW instance's database, and the replacement server dies at
	// startup with "connection refused" -- leaving the user with no server at all.
	pid := p.startedPID()

	// Both outcomes below share one release decision. Returning early from the
	// not-owned branch used to release the lock without checking whether OUR
	// postmaster was still alive -- and it can be, since losing ownership only
	// means postmaster.pid no longer names us (it may be missing or unreadable).
	var err error
	if !p.stillOwnsPostmaster() {
		slog.Warn("embedded postgres: data directory no longer owned by this instance; not stopping it",
			"data_dir", p.dataDir, "started_pid", pid)
		// Deliberately not nil: the caller asked for a shutdown and did not get
		// one. Reporting success here would be the same class of lie that made
		// this bug hard to see.
		err = ErrPostmasterNotOwned
	} else {
		err = p.stopOwnPostmaster(ctx)
	}

	p.mu.Lock()
	p.ep = nil
	p.pid = 0
	lock := p.lock
	p.mu.Unlock()
	// Windows-only bookkeeping (no-op on Unix): this instance no longer claims
	// pid as its own for dataDir, regardless of whether the shutdown above
	// actually succeeded -- p.pid is zeroed unconditionally above too.
	forgetWindowsOwnedProcess(p.dataDir)

	// Release ONLY once the postmaster is confirmed down. If the shutdown failed
	// and the process is still alive, holding the lock is the point: letting the
	// next instance start alongside a postmaster we could not kill is the hazard
	// this lock exists to prevent. The kernel releases it if we die.
	if shouldReleaseOwnerLock(err, pid) {
		p.mu.Lock()
		p.lock = nil
		p.mu.Unlock()
		lock.release()
	} else {
		// The caller may drop this PG; retain so the finalizer cannot release it.
		retainOwnerLock(lock)
		slog.Error("embedded postgres: shutdown did not confirm exit; keeping the data directory locked",
			"pid", pid, "error", err)
	}
	return err
}

// postmasterStillRunning reports whether a postmaster for dataDir is alive.
//
// capturedPID is consulted first, but it cannot be relied on alone: p.pid is only
// recorded once the embedded library's Start returns successfully, and that
// library can launch PostgreSQL, fail a later step, fail its own cleanup, and
// return an error -- leaving a live postmaster with no captured pid. So when we
// have no pid, ask the data directory who owns it.
func postmasterStillRunning(dataDir string, capturedPID int) bool {
	if capturedPID != 0 {
		return processAlive(capturedPID)
	}
	pid, _, err := readPostmasterInfo(dataDir)
	if err != nil {
		return false
	}
	return processAlive(pid) && processIsPostgresFor(pid, dataDir)
}

// shouldReleaseOwnerLock reports whether the data-directory lock may be handed on.
//
// Pure so the decision can be tested without arranging an unkillable process:
// release when the shutdown reported success, or when the postmaster is
// demonstrably gone regardless of what the shutdown reported.
func shouldReleaseOwnerLock(stopErr error, pid int) bool {
	if stopErr == nil {
		return true
	}
	if pid == 0 {
		return true
	}
	return !processAlive(pid)
}

// stopOwnPostmaster shuts down the postmaster THIS instance started, addressing
// it by pid rather than by data directory.
//
// The distinction is the whole point (#585). `pg_ctl stop -D <dir>` re-resolves
// the target through postmaster.pid at the moment it acts, so an ownership check
// beforehand is a TOCTOU: the check can pass, a replacement instance can take the
// directory over, and pg_ctl then stops the NEW postmaster. Signalling a captured
// pid avoids following a changed pid file. Because the OS can reuse that pid,
// identity must still be re-verified immediately before every signal below.
//
// SIGINT is Postgres's fast shutdown: disconnect clients, roll back in-flight
// transactions, checkpoint, exit.
func (p *PG) stopOwnPostmaster(ctx context.Context) error {
	pid := p.startedPID()
	if pid == 0 {
		return ErrPostmasterNotOwned
	}
	// Guard against pid reuse: our postmaster may have exited and its pid been
	// recycled by an unrelated process since we captured it.
	if !processIsPostgresFor(pid, p.dataDir) {
		if processAlive(pid) {
			return ErrPostmasterNotOwned
		}
		return nil // already gone; nothing to do
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	// Re-verify immediately before signalling so pid reuse during the work above
	// cannot direct SIGINT at a stranger. A residual TOCTOU remains between these
	// two adjacent calls, but it is the minimum irreducible gap when using portable
	// pid-based signals and is orders of magnitude smaller than the previous window.
	if !processIsPostgresFor(pid, p.dataDir) {
		if processAlive(pid) {
			return ErrPostmasterNotOwned
		}
		return nil
	}
	if err := proc.Signal(syscall.SIGINT); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}

	stopCtx, cancel := context.WithTimeout(ctx, pgStopTimeout)
	defer cancel()
	if waitForPostmasterExit(stopCtx, pid) {
		return nil
	}

	if err := p.forceKill(); err != nil {
		return err
	}
	killCtx, cancelKill := context.WithTimeout(ctx, killWait)
	defer cancelKill()
	if !waitForPostmasterExit(killCtx, pid) {
		// Reporting success here would tell the caller the database is down when it
		// is not -- the same shape of lie this whole change exists to remove.
		return fmt.Errorf("embedded postgres: pid %d still alive after SIGKILL", pid)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

// waitForExit polls until pid is gone or ctx expires, reporting whether it exited.
func waitForExit(ctx context.Context, pid int) bool {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !processAlive(pid) {
			return true
		}
		select {
		case <-ctx.Done():
			return !processAlive(pid)
		case <-ticker.C:
		}
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
			// Windows-only bookkeeping (no-op on Unix): record the pid/creation-time
			// pair for this dataDir now, while we are certain it is ours, so later
			// identity checks (stopOwnPostmaster/forceKill) do not have to fall back
			// to comparing executable paths -- see processidentity_windows.go.
			recordWindowsOwnedProcess(p.dataDir, pid)
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
// startedPID returns the postmaster pid this instance launched, or 0.
func (p *PG) startedPID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pid
}

// stillOwnsPostmaster reports whether the postmaster recorded in the data
// directory is the one this instance started (#585).
//
// It fails CLOSED: if we never captured a pid, or the pid file is unreadable or
// names a different process, we do not claim ownership and therefore do not stop
// it. Declining to stop a postmaster we may not own can at worst leak one that
// something else is already responsible for; stopping one we do not own takes
// the database out from under a live instance, which is the far worse outcome.
//
// There is a residual TOCTOU window between this check and the shutdown itself.
// It is orders of magnitude smaller than the seconds-wide race it closes, but
// closing it entirely needs an inter-process lock held for the data directory's
// whole lifetime -- worth doing separately.
func (p *PG) stillOwnsPostmaster() bool {
	started := p.startedPID()
	if started == 0 {
		return false
	}
	current, _, err := readPostmasterInfo(p.dataDir)
	if err != nil {
		return false
	}
	return current == started
}

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
// and shuts it down so a fresh instance can start. It only acts when both the pid
// file records our data dir and the live process has a matching Postgres command
// line. Returns true if it stopped one. Dead/parse-error pid files are left to
// removeStalePostmasterPID.
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

	// The checks above prove only what the FILE says. Confirm the live process is
	// actually our Postgres before doing anything to it. If identity cannot be
	// established, leave the pid file to removeStalePostmasterPID and let startup
	// fail loudly -- failing to start is recoverable, killing a stranger is not.
	if !processIsPostgresFor(pid, p.dataDir) {
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

// processAlive reports whether pid names a live process. Implemented per
// platform: processliveness_unix.go (signal 0) and processliveness_windows.go
// (OpenProcess + GetExitCodeProcess), since a Windows liveness probe cannot use
// signal 0.

// windowsStillActive mirrors Win32's STILL_ACTIVE sentinel (also
// STATUS_PENDING, 259): GetExitCodeProcess reports this value for a process
// that has not exited yet. golang.org/x/sys/windows does not currently export
// it, so it is named here.
const windowsStillActive = 259

// windowsProcessAlive is the pure decision behind Windows process liveness,
// factored out of processliveness_windows.go so it is testable on any OS: the
// OpenProcess/GetExitCodeProcess calls that produce its inputs only compile
// under GOOS=windows.
func windowsProcessAlive(err error, exitCode uint32) bool {
	return err == nil && exitCode == windowsStillActive
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
	// Only ever kill the pid we captured at start. Reading it back from the
	// shared pid file would reintroduce exactly the bug this file now guards
	// against: that file can name a postmaster belonging to another instance.
	pid := p.startedPID()
	if pid == 0 {
		return ErrPostmasterNotOwned
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	// Re-verify immediately before signalling so pid reuse during the shutdown
	// wait cannot direct SIGKILL at a stranger. A residual TOCTOU remains between
	// these two adjacent calls, but it is the minimum irreducible gap when using
	// portable pid-based signals and is orders of magnitude smaller than the
	// previous seconds-wide window.
	if !processIsPostgresFor(pid, p.dataDir) {
		if processAlive(pid) {
			return ErrPostmasterNotOwned
		}
		return nil
	}
	if err := proc.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}
