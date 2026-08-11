package storage_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/storage"
)

func TestUploadHandler_WebMContentTypeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p, err := storage.NewLocal(dir, "http://example.test", "", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	handler := p.UploadHandler()
	data := append([]byte{0x1a, 0x45, 0xdf, 0xa3}, bytes.Repeat([]byte{0x00}, 252)...)
	grant, err := p.PresignUpload("notes/abc/audio/recording.webm", time.Minute)
	if err != nil {
		t.Fatalf("presign upload: %v", err)
	}
	put := httptest.NewRequest(http.MethodPut, grant.URL, bytes.NewReader(data))
	put.Header.Set("Content-Type", "audio/webm")
	putResponse := httptest.NewRecorder()
	handler.ServeHTTP(putResponse, put)
	if putResponse.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d", putResponse.Code, http.StatusOK)
	}

	downloadURL, err := p.PresignDownload(grant.Key, time.Minute)
	if err != nil {
		t.Fatalf("presign download: %v", err)
	}
	get := httptest.NewRequest(http.MethodGet, downloadURL, nil)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, get)
	if got := getResponse.Header().Get("Content-Type"); got != "audio/webm" {
		t.Fatalf("Content-Type = %q, want audio/webm", got)
	}
	if got := getResponse.Header().Get("Content-Length"); got != "256" {
		t.Fatalf("Content-Length = %q, want 256", got)
	}
	if got := getResponse.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("Accept-Ranges = %q, want bytes", got)
	}
	if !bytes.Equal(getResponse.Body.Bytes(), data) {
		t.Fatal("GET body does not match uploaded WebM bytes")
	}
	if detected := http.DetectContentType(getResponse.Body.Bytes()); detected != "video/webm" {
		t.Fatalf("fixture detection = %q, want video/webm WebM signature", detected)
	}

	rangeRequest := httptest.NewRequest(http.MethodGet, downloadURL, nil)
	rangeRequest.Header.Set("Range", "bytes=100-199")
	rangeResponse := httptest.NewRecorder()
	handler.ServeHTTP(rangeResponse, rangeRequest)
	if rangeResponse.Code != http.StatusPartialContent {
		t.Fatalf("range status = %d, want %d", rangeResponse.Code, http.StatusPartialContent)
	}
	if got := rangeResponse.Header().Get("Content-Range"); got != "bytes 100-199/256" {
		t.Fatalf("Content-Range = %q, want bytes 100-199/256", got)
	}
	if got := rangeResponse.Body.Len(); got != 100 {
		t.Fatalf("range body length = %d, want 100", got)
	}
	if !bytes.Equal(rangeResponse.Body.Bytes(), data[100:200]) {
		t.Fatal("range body does not match requested WebM bytes")
	}
}

