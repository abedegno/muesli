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
	// The mismatched object must never be promoted to its content-addressed
	// destination, and no sibling temp file may survive the failure.
	if _, err := os.Stat(cache.DownloadPath(obj.SHA256)); !os.IsNotExist(err) {
		t.Fatalf("expected checksum-mismatched object not to be promoted, stat err: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(cache.Root, "downloads"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Fatalf("unexpected leftover file %q after checksum mismatch", e.Name())
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

	// A same-server redirect (https -> https, via a relative Location) must
	// be followed successfully by the enforced client, actually exercising
	// the CheckRedirect code path rather than fetching the target directly.
	mux := http.NewServeMux()
	mux.HandleFunc("/target", func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	})
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/target", http.StatusFound)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	client := srv.Client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("non-https redirect")
		}
		return nil
	}
	obj := Object{URL: srv.URL + "/redirect", SizeBytes: int64(len(body)), SHA256: sha256Hex(body)}
	cache := newCache(t)
	if _, err := EnsureObject(context.Background(), cache, obj, Options{HTTPClient: client}); err != nil {
		t.Fatalf("https-to-https redirect should succeed: %v", err)
	}

	// A redirect to a non-https absolute URL must be rejected by the same
	// CheckRedirect policy, proving the guard is real and not vacuous.
	insecureMux := http.NewServeMux()
	insecureMux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.test/insecure", http.StatusFound)
	})
	insecureRedirector := httptest.NewTLSServer(insecureMux)
	defer insecureRedirector.Close()

	badClient := insecureRedirector.Client()
	badClient.CheckRedirect = client.CheckRedirect
	badObj := Object{URL: insecureRedirector.URL + "/redirect", SizeBytes: int64(len(body)), SHA256: sha256Hex(body)}
	cache2 := newCache(t)
	if _, err := EnsureObject(context.Background(), cache2, badObj, Options{HTTPClient: badClient}); err == nil {
		t.Fatal("expected redirect to a non-https url to be rejected")
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

	// The slow server writes and flushes a partial body -- so bytes are
	// actually being streamed into the sibling temp file -- then blocks
	// until the request context is canceled, so cancellation genuinely
	// interrupts an in-flight stream rather than a request that never
	// started.
	partial := []byte("partial-bytes-before-cancel")
	streaming := make(chan struct{})
	handlerDone := make(chan struct{})
	srvSlow := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(partial)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		close(streaming)
		<-r.Context().Done()
		close(handlerDone)
	}))
	defer srvSlow.Close()

	slowObj := Object{URL: srvSlow.URL, SizeBytes: int64(len(partial)) + 1000, SHA256: strings.Repeat("1", 64)}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := EnsureObject(ctx, cache, slowObj, Options{HTTPClient: srvSlow.Client()})
		result <- err
	}()

	select {
	case <-streaming:
	case <-time.After(5 * time.Second):
		t.Fatal("server never began streaming a response body")
	}
	cancel()

	select {
	case err := <-result:
		if err == nil {
			t.Fatal("expected cancellation error for an in-flight stream")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("EnsureObject did not return after context cancellation")
	}

	select {
	case <-handlerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("server handler never observed the canceled request")
	}

	// The canceled, partially-streamed object must never be promoted.
	if _, err := os.Stat(cache.DownloadPath(slowObj.SHA256)); !os.IsNotExist(err) {
		t.Fatalf("expected canceled download not to be promoted, stat err: %v", err)
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
		t.Fatal("expected no leftover temp file after mid-stream cancellation")
	}
	if !sawGood {
		t.Fatal("expected the previously verified good download to be preserved")
	}
}

