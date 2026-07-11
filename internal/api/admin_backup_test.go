package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/backup"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

// stubBackupRunner never shells out to pg_dump: it writes a small dummy file
// to outputPath instead, recording every call.
type stubBackupRunner struct {
	err   error
	calls int
}

func (s *stubBackupRunner) Run(_ context.Context, _, outputPath string) error {
	s.calls++
	if s.err != nil {
		return s.err
	}
	return os.WriteFile(outputPath, []byte("fake pg_dump output"), 0o644)
}

// backupServer builds a Server with the given backup dir + stubbed Runner
// wired through Deps, and returns an authenticated admin header (same
// setup/login flow as adminServer in admin_plugins_test.go).
func backupServer(t *testing.T, dir string, runner backup.Runner) (*api.Server, map[string]string) {
	t.Helper()
	st := store.New(testutil.NewPool(t))
	cr, err := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatal(err)
	}
	prov, _ := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	srv := api.NewServer(api.Deps{
		Store:   st,
		Storage: prov,
		Crypto:  cr,
		Config: config.Config{
			BackupDir:            dir,
			BackupRetentionCount: 7,
		},
		BackupRunner: runner,
	})

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	return srv, map[string]string{"Authorization": "Bearer " + login.Token}
}

func TestAdminBackup_NotConfigured(t *testing.T) {
	t.Parallel()
	srv, hdr := backupServer(t, "", &stubBackupRunner{})

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/backup", nil, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("create status %d, want 400: %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/admin/backups", nil, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("list status %d, want 400: %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/admin/backups/muesli-20240101120000.dump", nil, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("download status %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestAdminBackup_CreateListDownload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runner := &stubBackupRunner{}
	srv, hdr := backupServer(t, dir, runner)

	// Empty dir -> empty list, not an error.
	rec := doJSON(t, srv, http.MethodGet, "/api/admin/backups", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status %d: %s", rec.Code, rec.Body)
	}
	var list []backup.Info
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("initial list = %+v, want empty", list)
	}

	// Create.
	rec = doJSON(t, srv, http.MethodPost, "/api/admin/backup", nil, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", rec.Code, rec.Body)
	}
	var created backup.Info
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if created.Filename == "" {
		t.Fatal("expected a filename")
	}
	if runner.calls != 1 {
		t.Fatalf("Runner calls = %d, want 1 (stub, not real pg_dump)", runner.calls)
	}

	// List now shows it.
	rec = doJSON(t, srv, http.MethodGet, "/api/admin/backups", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status %d: %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(list) != 1 || list[0].Filename != created.Filename {
		t.Fatalf("list = %+v, want [%s]", list, created.Filename)
	}

	// Download happy path.
	rec = doJSON(t, srv, http.MethodGet, "/api/admin/backups/"+created.Filename, nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("download status %d: %s", rec.Code, rec.Body)
	}
	if rec.Body.String() != "fake pg_dump output" {
		t.Fatalf("download body = %q", rec.Body.String())
	}
	cd := rec.Header().Get("Content-Disposition")
	if cd == "" {
		t.Fatal("expected a Content-Disposition header")
	}

	// Unauthenticated is rejected.
	rec = doJSON(t, srv, http.MethodGet, "/api/admin/backups", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status %d, want 401", rec.Code)
	}
}

func TestAdminBackup_DownloadRejectsPathTraversal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A file outside the backup dir that a traversal attempt might target.
	secret := dir + "-sibling-secret.txt"
	if err := os.WriteFile(secret, []byte("do not leak me"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(secret) })

	srv, hdr := backupServer(t, dir, &stubBackupRunner{})

	for _, filename := range []string{
		"..%2F..%2Fetc%2Fpasswd",
		"..-sibling-secret.txt",
		"not-a-backup.dump",
		"muesli-2024.dump",
		"muesli-20240101120000.dump.txt",
	} {
		rec := doJSON(t, srv, http.MethodGet, "/api/admin/backups/"+filename, nil, hdr)
		if rec.Code != http.StatusNotFound {
			t.Errorf("download(%q) status = %d, want 404", filename, rec.Code)
		}
	}
}

func TestAdminBackup_DownloadMissingFile404(t *testing.T) {
	t.Parallel()
	srv, hdr := backupServer(t, t.TempDir(), &stubBackupRunner{})

	rec := doJSON(t, srv, http.MethodGet, "/api/admin/backups/muesli-20240101120000.dump", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404: %s", rec.Code, rec.Body)
	}
}