func TestUploadHandler_LegacyObjectFallsBackToDetectedContentType(t *testing.T) {
	dir := t.TempDir()
	p, err := storage.NewLocal(dir, "http://example.test", "", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	key := "notes/legacy/audio.bin"
	objectPath := filepath.Join(dir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data := bytes.Repeat([]byte{0x00}, 256)
	if err := os.WriteFile(objectPath, data, 0o644); err != nil {
		t.Fatalf("write legacy object: %v", err)
	}

	downloadURL, err := p.PresignDownload(key, time.Minute)
	if err != nil {
		t.Fatalf("presign download: %v", err)
	}
	response := httptest.NewRecorder()
	p.UploadHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, downloadURL, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want application/octet-stream", got)
	}
	if !bytes.Equal(response.Body.Bytes(), data) {
		t.Fatal("legacy object body does not round-trip")
	}
}

// countingReader wraps an io.Reader and records the total number of bytes
// actually returned across all Read calls. It lets a test observe how much of
// a request body the server really pulled off the wire, independent of what
// status code it eventually returns.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (c *countingReader) BytesRead() int64 { return c.n }

func TestLocalProviderRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p, err := storage.NewLocal(dir, "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	grant, err := p.PresignUpload("notes/abc/audio/x.webm", time.Minute)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if grant.Method != http.MethodPut || !strings.Contains(grant.URL, "sig=") {
		t.Fatalf("bad grant %+v", grant)
	}

	// Before upload: not present.
	if ok, _, _ := p.Verify(grant.Key); ok {
		t.Fatal("verify should be false before upload")
	}

	// Simulate the client PUT by serving the provider's HTTP handler.
	srv := httptest.NewServer(p.UploadHandler())
	defer srv.Close()
	putURL := strings.Replace(grant.URL, "http://example.test", srv.URL, 1)
	req, _ := http.NewRequest(http.MethodPut, putURL, strings.NewReader("audio-bytes"))
	req.Header.Set("Content-Type", "audio/webm")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT failed: status=%v err=%v", resp.StatusCode, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	contentTypePath := filepath.Join(dir, filepath.FromSlash(grant.Key)) + ".contenttype"
	if stored, err := os.ReadFile(contentTypePath); err != nil || string(stored) != "audio/webm" {
		t.Fatalf("stored content type = %q, err=%v", stored, err)
	}

	// After upload: present with correct size.
	ok, size, err := p.Verify(grant.Key)
	if err != nil || !ok || size != int64(len("audio-bytes")) {
		t.Fatalf("verify: ok=%v size=%d err=%v", ok, size, err)
	}

	// A tampered signature is rejected.
	bad := strings.Replace(putURL, "sig=", "sig=deadbeef", 1)
	req2, _ := http.NewRequest(http.MethodPut, bad, strings.NewReader("x"))
	resp2, _ := http.DefaultClient.Do(req2)
	if resp2.StatusCode == http.StatusOK {
		t.Fatal("tampered signature should be rejected")
	}
}

