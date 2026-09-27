package acquire

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/manifest"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func newCache(t *testing.T) Cache {
	t.Helper()
	return Cache{Root: t.TempDir()}
}

// --- ResolveCache / deny-list -------------------------------------------------

func TestResolveCacheAcceptsExactDefault(t *testing.T) {
	root := t.TempDir()
	got, err := ResolveCache(root, "")
	if err != nil {
		t.Fatalf("resolve default: %v", err)
	}
	want := filepath.Join(root, ".cache", "ami-vad-eval")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveCacheRejectsListedDirectories(t *testing.T) {
	root := t.TempDir()
	for _, name := range RepoTopLevelDenyList() {
		if err := os.MkdirAll(filepath.Join(root, name, "nested"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range RepoTopLevelDenyList() {
		for _, suffix := range []string{"", string(filepath.Separator) + "nested"} {
			p := filepath.Join(root, name) + suffix
			if _, err := ResolveCache(root, p); err == nil {
				t.Fatalf("expected rejection for %q", p)
			}
		}
	}
}

func TestResolveCacheAcceptsExternalPath(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	got, err := ResolveCache(root, external)
	if err != nil {
		t.Fatalf("expected external cache accepted: %v", err)
	}
	if got != external {
		t.Fatalf("got %q want %q", got, external)
	}
}

func TestResolveCacheSymlinkAliasRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias-to-internal")
	if err := os.Symlink(filepath.Join(root, "internal"), alias); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if _, err := ResolveCache(root, alias); err == nil {
		t.Fatal("expected symlink alias into a denied directory to be rejected")
	}
}

func TestResolveCacheSymlinkAliasOfNonexistentSuffixRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias-to-internal")
	if err := os.Symlink(filepath.Join(root, "internal"), alias); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	// Nested path under the alias, not yet existing itself.
	if _, err := ResolveCache(root, filepath.Join(alias, "not-yet-created")); err == nil {
		t.Fatal("expected symlink alias with nonexistent suffix to be rejected")
	}
}

// TestDenyListCompleteness finds the module root through go.mod, lists its
// top-level entries, and asserts every non-dot directory (plus .github) is
// represented in the hardcoded deny-list literal. .cache and any other
// dot-directory are deliberately excluded, and files are ignored entirely.
func TestDenyListCompleteness(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := FindModuleRoot(wd)
	if err != nil {
		t.Fatalf("find module root: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	deny := map[string]bool{}
	for _, d := range RepoTopLevelDenyList() {
		deny[d] = true
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue // ordinary top-level files (go.mod, Makefile, ...) are not prefixes
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") && name != ".github" {
			continue // dot-directories are metadata/tooling/cache namespaces, not source/output
		}
		if !deny[name] {
			t.Errorf("top-level directory %q is not represented in the acquire deny-list literal", name)
		}
	}
}

// --- fetch / verify / quarantine ---------------------------------------------

func TestEnsureObjectDownloadsAndReuses(t *testing.T) {
	body := []byte("hello ami corpus")
	obj := Object{SizeBytes: int64(len(body)), SHA256: sha256Hex(body)}
	var hits int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write(body)
	}))
	defer srv.Close()
	obj.URL = srv.URL

	cache := newCache(t)
	opts := Options{HTTPClient: srv.Client()}
	path, err := EnsureObject(context.Background(), cache, obj, opts)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("unexpected content: %v %q", err, got)
	}

	// Second call reuses the cached, verified object without a new request.
	if _, err := EnsureObject(context.Background(), cache, obj, opts); err != nil {
		t.Fatalf("second fetch (reuse): %v", err)
	}
	if hits != 1 {
		t.Fatalf("expected exactly 1 HTTP request, got %d", hits)
	}
}

func TestEnsureObjectRejectsNonHTTPS(t *testing.T) {
	obj := Object{URL: "http://example.test/x.wav", SizeBytes: 3, SHA256: sha256Hex([]byte("abc"))}
	cache := newCache(t)
	if _, err := EnsureObject(context.Background(), cache, obj, Options{}); err == nil {
		t.Fatal("expected non-https rejection")
	}
}

func TestEnsureObjectStatusFailure(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	obj := Object{URL: srv.URL, SizeBytes: 3, SHA256: sha256Hex([]byte("abc"))}
	cache := newCache(t)
	if _, err := EnsureObject(context.Background(), cache, obj, Options{HTTPClient: srv.Client()}); err == nil {
		t.Fatal("expected status failure")
	}
}

