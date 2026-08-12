package embedded

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// These tests exercise the pure decision logic behind the Windows liveness and
// identity implementations (processliveness_windows.go,
// processidentity_windows.go). They run on every OS this CI builds, including
// this one (Linux) -- unlike the OpenProcess/GetExitCodeProcess/
// QueryFullProcessImageName calls themselves, which only compile under
// GOOS=windows and so cannot be exercised by a runner that isn't Windows. See
// the PR description for why the OS-calling code itself has no test here.

func TestWindowsProcessAlive(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		exitCode uint32
		want     bool
	}{
		{name: "still running", err: nil, exitCode: windowsStillActive, want: true},
		{name: "exited cleanly", err: nil, exitCode: 0, want: false},
		{name: "exited with non-zero code", err: nil, exitCode: 1, want: false},
		{name: "exited with an unrelated large exit code", err: nil, exitCode: 0x00001030, want: false},
		{name: "GetExitCodeProcess failed", err: errors.New("access denied"), exitCode: windowsStillActive, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := windowsProcessAlive(tc.err, tc.exitCode); got != tc.want {
				t.Fatalf("windowsProcessAlive(%v, %d) = %v, want %v", tc.err, tc.exitCode, got, tc.want)
			}
		})
	}
}

func TestPostgresImagePathMatches(t *testing.T) {
	cases := []struct {
		name      string
		imagePath string
		wantPath  string
		want      bool
	}{
		{
			name:      "exact match",
			imagePath: `C:\data\mydb-runtime\bin\postgres.exe`,
			wantPath:  `C:\data\mydb-runtime\bin\postgres.exe`,
			want:      true,
		},
		{
			name:      "case-insensitive match",
			imagePath: `c:\Data\MyDB-Runtime\BIN\Postgres.EXE`,
			wantPath:  `C:\data\MyDB-Runtime\bin\postgres.exe`,
			want:      true,
		},
		{
			name:      "different data directory's binary",
			imagePath: `C:\data\other-runtime\bin\postgres.exe`,
			wantPath:  `C:\data\mydb-runtime\bin\postgres.exe`,
			want:      false,
		},
		{
			name:      "unrelated executable reusing the pid",
			imagePath: `C:\Windows\System32\notepad.exe`,
			wantPath:  `C:\data\mydb-runtime\bin\postgres.exe`,
			want:      false,
		},
		{
			name:      "empty image path",
			imagePath: "",
			wantPath:  `C:\data\mydb-runtime\bin\postgres.exe`,
			want:      false,
		},
		{
			name:      "empty want path",
			imagePath: `C:\data\mydb-runtime\bin\postgres.exe`,
			wantPath:  "",
			want:      false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := postgresImagePathMatches(tc.imagePath, tc.wantPath); got != tc.want {
				t.Fatalf("postgresImagePathMatches(%q, %q) = %v, want %v", tc.imagePath, tc.wantPath, got, tc.want)
			}
		})
	}
}

func TestInstallRootForDataDirUsesEmbeddedBinaryOverride(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	t.Setenv(embeddedPGBinaryEnv, "")
	if got, want := installRootForDataDir(dataDir), runtimePathForDataDir(dataDir); got != want {
		t.Fatalf("installRootForDataDir(%q) = %q, want %q", dataDir, got, want)
	}

	t.Setenv(embeddedPGBinaryEnv, "/tmp/binaries")
	if got, want := installRootForDataDir(dataDir), "/tmp/binaries"; got != want {
		t.Fatalf("installRootForDataDir(%q) = %q, want %q", dataDir, got, want)
	}
}

func TestInstallRootForDataDirMatchesPGInstallRoot(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	pg := &PG{dataDir: dataDir, runtimeDir: runtimePathForDataDir(dataDir)}

	t.Setenv(embeddedPGBinaryEnv, "")
	if got, want := installRootForDataDir(dataDir), pg.installRoot(); got != want {
		t.Fatalf("installRootForDataDir(%q) = %q, want %q (PG.installRoot())", dataDir, got, want)
	}

	t.Setenv(embeddedPGBinaryEnv, "/tmp/binaries")
	if got, want := installRootForDataDir(dataDir), pg.installRoot(); got != want {
		t.Fatalf("installRootForDataDir(%q) = %q, want %q (PG.installRoot())", dataDir, got, want)
	}
}