func TestLocalPresignDownloadAndDelete(t *testing.T) {
	dir := t.TempDir()
	p, err := storage.NewLocal(dir, "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	key := "notes/abc/audio/y.webm"

	// Upload an object first via the signed PUT.
	grant, _ := p.PresignUpload(key, time.Minute)
	srv := httptest.NewServer(p.UploadHandler())
	defer srv.Close()
	putURL := strings.Replace(grant.URL, "http://example.test", srv.URL, 1)
	req, _ := http.NewRequest(http.MethodPut, putURL, strings.NewReader("audio-bytes"))
	req.Header.Set("Content-Type", "audio/webm")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT failed: %v status=%v", err, resp.StatusCode)
	}
	resp.Body.Close()

	// PresignDownload yields a signed GET that returns the bytes.
	dl, err := p.PresignDownload(key, time.Minute)
	if err != nil {
		t.Fatalf("presign download: %v", err)
	}
	getURL := strings.Replace(dl, "http://example.test", srv.URL, 1)
	gresp, err := http.Get(getURL)
	if err != nil || gresp.StatusCode != http.StatusOK {
		t.Fatalf("GET failed: %v status=%v", err, gresp.StatusCode)
	}
	body, _ := io.ReadAll(gresp.Body)
	gresp.Body.Close()
	if string(body) != "audio-bytes" {
		t.Fatalf("download body = %q", body)
	}

	// Delete removes the object.
	contentTypePath := filepath.Join(dir, filepath.FromSlash(key)) + ".contenttype"
	if err := p.Delete(key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if ok, _, _ := p.Verify(key); ok {
		t.Fatal("object should be gone after delete")
	}
	if _, err := os.Stat(contentTypePath); !os.IsNotExist(err) {
		t.Fatalf("content type sidecar still exists after delete: %v", err)
	}
	// Delete is idempotent (no error for a missing key).
	if err := p.Delete(key); err != nil {
		t.Fatalf("delete idempotent: %v", err)
	}
}

// Upload URLs (client-facing) use the public base; download URLs (plugin-facing)
// use the internal base. A download URL built on the internal base must still
// verify and serve via the same handler, since the signature is host-independent.
func TestLocalPublicInternalURLSplit(t *testing.T) {
	dir := t.TempDir()
	const public = "https://app.example.com"
	const internal = "http://muesli.internal:8080"
	p, err := storage.NewLocal(dir, public, internal, []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	key := "notes/abc/audio/z.webm"

	// Upload URL uses the public base, not the internal base.
	grant, err := p.PresignUpload(key, time.Minute)
	if err != nil {
		t.Fatalf("presign upload: %v", err)
	}
	if !strings.HasPrefix(grant.URL, public+"/_storage/") {
		t.Fatalf("upload URL %q should use public base %q", grant.URL, public)
	}
	if strings.Contains(grant.URL, internal) {
		t.Fatalf("upload URL %q must not use internal base", grant.URL)
	}

	// Download URL uses the internal base, not the public base.
	dl, err := p.PresignDownload(key, time.Minute)
	if err != nil {
		t.Fatalf("presign download: %v", err)
	}
	if !strings.HasPrefix(dl, internal+"/_storage/") {
		t.Fatalf("download URL %q should use internal base %q", dl, internal)
	}
	if strings.Contains(dl, public) {
		t.Fatalf("download URL %q must not use public base", dl)
	}

	// The download URL (signed on the internal base) must still verify and serve
	// via the handler. Strip the internal host, point it at the test server.
	srv := httptest.NewServer(p.UploadHandler())
	defer srv.Close()

	// Upload the object first (via the public-based PUT URL).
	putURL := strings.Replace(grant.URL, public, srv.URL, 1)
	req, _ := http.NewRequest(http.MethodPut, putURL, strings.NewReader("audio-bytes"))
	req.Header.Set("Content-Type", "audio/webm")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT failed: %v status=%v", err, resp.StatusCode)
	}
	resp.Body.Close()

	// Fetch via the internal-based download URL.
	getURL := strings.Replace(dl, internal, srv.URL, 1)
	gresp, err := http.Get(getURL)
	if err != nil || gresp.StatusCode != http.StatusOK {
		t.Fatalf("GET (internal-base download) failed: %v status=%v", err, gresp.StatusCode)
	}
	body, _ := io.ReadAll(gresp.Body)
	gresp.Body.Close()
	if string(body) != "audio-bytes" {
		t.Fatalf("download body = %q", body)
	}
}

func TestUploadSizeCap(t *testing.T) {
	dir := t.TempDir()
	p, err := storage.NewLocal(dir, "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	handler := p.UploadHandler()

	// signedPath builds a path+query string for a signed PUT on the given key,
	// using the internal signer via PresignUpload.
	signedPath := func(key string) string {
		grant, err := p.PresignUpload(key, time.Minute)
		if err != nil {
			t.Fatalf("presign: %v", err)
		}
		// grant.URL is like "http://example.test/_storage/<key>?exp=...&sig=..."
		// Strip the host so we can pass just the path+query to httptest.
		u := strings.TrimPrefix(grant.URL, "http://example.test")
		return u
	}

	t.Run("over cap via Content-Length header returns 413", func(t *testing.T) {
		path := signedPath("notes/cap/audio/a.webm")
		// Drive the handler directly via httptest.NewRecorder so we can set
		// ContentLength without the HTTP client enforcing body/length consistency.
		req, _ := http.NewRequest(http.MethodPut, path, strings.NewReader("tiny"))
		req.Header.Set("Content-Type", "audio/webm")
		req.ContentLength = storage.MaxUploadBytes + 1
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("expected 413, got %d", rr.Code)
		}
	})

	t.Run("normal upload within cap returns 200", func(t *testing.T) {
		path := signedPath("notes/cap/audio/b.webm")
		req, _ := http.NewRequest(http.MethodPut, path, strings.NewReader("audio-bytes"))
		req.Header.Set("Content-Type", "audio/webm")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
	})

	t.Run("streaming over cap with unknown length returns 413 and removes partial", func(t *testing.T) {
		// Shrink the cap so the streaming MaxBytesReader branch is exercised
		// without moving a real 1 GiB of bytes.
		p.SetMaxUploadBytes(100)
		t.Cleanup(func() { p.SetMaxUploadBytes(storage.MaxUploadBytes) })

		key := "notes/cap/audio/stream.webm"
		path := signedPath(key)
		// ContentLength = -1 (unknown) bypasses the header guard, so the body is
		// streamed through MaxBytesReader, which trips past the cap.
		req, _ := http.NewRequest(http.MethodPut, path, strings.NewReader(strings.Repeat("x", 500)))
		req.Header.Set("Content-Type", "audio/webm")
		req.ContentLength = -1
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("expected 413, got %d", rr.Code)
		}
		// The handler must remove the truncated partial object.
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(key))); !os.IsNotExist(err) {
			t.Fatalf("partial object should not exist, stat err = %v", err)
		}
	})

	t.Run("lying content-length under cap but body over cap returns 413 and removes partial", func(t *testing.T) {
		// Shrink the cap so the streaming MaxBytesReader branch is exercised
		// without moving a real 1 GiB of bytes.
		p.SetMaxUploadBytes(100)
		t.Cleanup(func() { p.SetMaxUploadBytes(storage.MaxUploadBytes) })

		key := "notes/cap/audio/lyingcl.webm"
		path := signedPath(key)
		// ContentLength declares 50 (<= 100 cap), so the up-front header guard
		// `r.ContentLength > l.maxUpload` is false and is bypassed. The body
		// actually streams 500 bytes, which trips MaxBytesReader past the cap.
		req, _ := http.NewRequest(http.MethodPut, path, strings.NewReader(strings.Repeat("x", 500)))
		req.Header.Set("Content-Type", "audio/webm")
		req.ContentLength = 50
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("expected 413, got %d", rr.Code)
		}
		// The handler must remove the truncated partial object.
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(key))); !os.IsNotExist(err) {
			t.Fatalf("partial object should not exist, stat err = %v", err)
		}
	})

	t.Run("streaming over cap reads only a small bounded prefix, not the whole oversized body", func(t *testing.T) {
		// Shrink the cap so the streaming MaxBytesReader branch is exercised,
		// same pattern as the sibling cap tests above.
		const streamCap = 100
		p.SetMaxUploadBytes(streamCap)
		t.Cleanup(func() { p.SetMaxUploadBytes(storage.MaxUploadBytes) })

		key := "notes/cap/audio/countingstream.webm"
		path := signedPath(key)

		// The body source is far larger than the shrunk cap. If the handler
		// buffered (or otherwise consumed) the whole body before checking size,
		// the countingReader below would record close to oversizedBodySize
		// bytes read. If it truly streams and enforces the cap while reading,
		// it must stop shortly after crossing the cap.
		const oversizedBodySize = 5 * 1024 * 1024 // 5 MiB, many times streamCap
		cr := &countingReader{r: strings.NewReader(strings.Repeat("z", oversizedBodySize))}

		req, _ := http.NewRequest(http.MethodPut, path, cr)
		req.Header.Set("Content-Type", "audio/webm")
		// Unknown Content-Length forces the handler to discover the overflow by
		// reading the stream (mirroring the "streaming over cap with unknown
		// length" test above), rather than rejecting on the header alone.
		req.ContentLength = -1
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("expected 413, got %d", rr.Code)
		}
		// The handler must remove the truncated partial object, same as the
		// sibling streaming-cap tests.
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(key))); !os.IsNotExist(err) {
			t.Fatalf("partial object should not exist, stat err = %v", err)
		}

		// The crux of this test: prove the handler stopped reading the source
		// early instead of buffering/consuming the whole oversized body first.
		//
		// http.MaxBytesReader trims every underlying Read request to at most
		// (remaining-allowed+1) bytes, so regardless of io.Copy's internal
		// buffer size (or any ReaderFrom fast path *os.File might use as the
		// io.Copy destination), the total bytes actually pulled from the
		// source is bounded to streamCap+1. A run against this handler
		// confirms bytesRead == streamCap+1 exactly; we assert a small
		// safety-margin bound (streamCap + 4096) instead of the exact value so
		// the test isn't brittle to minor, harmless read-size variance.
		const safetyMargin = 4096
		if got, max := cr.BytesRead(), int64(streamCap)+safetyMargin; got > max {
			t.Fatalf("handler read %d bytes from the source, want <= %d (cap=%d); this looks like it buffered the body instead of streaming it", got, max, streamCap)
		}
		if got := cr.BytesRead(); got >= int64(oversizedBodySize) {
			t.Fatalf("handler read the entire %d-byte oversized body (%d bytes read); cap enforcement is not streaming", oversizedBodySize, got)
		}
	})

	t.Run("wrong content-type returns 415 and writes nothing", func(t *testing.T) {
		key := "notes/cap/audio/wrongtype.webm"
		path := signedPath(key)
		req, _ := http.NewRequest(http.MethodPut, path, strings.NewReader("audio-bytes"))
		req.Header.Set("Content-Type", "text/html")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("expected 415, got %d", rr.Code)
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(key))); !os.IsNotExist(err) {
			t.Fatalf("no object should be written, stat err = %v", err)
		}
	})
	t.Run("missing content-type returns 415 and writes nothing", func(t *testing.T) {
		// No Content-Type header at all; mime.ParseMediaType("") returns an error,
		// so the handler must reject with 415 before touching the filesystem.
		key := "notes/cap/audio/notype.webm"
		path := signedPath(key)
		req, _ := http.NewRequest(http.MethodPut, path, strings.NewReader("audio-bytes"))
		// Deliberately omit req.Header.Set("Content-Type", ...).
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("expected 415, got %d", rr.Code)
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(key))); !os.IsNotExist(err) {
			t.Fatalf("no object should be written, stat err = %v", err)
		}
	})
}