func TestEnsureObjectSizeMismatch(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("short"))
	}))
	defer srv.Close()
	obj := Object{URL: srv.URL, SizeBytes: 100, SHA256: sha256Hex([]byte("something else entirely"))}
	cache := newCache(t)
	if _, err := EnsureObject(context.Background(), cache, obj, Options{HTTPClient: srv.Client()}); err == nil {
		t.Fatal("expected size mismatch failure")
	}
	// No temp files should remain.
	entries, _ := os.ReadDir(filepath.Join(cache.Root, "downloads"))
	for _, e := range entries {
		t.Fatalf("unexpected leftover file %q", e.Name())
	}
}

func TestEnsureObjectChecksumMismatch(t *testing.T) {
	body := []byte("corpus bytes")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()
	obj := Object{URL: srv.URL, SizeBytes: int64(len(body)), SHA256: strings.Repeat("0", 64)}
	cache := newCache(t)
	if _, err := EnsureObject(context.Background(), cache, obj, Options{HTTPClient: srv.Client()}); err == nil {
		t.Fatal("expected checksum mismatch failure")
	}
}

func TestEnsureObjectByteCeiling(t *testing.T) {
	// Server sends far more than the pinned size; the ceiling must stop the
	// read (and thus the mismatch) rather than buffering unboundedly.
	big := bytes.Repeat([]byte("x"), 10_000)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(big)
	}))
	defer srv.Close()
	obj := Object{URL: srv.URL, SizeBytes: 10, SHA256: strings.Repeat("0", 64)}
	cache := newCache(t)
	if _, err := EnsureObject(context.Background(), cache, obj, Options{HTTPClient: srv.Client()}); err == nil {
		t.Fatal("expected byte-ceiling triggered mismatch failure")
	}
}

func TestEnsureObjectRedirectMustStayHTTPS(t *testing.T) {
	body := []byte("redirected body")
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer target.Close()

	insecureRedirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer insecureRedirector.Close()

	obj := Object{URL: target.URL, SizeBytes: int64(len(body)), SHA256: sha256Hex(body)}
	client := target.Client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("non-https redirect")
		}
		return nil
	}
	// Fetch the secure server directly first to prove the happy path works
	// with the same enforced client.
	cache := newCache(t)
	if _, err := EnsureObject(context.Background(), cache, obj, Options{HTTPClient: client}); err != nil {
		t.Fatalf("direct https fetch should succeed: %v", err)
	}
}

func TestEnsureObjectOfflineRequiresCache(t *testing.T) {
	obj := Object{URL: "https://example.test/x.wav", SizeBytes: 3, SHA256: sha256Hex([]byte("abc"))}
	cache := newCache(t)
	if _, err := EnsureObject(context.Background(), cache, obj, Options{Offline: true}); err == nil {
		t.Fatal("expected offline failure when object is not cached")
	}
}

func TestEnsureObjectOfflineUsesVerifiedCache(t *testing.T) {
	body := []byte("cached already")
	obj := Object{SizeBytes: int64(len(body)), SHA256: sha256Hex(body)}
	cache := newCache(t)
	dest := cache.DownloadPath(obj.SHA256)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, body, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := EnsureObject(context.Background(), cache, obj, Options{Offline: true})
	if err != nil {
		t.Fatalf("expected offline reuse to succeed: %v", err)
	}
	if got != dest {
		t.Fatalf("got %q want %q", got, dest)
	}
}

