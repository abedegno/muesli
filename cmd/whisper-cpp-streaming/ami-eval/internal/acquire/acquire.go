// Package acquire downloads, verifies, and caches AMI corpus source objects
// named by the pinned manifest (muesli#778). It never trusts a pin without
// rehashing the bytes it actually has, keeps the cache confined to a safe,
// git-ignored location, and never performs network I/O in --offline mode.
package acquire

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/abedegno/muesli/cmd/whisper-cpp-streaming/ami-eval/internal/manifest"
)

// DefaultObjectTimeout is the per-object network timeout: generous enough
// for a slow link on a ~170MB AMI recording, bounded enough to fail a truly
// stalled transfer rather than hang the whole evaluation run.
const DefaultObjectTimeout = 15 * time.Minute

// repoTopLevelDenyList names every real top-level source/output directory in
// this repository (plus .github). It is a hardcoded literal, not a runtime
// directory listing or Git query -- see ResolveCache. Dot-directories other
// than .github (tooling/metadata/cache namespaces such as .cache itself) are
// deliberately excluded; TestDenyListCompleteness enforces that every
// current top-level directory is represented here.
var repoTopLevelDenyList = []string{
	".github",
	"agents",
	"bircher",
	"build",
	"cmd",
	"coverage-floors",
	"docs",
	"e2e",
	"infra",
	"internal",
	"native",
	// node_modules is git-ignored, not tracked source -- but CI installs it
	// as a real top-level directory before some jobs run Go tests, and a
	// local dev shell may have installed it too. Deny it defensively like
	// every other real top-level directory.
	"node_modules",
	"plugins",
	"scripts",
	"src",
	"test",
	"testdata",
	"web",
}

// FindModuleRoot walks upward from start looking for go.mod, never invoking
// Git. It returns the directory containing go.mod.
func FindModuleRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("acquire: go.mod not found above %q", start)
		}
		dir = parent
	}
}

// ResolveCache validates and returns the absolute cache root to use.
// cachePath empty selects the default, <moduleRoot>/.cache/ami-vad-eval,
// which is the sole path this function accepts under the module root; any
// other path resolving to, or nested beneath, a listed repository
// source/output directory is rejected. Symlinks are resolved on both the
// candidate and every deny-list directory (through however much of each
// path already exists) so an alias cannot bypass the check. External paths
// (outside the module root entirely, e.g. a shared or hosted cache) are
// accepted.
func ResolveCache(moduleRoot, cachePath string) (string, error) {
	defaultCache := filepath.Join(moduleRoot, ".cache", "ami-vad-eval")
	if strings.TrimSpace(cachePath) == "" {
		cachePath = defaultCache
	}
	abs, err := filepath.Abs(cachePath)
	if err != nil {
		return "", fmt.Errorf("acquire: resolve cache path: %w", err)
	}
	resolvedAbs, err := resolveExisting(abs)
	if err != nil {
		return "", fmt.Errorf("acquire: resolve cache path: %w", err)
	}

	for _, name := range repoTopLevelDenyList {
		denyDir := filepath.Join(moduleRoot, name)
		if _, err := os.Lstat(denyDir); err != nil {
			continue // not present in this checkout; nothing to alias
		}
		resolvedDeny, err := resolveExisting(denyDir)
		if err != nil {
			continue
		}
		if sameOrNested(resolvedAbs, resolvedDeny) {
			return "", fmt.Errorf("acquire: cache path %q is not allowed: nested under repository directory %q", cachePath, name)
		}
	}
	return abs, nil
}

// resolveExisting resolves symlinks along the longest existing ancestor of
// path and rejoins the remaining (not-yet-created) suffix unresolved, since
// components that don't exist cannot be symlinks.
func resolveExisting(path string) (string, error) {
	clean := filepath.Clean(path)
	var suffix []string
	cur := clean
	for {
		if _, err := os.Lstat(cur); err == nil {
			resolved, err := filepath.EvalSymlinks(cur)
			if err != nil {
				return "", err
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return clean, nil // nothing on this path exists; nothing to resolve
		}
		suffix = append(suffix, filepath.Base(cur))
		cur = parent
	}
}

func sameOrNested(child, ancestor string) bool {
	if child == ancestor {
		return true
	}
	return strings.HasPrefix(child, ancestor+string(filepath.Separator))
}

// Cache is a resolved, validated cache root providing the layout the rest of
// the evaluator relies on.
type Cache struct {
	Root string
}

// DownloadPath returns the content-addressed path for a verified source
// object (a downloaded file or a verified archive member).
func (c Cache) DownloadPath(sha256Hex string) string {
	return filepath.Join(c.Root, "downloads", sha256Hex)
}

// PreparedDir returns the directory for one recording's canonical prepared
// audio and reference data under a given manifest digest.
func (c Cache) PreparedDir(manifestDigest, recordingID string) string {
	return filepath.Join(c.Root, "prepared", manifestDigest, recordingID)
}

// ResultsDir returns the directory for one manifest digest / evaluation
// version's raw matrix results.
func (c Cache) ResultsDir(manifestDigest, evaluationVersion string) string {
	return filepath.Join(c.Root, "results", manifestDigest, evaluationVersion)
}

// Object is one network-fetchable, checksum-pinned source object.
type Object struct {
	URL       string
	SizeBytes int64
	SHA256    string
}

// Options controls acquisition behavior.
type Options struct {
	// Offline forbids all network use; every required object must already
	// be present and verified in the cache.
	Offline bool
	// Timeout bounds one object's network fetch. Zero selects
	// DefaultObjectTimeout.
	Timeout time.Duration
	// HTTPClient is used for network fetches. Nil selects a client whose
	// CheckRedirect rejects any non-HTTPS hop. Tests inject one pointed at
	// an httptest server.
	HTTPClient *http.Client
}

func (o Options) timeout() time.Duration {
	if o.Timeout > 0 {
		return o.Timeout
	}
	return DefaultObjectTimeout
}

func (o Options) client() *http.Client {
	if o.HTTPClient != nil {
		return o.HTTPClient
	}
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return fmt.Errorf("acquire: redirect to non-https url rejected")
			}
			return nil
		},
	}
}