// An empty internalURL falls back to the public base for download URLs.
func TestLocalInternalURLEmptyFallsBackToPublic(t *testing.T) {
	dir := t.TempDir()
	const public = "https://app.example.com"
	p, err := storage.NewLocal(dir, public, "", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	key := "notes/abc/audio/w.webm"

	dl, err := p.PresignDownload(key, time.Minute)
	if err != nil {
		t.Fatalf("presign download: %v", err)
	}
	if !strings.HasPrefix(dl, public+"/_storage/") {
		t.Fatalf("download URL %q should fall back to public base %q", dl, public)
	}

	grant, err := p.PresignUpload(key, time.Minute)
	if err != nil {
		t.Fatalf("presign upload: %v", err)
	}
	if !strings.HasPrefix(grant.URL, public+"/_storage/") {
		t.Fatalf("upload URL %q should use public base %q", grant.URL, public)
	}
}

// TestPathTraversalContainment verifies that a traversal key (e.g. ../evil),
// even when presented with a valid HMAC signature, is always stored inside the
// storage root — never outside it. This tests the path() guard:
//
//	filepath.Clean("/" + key) → strips ../; filepath.Join(root, …) stays inside.
//
// We bypass the HTTP client (which would path-clean the URL before sending) by
// calling handler.ServeHTTP directly and setting r.URL.Path explicitly.
func TestPathTraversalContainment(t *testing.T) {
	cases := []struct {
		name string
		key  string
	}{
		{"simple traversal key", "../evil"},
		{"nested traversal key", "notes/id/../../evil"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Each subtest gets its own empty directory and Local instance so
			// containment is verified independently — a file left by an earlier
			// subtest cannot mask a missing containment check here.
			dir := t.TempDir()
			p, err := storage.NewLocal(dir, "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
			if err != nil {
				t.Fatalf("new: %v", err)
			}
			handler := p.UploadHandler()

			// Obtain a genuine signed grant for the traversal key so the HMAC
			// check inside the handler passes.
			grant, err := p.PresignUpload(tc.key, time.Minute)
			if err != nil {
				t.Fatalf("presign: %v", err)
			}

			// grant.URL = "http://example.test/_storage/<key>?exp=...&sig=..."
			// Extract the raw query string (exp=...&sig=...).
			qIdx := strings.Index(grant.URL, "?")
			if qIdx < 0 {
				t.Fatalf("grant URL has no query: %s", grant.URL)
			}
			rawQuery := grant.URL[qIdx+1:]

			// Build the request using a placeholder path so url.Parse succeeds,
			// then override URL.Path with the traversal key verbatim.  This
			// bypasses HTTP-client path normalization that would strip "..".
			req, err := http.NewRequest(http.MethodPut,
				"http://example.test/_storage/placeholder?"+rawQuery,
				strings.NewReader("audio-bytes"))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			// Set the raw traversal path — the handler reads r.URL.Path directly.
			req.URL.Path = "/_storage/" + tc.key
			req.Header.Set("Content-Type", "audio/webm")

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
			}

			// Containment assertion 1: no file must exist above the storage root.
			// An unguarded handler would write to filepath.Join(parent, "evil").
			parent := filepath.Dir(dir)
			escapedPath := filepath.Join(parent, "evil")
			if _, statErr := os.Stat(escapedPath); !os.IsNotExist(statErr) {
				t.Fatalf("traversal escaped root: file found at %s (statErr=%v)", escapedPath, statErr)
			}

			// Containment assertion 2: the file must exist inside the root.
			// Both traversal keys normalise to "evil" relative to root.
			containedPath := filepath.Join(dir, "evil")
			if _, statErr := os.Stat(containedPath); statErr != nil {
				t.Fatalf("expected file inside root at %s, stat err: %v", containedPath, statErr)
			}
		})
	}
}

