// Package backup implements BAK01: an in-app, admin-driven Postgres backup
// feature for self-hosters. It shells out to pg_dump (custom format) via a
// small seam (Runner) so tests never invoke the real binary, and provides
// helpers to list and prune backups on disk.
//
// There is intentionally no restore support here: restore stays the
// documented manual pg_restore/psql procedure in docs/BACKUP.md. Dumps
// produced by Create are plain pg_dump --format=custom files and are
// restorable with `pg_restore` exactly as documented there.
package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// filenameTimeLayout matches Go's reference time in the form used by the
// backup filename convention: muesli-<UTC timestamp>.dump.
const filenameTimeLayout = "20060102150405"

// filenamePattern is the strict naming convention for backup files. Anything
// that doesn't match this exactly is rejected before it ever reaches the
// filesystem layer (list, download, prune all filter through it).
var filenamePattern = regexp.MustCompile(`^muesli-[0-9]{14}\.dump$`)

// Runner runs pg_dump against databaseURL, writing a custom-format dump to
// outputPath. It exists as a seam so tests can stub pg_dump invocation.
type Runner interface {
	Run(ctx context.Context, databaseURL, outputPath string) error
}

// PgDumpRunner is the default Runner: it shells out to the pg_dump binary.
// pg_dump accepts a full postgres:// connection URI as a positional
// argument, so no manual URL parsing is needed.
type PgDumpRunner struct{}

// Run invokes `pg_dump --format=custom --file=<outputPath> <databaseURL>`.
func (PgDumpRunner) Run(ctx context.Context, databaseURL, outputPath string) error {
	cmd := exec.CommandContext(ctx, "pg_dump", "--format=custom", "--file="+outputPath, databaseURL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pg_dump: %w: %s", err, out)
	}
	return nil
}

// Info describes one backup file's metadata, as returned by the admin API
// and used to render the admin UI's backups table.
type Info struct {
	Filename  string    `json:"filename"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

// ErrNotConfigured is returned by Create when dir is empty (the backup
// feature is disabled).
var ErrNotConfigured = errors.New("backup directory not configured")

// ValidFilename reports whether name matches the strict muesli-<UTC
// timestamp>.dump naming convention. Callers MUST check this before using a
// caller-supplied filename to build a filesystem path (rejects path
// traversal, absolute paths, and anything else that isn't a plain backup
// filename).
func ValidFilename(name string) bool {
	return filenamePattern.MatchString(name)
}

// Create runs a backup now via r, writing muesli-<UTC timestamp>.dump into
// dir (created with MkdirAll if missing), and returns its metadata. Returns
// ErrNotConfigured if dir is empty.
func Create(ctx context.Context, r Runner, databaseURL, dir string) (Info, error) {
	if dir == "" {
		return Info{}, ErrNotConfigured
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Info{}, fmt.Errorf("create backup dir: %w", err)
	}
	filename := "muesli-" + time.Now().UTC().Format(filenameTimeLayout) + ".dump"
	outputPath := filepath.Join(dir, filename)
	if err := r.Run(ctx, databaseURL, outputPath); err != nil {
		return Info{}, err
	}
	fi, err := os.Stat(outputPath)
	if err != nil {
		return Info{}, fmt.Errorf("stat backup: %w", err)
	}
	return Info{Filename: filename, SizeBytes: fi.Size(), CreatedAt: fi.ModTime().UTC()}, nil
}

// List returns the backups in dir, newest-first, filtered strictly by
// ValidFilename. A missing dir is treated as "no backups yet" (empty slice,
// no error) rather than a failure.
func List(dir string) ([]Info, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Info{}, nil
		}
		return nil, err
	}
	out := make([]Info, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !ValidFilename(e.Name()) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue // best-effort: skip entries we can't stat (e.g. removed mid-scan)
		}
		out = append(out, Info{Filename: e.Name(), SizeBytes: fi.Size(), CreatedAt: fi.ModTime().UTC()})
	}
	// The timestamp in the filename sorts lexicographically in chronological
	// order, so a plain string sort (descending) is newest-first.
	sort.Slice(out, func(i, j int) bool { return out[i].Filename > out[j].Filename })
	return out, nil
}

// Prune enforces retentionCount by deleting the oldest backups in dir beyond
// that count. retentionCount <= 0 disables pruning (keep everything).
func Prune(dir string, retentionCount int) error {
	if retentionCount <= 0 {
		return nil
	}
	infos, err := List(dir)
	if err != nil {
		return err
	}
	if len(infos) <= retentionCount {
		return nil
	}
	for _, info := range infos[retentionCount:] {
		if err := os.Remove(filepath.Join(dir, info.Filename)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("prune %s: %w", info.Filename, err)
		}
	}
	return nil
}

// ListRunner runs `pg_restore --list` against a dump file and returns its
// raw stdout (the archive's table-of-contents listing). It exists as a seam
// so tests can stub the invocation without shelling out to a real binary.
type ListRunner interface {
	List(ctx context.Context, dumpPath string) (string, error)
}

// PgRestoreListRunner is the default ListRunner: it shells out to the real
// pg_restore binary with the --list flag.
type PgRestoreListRunner struct{}

// List invokes `pg_restore --list <dumpPath>` and returns its stdout.
func (PgRestoreListRunner) List(ctx context.Context, dumpPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "pg_restore", "--list", dumpPath)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("pg_restore --list: %w: %s", err, out)
	}
	return string(out), nil
}

// VerifyResult describes the outcome of verifying a backup file's integrity
// by reading its table-of-contents via pg_restore --list.
type VerifyResult struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	SizeBytes  int64  `json:"size_bytes"`
	TableCount int    `json:"table_count"`
}

// Verify checks the integrity of a backup file by running ListRunner.List
// against it. It does NOT restore anything — it only reads the archive's
// table-of-contents. A corrupt/broken dump is reported as a normal result
// (OK=false) with a nil top-level error; a Go error is only returned for
// cases where the request itself is invalid (invalid filename, missing file).
func Verify(ctx context.Context, r ListRunner, dir, filename string) (VerifyResult, error) {
	if !ValidFilename(filename) {
		return VerifyResult{}, os.ErrNotExist
	}
	path := filepath.Join(dir, filename)
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return VerifyResult{}, os.ErrNotExist
		}
		return VerifyResult{}, err
	}
	size := fi.Size()

	toc, err := r.List(ctx, path)
	if err != nil {
		// A broken/corrupt dump is a normal verify-failed result, not a Go error.
		return VerifyResult{OK: false, Error: err.Error(), SizeBytes: size}, nil
	}

	tableCount := countTableDataEntries(toc)
	return VerifyResult{OK: true, SizeBytes: size, TableCount: tableCount}, nil
}

// countTableDataEntries counts lines containing " TABLE DATA " in the
// pg_restore --list output. Each such line represents one table's data entry
// in the archive's TOC (format: "3419; 0 16385 TABLE DATA public notes postgres").
func countTableDataEntries(toc string) int {
	count := 0
	for _, line := range strings.Split(toc, "\n") {
		if strings.Contains(line, " TABLE DATA ") {
			count++
		}
	}
	return count
}