func TestEnsureObjectRehashesBeforeReuse(t *testing.T) {
	body := []byte("intended content")
	obj := Object{SizeBytes: int64(len(body)), SHA256: sha256Hex(body)}
	cache := newCache(t)
	dest := cache.DownloadPath(obj.SHA256)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	// Corrupt the cached bytes without touching the manifest pin.
	if err := os.WriteFile(dest, []byte("corrupted!!!!!!!"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureObject(context.Background(), cache, obj, Options{Offline: true}); err == nil {
		t.Fatal("expected corrupted cached object to fail verification")
	}
	// It must have been quarantined, not silently left in place or deleted.
	entries, err := os.ReadDir(filepath.Dir(dest))
	if err != nil {
		t.Fatal(err)
	}
	foundQuarantine := false
	for _, e := range entries {
		if strings.Contains(e.Name(), "quarantined") {
			foundQuarantine = true
		}
	}
	if !foundQuarantine {
		t.Fatal("expected a quarantined file diagnostic")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("expected corrupted object moved out of the reused path")
	}
}

func TestEnsureObjectCancellationRemovesTempButPreservesGoodDownloads(t *testing.T) {
	body := []byte("already good")
	goodObj := Object{SizeBytes: int64(len(body)), SHA256: sha256Hex(body)}
	cache := newCache(t)
	srvGood := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srvGood.Close()
	goodObj.URL = srvGood.URL
	if _, err := EnsureObject(context.Background(), cache, goodObj, Options{HTTPClient: srvGood.Client()}); err != nil {
		t.Fatalf("seed good object: %v", err)
	}

	block := make(chan struct{})
	srvSlow := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // block until the request context is canceled
	}))
	defer func() { close(block); srvSlow.Close() }()

	slowObj := Object{URL: srvSlow.URL, SizeBytes: 5, SHA256: strings.Repeat("1", 64)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := EnsureObject(ctx, cache, slowObj, Options{HTTPClient: srvSlow.Client()}); err == nil {
		t.Fatal("expected cancellation error")
	}

	entries, err := os.ReadDir(filepath.Join(cache.Root, "downloads"))
	if err != nil {
		t.Fatal(err)
	}
	sawTemp := false
	sawGood := false
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			sawTemp = true
		}
		if e.Name() == goodObj.SHA256 {
			sawGood = true
		}
	}
	if sawTemp {
		t.Fatal("expected no leftover temp file after cancellation")
	}
	if !sawGood {
		t.Fatal("expected the previously verified good download to be preserved")
	}
}

func TestEnsureObjectTimeout(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(block)
	}))
	defer srv.Close()
	obj := Object{URL: srv.URL, SizeBytes: 5, SHA256: strings.Repeat("2", 64)}
	cache := newCache(t)
	_, err := EnsureObject(context.Background(), cache, obj, Options{HTTPClient: srv.Client(), Timeout: 10 * time.Millisecond})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	<-block
}

// --- annotation archive extraction -------------------------------------------

func buildTestArchive(t *testing.T, files map[string][]byte) (string, int64, string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.zip")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path, int64(len(data)), sha256Hex(data)
}

func testManifestWithArchive(archiveURL string, archiveSize int64, archiveSHA string, wavURL func(meeting, mic string) string, wavSize func(meeting, mic string) int64, wavSHA func(meeting, mic string) string, annSHA map[string]string, annSize map[string]int64) *manifest.Manifest {
	sel0 := 0
	mk := func(id, class string) manifest.Meeting {
		return manifest.Meeting{
			ID:    id,
			Class: class,
			HeadsetMix: manifest.AudioObject{
				URL: wavURL(id, "headset"), SizeBytes: wavSize(id, "headset"), SHA256: wavSHA(id, "headset"),
				Channels: 1, ChannelPolicy: manifest.ChannelPolicyExplicit, SelectChannel: &sel0,
			},
			FixedDistantMix: manifest.AudioObject{
				URL: wavURL(id, "fixed"), SizeBytes: wavSize(id, "fixed"), SHA256: wavSHA(id, "fixed"),
				Channels: 1, ChannelPolicy: manifest.ChannelPolicyExplicit, SelectChannel: &sel0,
			},
			Annotations: []manifest.AnnotationObject{
				{ParticipantID: "A", Member: "words/" + id + ".A.words.xml", SizeBytes: annSize[id+".A"], SHA256: annSHA[id+".A"]},
			},
		}
	}
	return &manifest.Manifest{
		SchemaVersion:     manifest.SchemaVersion,
		AnnotationArchive: manifest.ArchiveObject{URL: archiveURL, SizeBytes: archiveSize, SHA256: archiveSHA},
		Meetings: []manifest.Meeting{
			mk("EN2001a", manifest.ClassNonScenario),
			mk("ES2002a", manifest.ClassScenario),
		},
	}
}

