package embedded

import (
	"errors"
	"path/filepath"
	"testing"
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