func TestEnsureObjectTimeout(t *testing.T) {
	// The handler may or may not have been dispatched before the very short
	// per-object timeout fires (a genuine race, since dialing/TLS handshake
	// takes nonzero time). started is closed only if the handler actually
	// ran; waiting on it must therefore be bounded, or a run where the
	// timeout wins the race hangs forever waiting for a signal that will
	// never come.
	started := make(chan struct{})
	handlerDone := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(handlerDone)
	}))
	defer srv.Close()
	obj := Object{URL: srv.URL, SizeBytes: 5, SHA256: strings.Repeat("2", 64)}
	cache := newCache(t)
	_, err := EnsureObject(context.Background(), cache, obj, Options{HTTPClient: srv.Client(), Timeout: 10 * time.Millisecond})
	if err == nil {
		t.Fatal("expected timeout error")
	}

	select {
	case <-started:
		// The handler did start; give it a bounded window to observe the
		// canceled request context before checking cleanup.
		select {
		case <-handlerDone:
		case <-time.After(5 * time.Second):
			t.Fatal("handler did not observe request cancellation in time")
		}
	case <-time.After(2 * time.Second):
		// The timeout fired before the handler was ever dispatched -- also
		// a valid outcome; nothing more to wait for.
	}

	// Either way, the timed-out object must never be promoted, and no
	// sibling temp file may survive.
	if _, statErr := os.Stat(cache.DownloadPath(obj.SHA256)); !os.IsNotExist(statErr) {
		t.Fatalf("expected timed-out download not to be promoted, stat err: %v", statErr)
	}
	entries, err := os.ReadDir(filepath.Join(cache.Root, "downloads"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("unexpected leftover temp file %q after timeout", e.Name())
		}
	}
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

// testMeetings is the schema-2 split membership used by acquisition
// fixtures: meeting ID, class, split.
var testMeetings = []struct {
	id    string
	class string
	split manifest.Split
}{
	{"EN2001a", manifest.ClassNonScenario, manifest.SplitTuning},
	{"ES2002a", manifest.ClassScenario, manifest.SplitTuning},
	{"EN2002a", manifest.ClassNonScenario, manifest.SplitHeldOut},
	{"ES2004a", manifest.ClassScenario, manifest.SplitHeldOut},
}

// micFile maps the fixture's short mic names to the required AMI basename
// suffixes.
var micFile = map[string]string{"headset": manifest.HeadsetAudioSuffix, "fixed": manifest.FixedDistantAudioSuffix}

// audioPath is the URL path each fixture WAV is served at.
func audioPath(meeting, mic string) string {
	return "/amicorpus/" + meeting + "/audio/" + meeting + micFile[mic]
}

func testManifestWithArchive(archiveURL string, archiveSize int64, archiveSHA string, wavURL func(meeting, mic string) string, wavSize func(meeting, mic string) int64, wavSHA func(meeting, mic string) string, annSHA map[string]string, annSize map[string]int64) *manifest.Manifest {
	sel0 := 0
	mk := func(id, class string, split manifest.Split) manifest.Meeting {
		return manifest.Meeting{
			ID:    id,
			Class: class,
			Split: split,
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
	man := &manifest.Manifest{
		SchemaVersion:     manifest.SchemaVersion,
		AnnotationArchive: manifest.ArchiveObject{URL: archiveURL, SizeBytes: archiveSize, SHA256: archiveSHA},
	}
	for _, m := range testMeetings {
		man.Meetings = append(man.Meetings, mk(m.id, m.class, m.split))
	}
	return man
}

// fixtureServer serves exactly the mapped paths and fails the test on any
// other request -- there is deliberately no catch-all response.
func fixtureServer(t *testing.T, bodies map[string][]byte, served *int32) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := bodies[r.URL.Path]
		if !ok {
			t.Errorf("unexpected request for %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if served != nil {
			atomic.AddInt32(served, 1)
		}
		w.Write(body)
	}))
}

