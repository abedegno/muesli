package storage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// MaxUploadBytes caps a single signed PUT body. Generous for meeting audio; guards
// against an unbounded write exhausting disk.
const MaxUploadBytes = 1 << 30 // 1 GiB

// DefaultAllowedContentTypes is the built-in audio Content-Type allowlist for
// the upload PUT path, covering the audio/* family the roadmap calls out
// (mpeg, mp4/m4a, wav, ogg/opus, webm, flac) plus common vendor variants.
// audio/webm MUST stay in this list: it is the desktop client's actual
// recorder mimeType (see src/main/muesliClient.ts's uploadAudio default).
// Overridable via MUESLI_UPLOAD_ALLOWED_CONTENT_TYPES (see internal/config).
var DefaultAllowedContentTypes = []string{
	"audio/mpeg",
	"audio/mp3",
	"audio/mp4",
	"audio/m4a",
	"audio/x-m4a",
	"audio/wav",
	"audio/x-wav",
	"audio/wave",
	"audio/vnd.wave",
	"audio/ogg",
	"audio/opus",
	"audio/webm",
	"audio/flac",
	"audio/x-flac",
}

// Local is a filesystem-backed Provider that signs its own upload URLs with an
// HMAC, mirroring the presigned-URL pattern S3/MinIO use.
type Local struct {
	root string
	// publicURL is the client-facing base for upload (PUT) URLs handed to the host.
	publicURL string
	// internalURL is the plugin-facing base for download (GET) URLs handed to
	// plugins; it lets a containerized plugin fetch by the server's in-network
	// name. Falls back to publicURL when empty.
	internalURL string
	secret      []byte
	// maxUpload caps a single signed PUT body. Defaults to MaxUploadBytes;
	// overridable via SetMaxUploadBytes (primarily for tests and tuning).
	maxUpload int64
	// allowedContentTypes is the audio Content-Type allowlist enforced on the
	// upload PUT path. Defaults to DefaultAllowedContentTypes; overridable via
	// SetAllowedContentTypes (wired from MUESLI_UPLOAD_ALLOWED_CONTENT_TYPES).
	allowedContentTypes map[string]bool
}