func TestAdminBackup_CreatePrunesToRetention(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Pre-seed 7 fake backups (at the default retention count) so the next
	// create pushes it to 8, requiring one prune.
	for i := 1; i <= 7; i++ {
		name := "muesli-2024010" + string(rune('0'+i)) + "120000.dump"
		if err := os.WriteFile(dir+"/"+name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, hdr := backupServer(t, dir, &stubBackupRunner{})

	rec := doJSON(t, srv, http.MethodPost, "/api/admin/backup", nil, hdr)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/admin/backups", nil, hdr)
	var list []backup.Info
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(list) != 7 {
		t.Fatalf("post-create list len = %d, want 7 (pruned to retention count)", len(list))
	}
}

// stubListRunner stubs backup.ListRunner for handler tests.
type stubListRunner struct {
	output string
	err    error
}

func (s *stubListRunner) List(_ context.Context, _ string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.output, nil
}

// backupServerWithListRunner builds a Server with backup dir, Runner, and
// ListRunner wired through Deps, and returns an authenticated admin header.
func backupServerWithListRunner(t *testing.T, dir string, runner backup.Runner, listRunner backup.ListRunner) (*api.Server, map[string]string) {
	t.Helper()
	st := store.New(testutil.NewPool(t))
	cr, err := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatal(err)
	}
	prov, _ := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	srv := api.NewServer(api.Deps{
		Store:   st,
		Storage: prov,
		Crypto:  cr,
		Config: config.Config{
			BackupDir:            dir,
			BackupRetentionCount: 7,
		},
		BackupRunner:     runner,
		BackupListRunner: listRunner,
	})

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	return srv, map[string]string{"Authorization": "Bearer " + login.Token}
}

func TestAdminBackup_VerifyHappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := "muesli-20240301120000.dump"
	if err := os.WriteFile(dir+"/"+filename, []byte("fake dump data"), 0o644); err != nil {
		t.Fatal(err)
	}

	toc := "3419; 0 16385 TABLE DATA public notes postgres\n3420; 0 16390 TABLE DATA public users postgres\n"
	listRunner := &stubListRunner{output: toc}
	srv, hdr := backupServerWithListRunner(t, dir, &stubBackupRunner{}, listRunner)

	rec := doJSON(t, srv, http.MethodGet, "/api/admin/backups/"+filename+"/verify", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body)
	}

	var result struct {
		OK         bool   `json:"ok"`
		Error      string `json:"error"`
		SizeBytes  int64  `json:"size_bytes"`
		TableCount int    `json:"table_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected ok=true, got false (error: %s)", result.Error)
	}
	if result.TableCount != 2 {
		t.Errorf("table_count = %d, want 2", result.TableCount)
	}
	if result.SizeBytes != 14 {
		t.Errorf("size_bytes = %d, want 14", result.SizeBytes)
	}
}

func TestAdminBackup_VerifyRunnerError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := "muesli-20240301120000.dump"
	if err := os.WriteFile(dir+"/"+filename, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}

	listRunner := &stubListRunner{err: errors.New("pg_restore: [archiver] unsupported version")}
	srv, hdr := backupServerWithListRunner(t, dir, &stubBackupRunner{}, listRunner)

	rec := doJSON(t, srv, http.MethodGet, "/api/admin/backups/"+filename+"/verify", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body)
	}

	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.OK {
		t.Fatal("expected ok=false")
	}
	if result.Error == "" {
		t.Fatal("expected non-empty error field")
	}
}

func TestAdminBackup_VerifyMissingFile(t *testing.T) {
	t.Parallel()
	srv, hdr := backupServerWithListRunner(t, t.TempDir(), &stubBackupRunner{}, &stubListRunner{output: "x"})

	rec := doJSON(t, srv, http.MethodGet, "/api/admin/backups/muesli-20240301120000.dump/verify", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404: %s", rec.Code, rec.Body)
	}
}

func TestAdminBackup_VerifyInvalidFilename(t *testing.T) {
	t.Parallel()
	srv, hdr := backupServerWithListRunner(t, t.TempDir(), &stubBackupRunner{}, &stubListRunner{output: "x"})

	rec := doJSON(t, srv, http.MethodGet, "/api/admin/backups/../../etc/passwd/verify", nil, hdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404: %s", rec.Code, rec.Body)
	}
}

func TestAdminBackup_VerifyNotConfigured(t *testing.T) {
	t.Parallel()
	srv, hdr := backupServerWithListRunner(t, "", &stubBackupRunner{}, &stubListRunner{output: "x"})

	rec := doJSON(t, srv, http.MethodGet, "/api/admin/backups/muesli-20240301120000.dump/verify", nil, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestAdminBackup_VerifyUnauthenticated(t *testing.T) {
	t.Parallel()
	srv, _ := backupServerWithListRunner(t, t.TempDir(), &stubBackupRunner{}, &stubListRunner{output: "x"})

	rec := doJSON(t, srv, http.MethodGet, "/api/admin/backups/muesli-20240301120000.dump/verify", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401: %s", rec.Code, rec.Body)
	}
}