func TestPostmasterStartTimeMatches(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	const tolerance = 5 * time.Second

	cases := []struct {
		name     string
		recorded time.Time
		actual   time.Time
		want     bool
	}{
		{name: "exact match", recorded: base, actual: base, want: true},
		{name: "within tolerance, actual after recorded", recorded: base, actual: base.Add(3 * time.Second), want: true},
		{name: "within tolerance, actual before recorded", recorded: base, actual: base.Add(-3 * time.Second), want: true},
		{name: "outside tolerance", recorded: base, actual: base.Add(30 * time.Second), want: false},
		{name: "zero recorded", recorded: time.Time{}, actual: base, want: false},
		{name: "zero actual", recorded: base, actual: time.Time{}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := postmasterStartTimeMatches(tc.recorded, tc.actual, tolerance); got != tc.want {
				t.Fatalf("postmasterStartTimeMatches(%v, %v, %v) = %v, want %v", tc.recorded, tc.actual, tolerance, got, tc.want)
			}
		})
	}
}

// TestWindowsForeignIdentityMatchesDiscriminatesSharedBinariesRoot is the
// regression test for the cross-review finding on this PR: when
// MUESLI_EMBEDDED_PG_BINARIES points two different data directories at one
// shared binaries root, both resolve to the SAME expected postgres.exe path,
// so postgresImagePathMatches alone returns true for both regardless of which
// data directory a live process actually belongs to. This must not be treated
// as "identity established" -- the start-time cross-check has to be the thing
// that actually discriminates between the two data directories, not path
// equality.
func TestWindowsForeignIdentityMatchesDiscriminatesSharedBinariesRoot(t *testing.T) {
	const tolerance = 5 * time.Second

	dataDirAStart := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	dataDirBStart := time.Date(2026, 1, 1, 12, 30, 0, 0, time.UTC)

	// A live process whose actual creation time lines up with data dir A's
	// recorded start -- and, because both data directories share one
	// MUESLI_EMBEDDED_PG_BINARIES root, pathMatches is true for BOTH (that is
	// the scenario itself, not something under test here).
	livePostgresCreatedAt := dataDirAStart.Add(1 * time.Second)
	const pathMatches = true

	if !windowsForeignIdentityMatches(pathMatches, dataDirAStart, livePostgresCreatedAt, tolerance) {
		t.Fatal("expected a match for the data directory whose recorded start time lines up with the live process")
	}
	if windowsForeignIdentityMatches(pathMatches, dataDirBStart, livePostgresCreatedAt, tolerance) {
		t.Fatal("expected NO match for a different data directory that merely shares the same binaries root (and so the same expected executable path) -- path equality must not be treated as identity on its own")
	}
}

func TestWindowsForeignIdentityMatchesRequiresPathMatchToo(t *testing.T) {
	sameInstant := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if windowsForeignIdentityMatches(false, sameInstant, sameInstant, 5*time.Second) {
		t.Fatal("expected no match when the executable path itself does not match, even though timestamps align exactly")
	}
}

func TestWindowsOwnedIdentityMatches(t *testing.T) {
	cases := []struct {
		name             string
		err              error
		recordedCreation int64
		currentCreation  int64
		want             bool
	}{
		{name: "same creation time", err: nil, recordedCreation: 1000, currentCreation: 1000, want: true},
		{name: "pid recycled to a process created at a different instant", err: nil, recordedCreation: 1000, currentCreation: 2000, want: false},
		{name: "current creation time unavailable", err: errors.New("access denied"), recordedCreation: 1000, currentCreation: 1000, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := windowsOwnedIdentityMatches(tc.err, tc.recordedCreation, tc.currentCreation); got != tc.want {
				t.Fatalf("windowsOwnedIdentityMatches(%v, %d, %d) = %v, want %v", tc.err, tc.recordedCreation, tc.currentCreation, got, tc.want)
			}
		})
	}
}