func TestAcquireExtractsAndVerifiesAnnotationMembers(t *testing.T) {
	xmls := map[string][]byte{}
	archiveFiles := map[string][]byte{}
	for _, m := range testMeetings {
		xmls[m.id] = []byte("<nite:root/>" + m.id + " content")
		archiveFiles["words/"+m.id+".A.words.xml"] = xmls[m.id]
	}
	archivePath, archiveSize, archiveSHA := buildTestArchive(t, archiveFiles)
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}

	wavBody := func(meeting, mic string) []byte { return []byte(meeting + "-" + mic + "-audio") }
	bodies := map[string][]byte{"/archive.zip": archiveData}
	for _, m := range testMeetings {
		for _, mic := range []string{"headset", "fixed"} {
			bodies[audioPath(m.id, mic)] = wavBody(m.id, mic)
		}
	}
	var served int32
	srv := fixtureServer(t, bodies, &served)
	defer srv.Close()

	wavSHA := func(meeting, mic string) string { return sha256Hex(wavBody(meeting, mic)) }
	wavSize := func(meeting, mic string) int64 { return int64(len(wavBody(meeting, mic))) }
	wavURL := func(meeting, mic string) string { return srv.URL + audioPath(meeting, mic) }

	annSHA := map[string]string{}
	annSize := map[string]int64{}
	for id, x := range xmls {
		annSHA[id+".A"] = sha256Hex(x)
		annSize[id+".A"] = int64(len(x))
	}
	man := testManifestWithArchive(srv.URL+"/archive.zip", archiveSize, archiveSHA, wavURL, wavSize, wavSHA, annSHA, annSize)

	cache := newCache(t)
	recs, err := Acquire(context.Background(), cache, man, Options{HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if len(recs) != 8 {
		t.Fatalf("expected 8 recordings, got %d", len(recs))
	}
	splits := map[manifest.Split]int{}
	for _, r := range recs {
		splits[r.Recording.Split]++
		p, ok := r.AnnotationPaths["A"]
		if !ok {
			t.Fatalf("recording %s missing participant A path", r.Recording.ID)
		}
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read extracted annotation: %v", err)
		}
		if !bytes.Equal(got, xmls[r.Recording.MeetingID]) {
			t.Fatalf("recording %s: extracted annotation mismatch", r.Recording.ID)
		}
		audio, err := os.ReadFile(r.AudioPath)
		if err != nil {
			t.Fatal(err)
		}
		wantMic := "headset"
		if r.Recording.Mic == manifest.MicFixedDistant {
			wantMic = "fixed"
		}
		if !bytes.Equal(audio, wavBody(r.Recording.MeetingID, wantMic)) {
			t.Fatalf("recording %s: audio bytes belong to another recording", r.Recording.ID)
		}
	}
	if splits[manifest.SplitTuning] != 4 || splits[manifest.SplitHeldOut] != 4 {
		t.Fatalf("expected 4 recordings per split, got %v", splits)
	}
	// Archive fetched once; then reused for member extraction across all 8
	// recordings without additional archive network hits.
	if served != 9 { // 1 archive + 8 audio objects
		t.Fatalf("expected 9 network requests (1 archive + 8 audio), got %d", served)
	}
}

func TestAcquireFailsOnUnknownArchiveMember(t *testing.T) {
	archivePath, archiveSize, archiveSHA := buildTestArchive(t, map[string][]byte{
		"words/OTHER.A.words.xml": []byte("wrong file"),
	})
	archiveData, _ := os.ReadFile(archivePath)
	bodies := map[string][]byte{"/archive.zip": archiveData}
	wavBody := func(meeting, mic string) []byte { return []byte(meeting + mic) }
	for _, m := range testMeetings {
		for _, mic := range []string{"headset", "fixed"} {
			bodies[audioPath(m.id, mic)] = wavBody(m.id, mic)
		}
	}
	srv := fixtureServer(t, bodies, nil)
	defer srv.Close()

	annSHA := map[string]string{}
	annSize := map[string]int64{}
	for _, m := range testMeetings {
		annSHA[m.id+".A"] = strings.Repeat("9", 64)
		annSize[m.id+".A"] = 10
	}
	man := testManifestWithArchive(srv.URL+"/archive.zip", archiveSize, archiveSHA,
		func(meeting, mic string) string { return srv.URL + audioPath(meeting, mic) },
		func(meeting, mic string) int64 { return int64(len(wavBody(meeting, mic))) },
		func(meeting, mic string) string { return sha256Hex(wavBody(meeting, mic)) },
		annSHA, annSize,
	)
	cache := newCache(t)
	_, err := Acquire(context.Background(), cache, man, Options{HTTPClient: srv.Client()})
	if err == nil {
		t.Fatal("expected failure for missing archive member")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected a missing-member diagnostic, got: %v", err)
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