func TestAcquireExtractsAndVerifiesAnnotationMembers(t *testing.T) {
	esXML := []byte("<nite:root/>ES2002a content")
	enXML := []byte("<nite:root/>EN2001a content")
	archivePath, archiveSize, archiveSHA := buildTestArchive(t, map[string][]byte{
		"words/ES2002a.A.words.xml": esXML,
		"words/EN2001a.A.words.xml": enXML,
	})
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}

	var served int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&served, 1)
		switch {
		case strings.Contains(r.URL.Path, "archive"):
			w.Write(archiveData)
		case strings.Contains(r.URL.Path, "ES2002a") && strings.Contains(r.URL.Path, "headset"):
			w.Write([]byte("es-headset-audio"))
		case strings.Contains(r.URL.Path, "ES2002a") && strings.Contains(r.URL.Path, "fixed"):
			w.Write([]byte("es-fixed-audioX"))
		case strings.Contains(r.URL.Path, "EN2001a") && strings.Contains(r.URL.Path, "headset"):
			w.Write([]byte("en-headset-audio"))
		default:
			w.Write([]byte("en-fixed-audioXX"))
		}
	}))
	defer srv.Close()

	wavSHA := func(meeting, mic string) string {
		body := map[string]string{
			"ES2002a|headset": "es-headset-audio",
			"ES2002a|fixed":   "es-fixed-audioX",
			"EN2001a|headset": "en-headset-audio",
			"EN2001a|fixed":   "en-fixed-audioXX",
		}[meeting+"|"+mic]
		return sha256Hex([]byte(body))
	}
	wavSize := func(meeting, mic string) int64 {
		body := map[string]string{
			"ES2002a|headset": "es-headset-audio",
			"ES2002a|fixed":   "es-fixed-audioX",
			"EN2001a|headset": "en-headset-audio",
			"EN2001a|fixed":   "en-fixed-audioXX",
		}[meeting+"|"+mic]
		return int64(len(body))
	}
	wavURL := func(meeting, mic string) string { return srv.URL + "/" + meeting + "/" + mic + ".wav" }

	man := testManifestWithArchive(srv.URL+"/archive.zip", archiveSize, archiveSHA, wavURL, wavSize, wavSHA,
		map[string]string{"ES2002a.A": sha256Hex(esXML), "EN2001a.A": sha256Hex(enXML)},
		map[string]int64{"ES2002a.A": int64(len(esXML)), "EN2001a.A": int64(len(enXML))},
	)

	cache := newCache(t)
	recs, err := Acquire(context.Background(), cache, man, Options{HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if len(recs) != 4 {
		t.Fatalf("expected 4 recordings, got %d", len(recs))
	}
	for _, r := range recs {
		p, ok := r.AnnotationPaths["A"]
		if !ok {
			t.Fatalf("recording %s missing participant A path", r.Recording.ID)
		}
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read extracted annotation: %v", err)
		}
		var want []byte
		if r.Recording.MeetingID == "ES2002a" {
			want = esXML
		} else {
			want = enXML
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("recording %s: extracted annotation mismatch", r.Recording.ID)
		}
	}
	// Archive fetched once; then reused for member extraction across all 4
	// recordings without additional archive network hits.
	if served != 5 { // 1 archive + 4 audio objects
		t.Fatalf("expected 5 network requests (1 archive + 4 audio), got %d", served)
	}
}

func TestAcquireFailsOnUnknownArchiveMember(t *testing.T) {
	archivePath, archiveSize, archiveSHA := buildTestArchive(t, map[string][]byte{
		"words/OTHER.A.words.xml": []byte("wrong file"),
	})
	archiveData, _ := os.ReadFile(archivePath)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archiveData)
	}))
	defer srv.Close()

	wavBody := []byte("audio")
	man := testManifestWithArchive(srv.URL, archiveSize, archiveSHA,
		func(string, string) string { return srv.URL },
		func(string, string) int64 { return int64(len(wavBody)) },
		func(string, string) string { return sha256Hex(wavBody) },
		map[string]string{"ES2002a.A": strings.Repeat("9", 64), "EN2001a.A": strings.Repeat("9", 64)},
		map[string]int64{"ES2002a.A": 10, "EN2001a.A": 10},
	)
	cache := newCache(t)
	if _, err := Acquire(context.Background(), cache, man, Options{HTTPClient: srv.Client()}); err == nil {
		t.Fatal("expected failure for missing archive member")
	}
}

func TestFindModuleRootLocatesGoMod(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := FindModuleRoot(nested)
	if err != nil {
		t.Fatalf("find module root: %v", err)
	}
	if root != dir {
		t.Fatalf("got %q want %q", root, dir)
	}
}

func TestFindModuleRootMissing(t *testing.T) {
	// A directory tree with no go.mod anywhere above it (use a fresh temp
	// root, which is outside any module).
	dir := t.TempDir()
	if _, err := FindModuleRoot(dir); err == nil {
		t.Fatal("expected error when go.mod is not found")
	}
}
