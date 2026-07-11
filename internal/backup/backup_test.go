package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeRunner is a Runner stub that never shells out to pg_dump: it writes
// canned content to outputPath (or returns a canned error), recording every
// call it receives.
type fakeRunner struct {
	err     error
	content []byte

	calls []struct{ databaseURL, outputPath string }
}

func (f *fakeRunner) Run(_ context.Context, databaseURL, outputPath string) error {
	f.calls = append(f.calls, struct{ databaseURL, outputPath string }{databaseURL, outputPath})
	if f.err != nil {
		return f.err
	}
	content := f.content
	if content == nil {
		content = []byte("fake dump content")
	}
	return os.WriteFile(outputPath, content, 0o644)
}

func TestCreate_NotConfigured(t *testing.T) {
	_, err := Create(context.Background(), &fakeRunner{}, "postgres://x", "")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestCreate_WritesFileAndMetadata(t *testing.T) {
	dir := t.TempDir()
	r := &fakeRunner{content: []byte("0123456789")}

	info, err := Create(context.Background(), r, "postgres://user:pass@host/db", dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !ValidFilename(info.Filename) {
		t.Errorf("Filename %q does not match the naming convention", info.Filename)
	}
	if info.SizeBytes != 10 {
		t.Errorf("SizeBytes = %d, want 10", info.SizeBytes)
	}
	if info.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
	if len(r.calls) != 1 {
		t.Fatalf("Runner.Run calls = %d, want 1", len(r.calls))
	}
	if r.calls[0].databaseURL != "postgres://user:pass@host/db" {
		t.Errorf("databaseURL passed to Runner = %q", r.calls[0].databaseURL)
	}
	// The dir must be created if missing (t.TempDir already exists here, but
	// Create must also work when it doesn't).
	nested := filepath.Join(dir, "nested", "dir")
	if _, err := Create(context.Background(), r, "postgres://x", nested); err != nil {
		t.Fatalf("Create into missing dir: %v", err)
	}
	if fi, err := os.Stat(nested); err != nil || !fi.IsDir() {
		t.Errorf("nested dir was not created: %v", err)
	}
}

func TestCreate_RunnerError(t *testing.T) {
	dir := t.TempDir()
	wantErr := errors.New("pg_dump exploded")
	_, err := Create(context.Background(), &fakeRunner{err: wantErr}, "postgres://x", dir)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestList_EmptyDirNoError(t *testing.T) {
	got, err := List(t.TempDir())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List = %v, want empty", got)
	}
}

func TestList_MissingDirNoError(t *testing.T) {
	got, err := List(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List = %v, want empty", got)
	}
}

func TestList_FiltersAndSortsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"muesli-20240101000000.dump",
		"muesli-20240301000000.dump",
		"muesli-20240201000000.dump",
		"not-a-backup.txt",
		"muesli-invalid.dump",
		"muesli-20240101000000.dump.bak",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("List returned %d entries, want 3 (got %+v)", len(got), got)
	}
	want := []string{
		"muesli-20240301000000.dump",
		"muesli-20240201000000.dump",
		"muesli-20240101000000.dump",
	}
	for i, w := range want {
		if got[i].Filename != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i].Filename, w)
		}
	}
}

func TestPrune_DeletesOldestBeyondRetention(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"muesli-20240101000000.dump",
		"muesli-20240102000000.dump",
		"muesli-20240103000000.dump",
		"muesli-20240104000000.dump",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := Prune(dir, 2); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	remaining, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(remaining) != 2 {
		t.Fatalf("remaining = %d, want 2 (got %+v)", len(remaining), remaining)
	}
	for _, r := range remaining {
		if r.Filename != "muesli-20240104000000.dump" && r.Filename != "muesli-20240103000000.dump" {
			t.Errorf("unexpected surviving backup: %s", r.Filename)
		}
	}
}

func TestPrune_NoOpWhenUnderRetentionOrDisabled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "muesli-20240101000000.dump"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Prune(dir, 7); err != nil {
		t.Fatalf("Prune (under retention): %v", err)
	}
	if err := Prune(dir, 0); err != nil {
		t.Fatalf("Prune (disabled): %v", err)
	}
	remaining, _ := List(dir)
	if len(remaining) != 1 {
		t.Fatalf("remaining = %d, want 1", len(remaining))
	}
}

func TestValidFilename(t *testing.T) {
	cases := map[string]bool{
		"muesli-20240101120000.dump":                  true,
		"../../etc/passwd":                            false,
		"muesli-20240101120000.dump/../../etc/passwd": false,
		"muesli-2024.dump":                            false,
		"muesli-20240101120000.dump.txt":              false,
		"backup.dump":                                 false,
		"":                                            false,
	}
	for name, want := range cases {
		if got := ValidFilename(name); got != want {
			t.Errorf("ValidFilename(%q) = %v, want %v", name, got, want)
		}
	}
}

// fakeListRunner stubs pg_restore --list: returns canned output or an error.
type fakeListRunner struct {
	output string
	err    error
}

func (f *fakeListRunner) List(_ context.Context, _ string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.output, nil
}

func TestVerify_ValidDump(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := "muesli-20240301120000.dump"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte("fake dump content 123"), 0o644); err != nil {
		t.Fatal(err)
	}

	toc := `;
; Archive created at 2024-03-01 12:00:00 UTC
;     dbname: muesli
;
; Selected TOC Entries:
;
3419; 0 16385 TABLE DATA public notes postgres
3420; 0 16390 TABLE DATA public users postgres
3421; 0 16395 TABLE DATA public tags postgres
2800; 2606 16400 CONSTRAINT public notes notes_pkey postgres
`
	runner := &fakeListRunner{output: toc}
	result, err := Verify(context.Background(), runner, dir, filename)
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected OK=true, got false (error: %s)", result.Error)
	}
	if result.TableCount != 3 {
		t.Errorf("TableCount = %d, want 3", result.TableCount)
	}
	if result.SizeBytes != 21 {
		t.Errorf("SizeBytes = %d, want 21", result.SizeBytes)
	}
	if result.Error != "" {
		t.Errorf("Error = %q, want empty", result.Error)
	}
}

func TestVerify_RunnerError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filename := "muesli-20240301120000.dump"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte("corrupt data"), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := &fakeListRunner{err: errors.New("pg_restore: [archiver] unsupported version")}
	result, err := Verify(context.Background(), runner, dir, filename)
	if err != nil {
		t.Fatalf("Verify returned Go error: %v (expected nil)", err)
	}
	if result.OK {
		t.Fatal("expected OK=false")
	}
	if result.Error == "" {
		t.Fatal("expected non-empty Error field")
	}
	if result.SizeBytes != 12 {
		t.Errorf("SizeBytes = %d, want 12", result.SizeBytes)
	}
}

func TestVerify_InvalidFilename(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runner := &fakeListRunner{output: "anything"}

	_, err := Verify(context.Background(), runner, dir, "../../etc/passwd")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
}

func TestVerify_MissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runner := &fakeListRunner{output: "anything"}

	_, err := Verify(context.Background(), runner, dir, "muesli-20240301120000.dump")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
}