// TestNewLocal_ShortKeyError verifies that NewLocal rejects a signing key that is
// shorter than 16 bytes (the minimum enforced by the implementation).
func TestNewLocal_ShortKeyError(t *testing.T) {
	_, err := storage.NewLocal(t.TempDir(), "http://x", "", []byte("short"))
	if err == nil {
		t.Fatal("want non-nil error for a signing key shorter than 16 bytes, got nil")
	}
}

// TestUploadHandler_ExpiredURL verifies that a request with an already-expired
// `exp` query param is rejected with HTTP 403 before the signature is checked.
func TestUploadHandler_ExpiredURL(t *testing.T) {
	dir := t.TempDir()
	p, err := storage.NewLocal(dir, "http://example.test", "", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	// The handler checks expiry BEFORE the signature, so sig can be anything.
	// exp=1 is the Unix epoch — always in the past.
	req, _ := http.NewRequest(http.MethodPut,
		"/_storage/notes/abc/audio/x.webm?exp=1&sig=deadbeef",
		nil)
	rr := httptest.NewRecorder()
	p.UploadHandler().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("want 403 for expired URL, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestUploadHandler_MethodNotAllowed verifies that an unsupported HTTP method
// (DELETE) on a fully-valid signed URL returns HTTP 405.
func TestUploadHandler_MethodNotAllowed(t *testing.T) {
	dir := t.TempDir()
	p, err := storage.NewLocal(dir, "http://example.test", "", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	// PresignUpload produces a valid (non-expired, correctly signed) URL.
	// The HMAC signature does not include the HTTP method, so it stays valid
	// when we switch the method from PUT to DELETE.
	grant, err := p.PresignUpload("notes/abc/audio/z.webm", time.Minute)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	pathAndQuery := strings.TrimPrefix(grant.URL, "http://example.test")
	req, _ := http.NewRequest(http.MethodDelete, pathAndQuery, nil)
	rr := httptest.NewRecorder()
	p.UploadHandler().ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405 for unsupported method, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestUploadHandler_ContentTypeAllowlist covers the audio Content-Type
// allowlist enforced on the PUT path (HRD01): an allowed type succeeds, a
// disallowed-but-present type is rejected with 415 and names the rejected
// type, and a missing/malformed Content-Type header is rejected with a
// distinct 415 message.
func TestUploadHandler_ContentTypeAllowlist(t *testing.T) {
	dir := t.TempDir()
	p, err := storage.NewLocal(dir, "http://example.test", "", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	handler := p.UploadHandler()

	signedPath := func(key string) string {
		grant, err := p.PresignUpload(key, time.Minute)
		if err != nil {
			t.Fatalf("presign: %v", err)
		}
		return strings.TrimPrefix(grant.URL, "http://example.test")
	}

	put := func(t *testing.T, key, contentType string, setHeader bool) *httptest.ResponseRecorder {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPut, signedPath(key), strings.NewReader("audio-bytes"))
		if setHeader {
			req.Header.Set("Content-Type", contentType)
		}
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr
	}

	t.Run("allowed default type (audio/webm) succeeds", func(t *testing.T) {
		rr := put(t, "notes/ct/audio/a.webm", "audio/webm", true)
		if rr.Code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("another allowed default type (audio/mpeg) succeeds", func(t *testing.T) {
		rr := put(t, "notes/ct/audio/b.mp3", "audio/mpeg", true)
		if rr.Code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("disallowed audio subtype is rejected with 415 naming the type", func(t *testing.T) {
		rr := put(t, "notes/ct/audio/c.bin", "audio/x-whatever", true)
		if rr.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("want 415, got %d: %s", rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "audio/x-whatever") {
			t.Fatalf("expected error body to name the rejected type, got %q", rr.Body.String())
		}
	})

	t.Run("non-audio content type is rejected with 415", func(t *testing.T) {
		rr := put(t, "notes/ct/audio/d.bin", "image/png", true)
		if rr.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("want 415, got %d: %s", rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "image/png") {
			t.Fatalf("expected error body to name the rejected type, got %q", rr.Body.String())
		}
	})

	t.Run("missing content type header is rejected with a distinct 415 message", func(t *testing.T) {
		rr := put(t, "notes/ct/audio/e.bin", "", false)
		if rr.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("want 415, got %d: %s", rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "missing or invalid content type") {
			t.Fatalf("expected distinct missing-content-type message, got %q", rr.Body.String())
		}
	})

	t.Run("malformed content type header is rejected with a distinct 415 message", func(t *testing.T) {
		rr := put(t, "notes/ct/audio/f.bin", ";;;not-a-media-type", true)
		if rr.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("want 415, got %d: %s", rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "missing or invalid content type") {
			t.Fatalf("expected distinct malformed-content-type message, got %q", rr.Body.String())
		}
	})
}

// TestUploadHandler_ContentTypeAllowlistOverride verifies SetAllowedContentTypes
// changes accept/reject behavior in both directions: a custom allowlist can
// reject a type that's allowed by default (e.g. audio/webm) and accept a type
// that isn't in the default list (e.g. audio/x-custom).
func TestUploadHandler_ContentTypeAllowlistOverride(t *testing.T) {
	dir := t.TempDir()
	p, err := storage.NewLocal(dir, "http://example.test", "", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	p.SetAllowedContentTypes([]string{"audio/x-custom"})
	handler := p.UploadHandler()

	signedPath := func(key string) string {
		grant, err := p.PresignUpload(key, time.Minute)
		if err != nil {
			t.Fatalf("presign: %v", err)
		}
		return strings.TrimPrefix(grant.URL, "http://example.test")
	}

	t.Run("default-allowed type is now rejected under the override", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPut, signedPath("notes/ovr/audio/a.webm"), strings.NewReader("audio-bytes"))
		req.Header.Set("Content-Type", "audio/webm")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("want 415 for audio/webm under custom allowlist, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("custom type is now accepted under the override", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPut, signedPath("notes/ovr/audio/b.bin"), strings.NewReader("audio-bytes"))
		req.Header.Set("Content-Type", "audio/x-custom")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("want 200 for audio/x-custom under custom allowlist, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("SetAllowedContentTypes(nil) restores the default allowlist", func(t *testing.T) {
		p.SetAllowedContentTypes(nil)
		req, _ := http.NewRequest(http.MethodPut, signedPath("notes/ovr/audio/c.webm"), strings.NewReader("audio-bytes"))
		req.Header.Set("Content-Type", "audio/webm")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("want 200 for audio/webm after restoring defaults, got %d: %s", rr.Code, rr.Body.String())
		}
	})
}