// hashFile returns the size and lowercase hex SHA-256 of an existing file.
func hashFile(path string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

// quarantine moves a corrupt cached object aside with a diagnostic-bearing
// suffix so it stops being reused, without deleting evidence outright.
func quarantine(path string) (string, error) {
	dest := fmt.Sprintf("%s.quarantined-%d", path, time.Now().UnixNano())
	if err := os.Rename(path, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// verified reports whether the object already exists at dest with the
// expected size and hash, quarantining it first if it exists but does not
// verify.
func verified(dest string, obj Object) (bool, error) {
	info, err := os.Stat(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.Size() == obj.SizeBytes {
		if _, sum, err := hashFile(dest); err == nil && sum == obj.SHA256 {
			return true, nil
		}
	}
	if _, err := quarantine(dest); err != nil {
		return false, fmt.Errorf("acquire: quarantine mismatched object %q: %w", dest, err)
	}
	return false, nil
}

// EnsureObject guarantees obj is present and verified at
// cache.DownloadPath(obj.SHA256), fetching it if necessary and permitted.
// It streams into a sibling temporary file under a byte ceiling while
// hashing, then promotes atomically only after size and SHA-256 match.
func EnsureObject(ctx context.Context, cache Cache, obj Object, opts Options) (string, error) {
	dest := cache.DownloadPath(obj.SHA256)
	ok, err := verified(dest, obj)
	if err != nil {
		return "", err
	}
	if ok {
		return dest, nil
	}
	if opts.Offline {
		return "", fmt.Errorf("acquire: offline mode requires a verified cached object for sha256 %s, none found", obj.SHA256)
	}
	if err := fetchToDest(ctx, obj, dest, opts); err != nil {
		return "", err
	}
	return dest, nil
}

func fetchToDest(ctx context.Context, obj Object, dest string, opts Options) error {
	if !strings.HasPrefix(strings.ToLower(obj.URL), "https://") {
		return fmt.Errorf("acquire: object url must be https: %s", redactURL(obj.URL))
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, opts.timeout())
	defer cancel()

	req, err := http.NewRequestWithContext(timeoutCtx, http.MethodGet, obj.URL, nil)
	if err != nil {
		return fmt.Errorf("acquire: build request: %w", err)
	}
	resp, err := opts.client().Do(req)
	if err != nil {
		return fmt.Errorf("acquire: fetch %s: %w", redactURL(obj.URL), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("acquire: fetch %s: unexpected status %d", redactURL(obj.URL), resp.StatusCode)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), filepath.Base(dest)+".tmp-*")
	if err != nil {
		return fmt.Errorf("acquire: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			tmp.Close()
			os.Remove(tmpPath)
		}
	}()

	h := sha256.New()
	// The byte ceiling is the manifest-pinned size plus one: reading past it
	// lets us report a clean "too large" diagnostic instead of exhausting
	// disk on a runaway or spoofed response, while still allowing the exact
	// pinned size through.
	limited := io.LimitReader(resp.Body, obj.SizeBytes+1)
	n, err := io.Copy(io.MultiWriter(tmp, h), limited)
	if err != nil {
		return fmt.Errorf("acquire: stream %s: %w", redactURL(obj.URL), err)
	}
	if n != obj.SizeBytes {
		return fmt.Errorf("acquire: %s: expected %d bytes, got %d", redactURL(obj.URL), obj.SizeBytes, n)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if sum != obj.SHA256 {
		return fmt.Errorf("acquire: %s: sha256 mismatch (expected %s)", redactURL(obj.URL), obj.SHA256)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("acquire: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("acquire: close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return fmt.Errorf("acquire: promote object: %w", err)
	}
	cleanup = false
	return nil
}

// redactURL keeps diagnostics actionable without leaking query strings that
// might carry credentials or signed-URL tokens.
func redactURL(raw string) string {
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		return raw[:i]
	}
	return raw
}

// AcquiredRecording is one manifest recording with every source object it
// needs verified and resolved to a local cache path.
type AcquiredRecording struct {
	Recording       manifest.Recording
	AudioPath       string
	AnnotationPaths map[string]string // participant ID -> verified local XML path
}

// Acquire validates the manifest, then verifies (fetching only what is
// missing and permitted) every object every recording needs: each
// recording's audio and the shared annotation archive, whose pinned members
// are extracted and independently verified. Any failure stops before any
// later stage runs.
func Acquire(ctx context.Context, cache Cache, man *manifest.Manifest, opts Options) ([]AcquiredRecording, error) {
	if err := man.Validate(); err != nil {
		return nil, err
	}

	archiveObj := Object{URL: man.AnnotationArchive.URL, SizeBytes: man.AnnotationArchive.SizeBytes, SHA256: man.AnnotationArchive.SHA256}
	archivePath, err := EnsureObject(ctx, cache, archiveObj, opts)
	if err != nil {
		return nil, fmt.Errorf("acquire: annotation archive: %w", err)
	}

	recordings := man.Recordings()
	out := make([]AcquiredRecording, 0, len(recordings))
	// Cache extracted-member verification results across recordings, since
	// both microphone conditions of one meeting share the same annotations.
	memberCache := map[string]string{}
	for _, rec := range recordings {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		audioObj := Object{URL: rec.Audio.URL, SizeBytes: rec.Audio.SizeBytes, SHA256: rec.Audio.SHA256}
		audioPath, err := EnsureObject(ctx, cache, audioObj, opts)
		if err != nil {
			return nil, fmt.Errorf("acquire: recording %s audio: %w", rec.ID, err)
		}

		annPaths := make(map[string]string, len(rec.Annotations))
		for _, ann := range rec.Annotations {
			key := ann.ParticipantID + "|" + ann.SHA256
			if p, ok := memberCache[key]; ok {
				annPaths[ann.ParticipantID] = p
				continue
			}
			p, err := ensureArchiveMember(cache, archivePath, ann)
			if err != nil {
				return nil, fmt.Errorf("acquire: recording %s annotation %s: %w", rec.ID, ann.ParticipantID, err)
			}
			memberCache[key] = p
			annPaths[ann.ParticipantID] = p
		}

		out = append(out, AcquiredRecording{Recording: rec, AudioPath: audioPath, AnnotationPaths: annPaths})
	}
	return out, nil
}

// ensureArchiveMember extracts one pinned member from the verified
// annotation archive and promotes it to its own content-addressed cache
// entry after independently verifying its size and SHA-256, purely from
// local data -- compatible with --offline once the archive itself is
// cached.
func ensureArchiveMember(cache Cache, archivePath string, ann manifest.AnnotationObject) (string, error) {
	dest := cache.DownloadPath(ann.SHA256)
	if ok, err := verified(dest, Object{SizeBytes: ann.SizeBytes, SHA256: ann.SHA256}); err != nil {
		return "", err
	} else if ok {
		return dest, nil
	}

	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", fmt.Errorf("open annotation archive: %w", err)
	}
	defer zr.Close()

	var member *zip.File
	for _, f := range zr.File {
		if f.Name == ann.Member {
			member = f
			break
		}
	}
	if member == nil {
		return "", fmt.Errorf("annotation archive member %q not found", ann.Member)
	}

	rc, err := member.Open()
	if err != nil {
		return "", fmt.Errorf("open annotation archive member %q: %w", ann.Member, err)
	}
	defer rc.Close()

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), filepath.Base(dest)+".tmp-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			tmp.Close()
			os.Remove(tmpPath)
		}
	}()

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), rc)
	if err != nil {
		return "", fmt.Errorf("extract annotation archive member %q: %w", ann.Member, err)
	}
	if n != ann.SizeBytes {
		return "", fmt.Errorf("annotation archive member %q: expected %d bytes, got %d", ann.Member, ann.SizeBytes, n)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if sum != ann.SHA256 {
		return "", fmt.Errorf("annotation archive member %q: sha256 mismatch (expected %s)", ann.Member, ann.SHA256)
	}
	if err := tmp.Sync(); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return "", err
	}
	cleanup = false
	return dest, nil
}

// RepoTopLevelDenyList returns a copy of the maintained deny-list literal,
// exposed for the completeness test.
func RepoTopLevelDenyList() []string {
	out := make([]string, len(repoTopLevelDenyList))
	copy(out, repoTopLevelDenyList)
	sort.Strings(out)
	return out
}
