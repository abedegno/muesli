package embedded

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// writePostmasterPID writes a minimal postmaster.pid naming pid as the owner.
// Line 2 is the data directory, which readPostmasterInfo requires.
func writePostmasterPID(t *testing.T, dataDir string, pid int) {
	t.Helper()
	body := strconv.Itoa(pid) + "\n" + dataDir + "\n1700000000\n5432\n/tmp\nlocalhost\n  123 456\nready\n"
	if err := os.WriteFile(filepath.Join(dataDir, postmasterPIDFile), []byte(body), 0o600); err != nil {
		t.Fatalf("write postmaster.pid: %v", err)
	}
}

// The case that produced #585: this instance started a postmaster, a replacement
// instance took the data directory over, and then our deferred Stop ran. Stopping
// here would shut down the replacement's database.
func TestStillOwnsPostmasterFalseAfterTakeover(t *testing.T) {
	dataDir := t.TempDir()
	p := &PG{dataDir: dataDir, pid: 4242}

	writePostmasterPID(t, dataDir, 9999) // someone else owns it now

	if p.stillOwnsPostmaster() {
		t.Fatal("claimed ownership of a postmaster started by another instance")
	}
}

func TestStillOwnsPostmasterTrueForOwnProcess(t *testing.T) {
	dataDir := t.TempDir()
	p := &PG{dataDir: dataDir, pid: 4242}

	writePostmasterPID(t, dataDir, 4242)

	if !p.stillOwnsPostmaster() {
		t.Fatal("disowned the postmaster this instance started; a clean quit would leak it")
	}
}

// Fail closed: no captured pid, or no readable pid file, means we cannot claim
// ownership. Leaking a postmaster someone else is responsible for beats stopping
// a database out from under a live instance.
func TestStillOwnsPostmasterFailsClosed(t *testing.T) {
	dataDir := t.TempDir()

	if (&PG{dataDir: dataDir, pid: 0}).stillOwnsPostmaster() {
		t.Fatal("claimed ownership with no captured pid")
	}

	if (&PG{dataDir: dataDir, pid: 4242}).stillOwnsPostmaster() {
		t.Fatal("claimed ownership with no postmaster.pid present")
	}

	if err := os.WriteFile(filepath.Join(dataDir, postmasterPIDFile), []byte("garbage\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if (&PG{dataDir: dataDir, pid: 4242}).stillOwnsPostmaster() {
		t.Fatal("claimed ownership from an unparseable postmaster.pid")
	}
}