// NewLocal builds a filesystem provider. signingKey is the HMAC key used to sign
// presigned URLs; it must be supplied by config (not generated per call) so
// signatures survive restarts and verify across replicas. It must be at least
// 16 bytes.
//
// publicURL is the base for upload URLs (client/host-facing); internalURL is the
// base for download URLs (plugin-facing). When internalURL is empty it falls
// back to publicURL. The HMAC signature is host-independent, so a URL signed on
// one base verifies fine when fetched via another.
func NewLocal(root, publicURL, internalURL string, signingKey []byte) (*Local, error) {
	if len(signingKey) < 16 {
		return nil, fmt.Errorf("storage signing key must be at least 16 bytes, got %d", len(signingKey))
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	secret := make([]byte, len(signingKey))
	copy(secret, signingKey)
	public := strings.TrimRight(publicURL, "/")
	internal := strings.TrimRight(internalURL, "/")
	if internal == "" {
		internal = public
	}
	return &Local{
		root:                root,
		publicURL:           public,
		internalURL:         internal,
		secret:              secret,
		maxUpload:           MaxUploadBytes,
		allowedContentTypes: contentTypeSet(DefaultAllowedContentTypes),
	}, nil
}

// SetMaxUploadBytes overrides the single-PUT body cap. Primarily for tests and
// tuning; production uses the MaxUploadBytes default set in NewLocal.
func (l *Local) SetMaxUploadBytes(n int64) {
	l.maxUpload = n
}

// SetAllowedContentTypes overrides the audio Content-Type allowlist enforced
// on the upload PUT path. An empty/nil slice restores DefaultAllowedContentTypes
// (it never disables the allowlist entirely). Mirrors SetMaxUploadBytes: wired
// from config (MUESLI_UPLOAD_ALLOWED_CONTENT_TYPES) in cmd/muesli/main.go.
func (l *Local) SetAllowedContentTypes(types []string) {
	if len(types) == 0 {
		l.allowedContentTypes = contentTypeSet(DefaultAllowedContentTypes)
		return
	}
	l.allowedContentTypes = contentTypeSet(types)
}

// contentTypeSet builds a lowercase lookup set from an allowlist slice.
func contentTypeSet(types []string) map[string]bool {
	set := make(map[string]bool, len(types))
	for _, t := range types {
		set[strings.ToLower(strings.TrimSpace(t))] = true
	}
	return set
}

func (l *Local) PresignUpload(key string, ttl time.Duration) (UploadGrant, error) {
	exp := time.Now().Add(ttl).Unix()
	sig := l.sign(key, exp)
	u := fmt.Sprintf("%s/_storage/%s?exp=%d&sig=%s",
		l.publicURL, key, exp, sig)
	return UploadGrant{
		URL:    u,
		Method: http.MethodPut,
		Key:    key,
		Expiry: time.Unix(exp, 0),
	}, nil
}

func (l *Local) Verify(key string) (bool, int64, error) {
	info, err := os.Stat(l.path(key))
	if os.IsNotExist(err) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	return true, info.Size(), nil
}

// Open returns a streaming reader for the stored object.
func (l *Local) Open(key string) (io.ReadCloser, error) {
	return os.Open(l.path(key))
}

// PresignDownload returns a signed GET URL the plugin can fetch directly.
func (l *Local) PresignDownload(key string, ttl time.Duration) (string, error) {
	exp := time.Now().Add(ttl).Unix()
	sig := l.sign(key, exp)
	return fmt.Sprintf("%s/_storage/%s?exp=%d&sig=%s&op=get", l.internalURL, key, exp, sig), nil
}

// Delete removes an object; a missing object is not an error.
func (l *Local) Delete(key string) error {
	err := os.Remove(l.path(key))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// UploadHandler serves the signed object endpoint (`/_storage/<key>`): PUT to
// store, GET to fetch (both signature-authenticated).
func (l *Local) UploadHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/_storage/")
		exp, _ := strconv.ParseInt(r.URL.Query().Get("exp"), 10, 64)
		sig := r.URL.Query().Get("sig")
		if exp < time.Now().Unix() {
			http.Error(w, "expired", http.StatusForbidden)
			return
		}
		if !hmac.Equal([]byte(sig), []byte(l.sign(key, exp))) {
			http.Error(w, "bad signature", http.StatusForbidden)
			return
		}

		switch r.Method {
		case http.MethodGet:
			f, err := os.Open(l.path(key))
			if os.IsNotExist(err) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if err != nil {
				http.Error(w, "internal", http.StatusInternalServerError)
				return
			}
			defer f.Close()
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = io.Copy(w, f)
		case http.MethodPut:
			// Defense-in-depth: even a holder of a valid signed URL may only
			// store an explicitly allowlisted audio type (see allowedContentTypes /
			// DefaultAllowedContentTypes). The desktop client always sends audio/webm.
			raw := r.Header.Get("Content-Type")
			mediaType, _, err := mime.ParseMediaType(raw)
			if err != nil || mediaType == "" {
				http.Error(w, "missing or invalid content type", http.StatusUnsupportedMediaType)
				return
			}
			if !l.allowedContentTypes[strings.ToLower(mediaType)] {
				http.Error(w, fmt.Sprintf("unsupported content type: %s", mediaType), http.StatusUnsupportedMediaType)
				return
			}
			if r.ContentLength > l.maxUpload {
				http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
				return
			}
			dst := l.path(key)
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				http.Error(w, "internal", http.StatusInternalServerError)
				return
			}
			f, err := os.Create(dst)
			if err != nil {
				http.Error(w, "internal", http.StatusInternalServerError)
				return
			}
			defer f.Close()
			limited := http.MaxBytesReader(w, r.Body, l.maxUpload)
			if _, err := io.Copy(f, limited); err != nil {
				// MaxBytesReader signals an over-limit body via *http.MaxBytesError.
				var mbe *http.MaxBytesError
				if errors.As(err, &mbe) {
					_ = os.Remove(dst) // don't leave a truncated partial object
					http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
					return
				}
				http.Error(w, "write failed", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

// MaxUploadBytes returns the configured per-upload body cap.
func (l *Local) MaxUploadBytes() int64 { return l.maxUpload }

func (l *Local) sign(key string, exp int64) string {
	mac := hmac.New(sha256.New, l.secret)
	fmt.Fprintf(mac, "%s\n%d", key, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

// path maps an object key to a filesystem path, guarding against traversal.
func (l *Local) path(key string) string {
	clean := filepath.Clean("/" + key) // force-rooted, strips ../
	return filepath.Join(l.root, filepath.FromSlash(clean))
}

var _ = url.QueryEscape // keys here are slash/uuid only; escaping handled by callers if needed
