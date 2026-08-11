package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// UUID / ID format validation
// ---------------------------------------------------------------------------

func TestUUIDValidation(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	// Setup user and obtain auth token.
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "uv@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "uv@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	badID := "not-a-uuid"

	tests := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		// Tags
		{"renameTag malformed id", http.MethodPut, "/api/tags/" + badID, map[string]string{"name": "x"}},
		{"deleteTag malformed id", http.MethodDelete, "/api/tags/" + badID, nil},
		// Folders
		{"deleteFolder malformed id", http.MethodDelete, "/api/folders/" + badID, nil},
		{"restoreFolder malformed id", http.MethodPost, "/api/folders/" + badID + "/restore", nil},
		{"purgeFolder malformed id", http.MethodDelete, "/api/folders/" + badID + "/permanent", nil},
		{"reorderFolder malformed id", http.MethodPut, "/api/folders/" + badID + "/reorder", map[string]any{"after_id": nil}},
		{"reorderNoteInFolder malformed folder id", http.MethodPut, "/api/folders/" + badID + "/notes/00000000-0000-0000-0000-000000000000/reorder", map[string]any{"after_id": nil}},
		{"reorderNoteInFolder malformed note id", http.MethodPut, "/api/folders/00000000-0000-0000-0000-000000000000/notes/" + badID + "/reorder", map[string]any{"after_id": nil}},
		{"updateFolder malformed id", http.MethodPut, "/api/folders/" + badID, map[string]string{"name": "x"}},
		// Smart lists
		{"deleteSmartList malformed id", http.MethodDelete, "/api/smart-lists/" + badID, nil},
		{"restoreSmartList malformed id", http.MethodPost, "/api/smart-lists/" + badID + "/restore", nil},
		{"purgeSmartList malformed id", http.MethodDelete, "/api/smart-lists/" + badID + "/permanent", nil},
		{"updateSmartList malformed id", http.MethodPut, "/api/smart-lists/" + badID, map[string]string{"name": "x"}},
		// Templates
		{"deleteTemplate malformed id", http.MethodDelete, "/api/templates/" + badID, nil},
		{"updateTemplate malformed id", http.MethodPut, "/api/templates/" + badID, map[string]string{"name": "x"}},
		// Admin plugins
		{"patchPlugin malformed id", http.MethodPatch, "/api/admin/plugins/" + badID, map[string]string{"name": "x"}},
		{"deletePlugin malformed id", http.MethodDelete, "/api/admin/plugins/" + badID, nil},
		// Admin jobs
		{"retryJob malformed id", http.MethodPost, "/api/admin/jobs/" + badID + "/retry", nil},
		// Notes
		{"duplicateNote malformed id", http.MethodPost, "/api/notes/" + badID + "/duplicate", nil},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := doJSON(t, srv, tc.method, tc.path, tc.body, hdr)
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s %s: got %d, want 404; body: %s", tc.method, tc.path, rec.Code, rec.Body)
			}
		})
	}
}

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

// ---------------------------------------------------------------------------
// Required-field validation — handleCreateNote
// ---------------------------------------------------------------------------

func TestCreateNoteTitleRequired(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "cn@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "cn@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	tests := []struct {
		name string
		body any
		want int
	}{
		{"missing title field", map[string]string{}, http.StatusBadRequest},
		{"empty title string", map[string]string{"title": ""}, http.StatusBadRequest},
		{"whitespace-only title", map[string]string{"title": "   "}, http.StatusBadRequest},
		{"valid title", map[string]string{"title": "My note"}, http.StatusCreated},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, srv, http.MethodPost, "/api/notes", tc.body, hdr)
			if rec.Code != tc.want {
				t.Errorf("got %d, want %d; body: %s", rec.Code, tc.want, rec.Body)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Required-field validation — handleLogin
// ---------------------------------------------------------------------------

func TestLoginRequiredFields(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "lr@example.com", "password": "password123"}, nil)

	tests := []struct {
		name string
		body map[string]string
		want int
	}{
		{"missing email", map[string]string{"password": "password123"}, http.StatusBadRequest},
		{"empty email", map[string]string{"email": "", "password": "password123"}, http.StatusBadRequest},
		{"whitespace email", map[string]string{"email": "   ", "password": "password123"}, http.StatusBadRequest},
		{"missing password", map[string]string{"email": "lr@example.com"}, http.StatusBadRequest},
		{"empty password", map[string]string{"email": "lr@example.com", "password": ""}, http.StatusBadRequest},
		{"whitespace password", map[string]string{"email": "lr@example.com", "password": "   "}, http.StatusBadRequest},
		{"valid credentials", map[string]string{"email": "lr@example.com", "password": "password123"}, http.StatusOK},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, srv, http.MethodPost, "/api/login", tc.body, nil)
			if rec.Code != tc.want {
				t.Errorf("got %d, want %d; body: %s", rec.Code, tc.want, rec.Body)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Status enum validation — handleListNotes
// ---------------------------------------------------------------------------

func TestListNotesStatusValidation(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "sv@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "sv@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	tests := []struct {
		name   string
		status string
		want   int
	}{
		// Valid values — all seven statuses plus empty string.
		{"no status param", "", http.StatusOK},
		{"status=draft", "draft", http.StatusOK},
		{"status=recording", "recording", http.StatusOK},
		{"status=uploaded", "uploaded", http.StatusOK},
		{"status=transcribing", "transcribing", http.StatusOK},
		{"status=summarizing", "summarizing", http.StatusOK},
		{"status=ready", "ready", http.StatusOK},
		{"status=failed", "failed", http.StatusOK},
		// Invalid values.
		{"status=unknown", "unknown", http.StatusBadRequest},
		{"status=READY uppercase", "READY", http.StatusBadRequest},
		{"status=pending (job status not note status)", "pending", http.StatusBadRequest},
		{"status=deleted", "deleted", http.StatusBadRequest},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			path := "/api/notes"
			if tc.status != "" {
				path += "?status=" + tc.status
			}
			rec := doJSON(t, srv, http.MethodGet, path, nil, hdr)
			if rec.Code != tc.want {
				t.Errorf("status=%q got %d, want %d; body: %s", tc.status, rec.Code, tc.want, rec.Body)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Body-size middleware
// ---------------------------------------------------------------------------

func TestBodySizeLimit(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "bs@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "bs@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	// Build a body that exceeds 1 MiB (1<<20 bytes = 1 048 576 bytes).
	oversized := `{"title":"` + strings.Repeat("x", 1<<20+1) + `"}`

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		hdrs   map[string]string
	}{
		// Authenticated route: POST /api/notes with oversized body.
		{"oversized createNote (authenticated)", http.MethodPost, "/api/notes", oversized, hdr},
		// Public route: POST /api/login with oversized body.
		{"oversized login (public)", http.MethodPost, "/api/login", oversized, nil},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := doRaw(t, srv, tc.method, tc.path, tc.body, tc.hdrs)
			// MaxBytesReader causes json.Decode to fail; handler returns 400.
			// Either 400 or 413 satisfies the contract.
			if rec.Code != http.StatusBadRequest && rec.Code != http.StatusRequestEntityTooLarge {
				t.Errorf("%s %s oversized body: got %d, want 400 or 413; body: %s",
					tc.method, tc.path, rec.Code, rec.Body)
			}
		})
	}

	t.Run("oversized createNote with unknown Content-Length", func(t *testing.T) {
		t.Parallel()

		const oversizedBodySize = 5 * 1024 * 1024 // 5 MiB, much larger than the 1 MiB cap
		body := `{"title":"` + strings.Repeat("x", oversizedBodySize) + `"}`
		cr := &countingReader{r: strings.NewReader(body)}

		req := httptest.NewRequest(http.MethodPost, "/api/notes", cr)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+login.Token)
		req.ContentLength = -1

		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		// MaxBytesReader causes json.Decode to fail; handler returns 400 or 413.
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("oversized streaming body: got %d, want 400 or 413; body: %s", rec.Code, rec.Body)
		}

		// Prove the handler stopped reading shortly after crossing the cap
		// instead of consuming the full oversized body first.
		const safetyMargin = 4096
		if got, max := cr.BytesRead(), int64(1<<20)+safetyMargin; got > max {
			t.Fatalf("handler read %d bytes from the source, want <= %d; this looks like it buffered the body instead of streaming it", got, max)
		}
		if got := cr.BytesRead(); got >= int64(len(body)) {
			t.Fatalf("handler read the entire oversized body (%d bytes read); cap enforcement is not streaming", got)
		}
	})
}
