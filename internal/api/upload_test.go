package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func TestMalformedNoteIDReturns404(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	_ = doJSON(t, srv, http.MethodPost, "/api/setup", map[string]string{"email": "h@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login", map[string]string{"email": "h@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}
	got := doJSON(t, srv, http.MethodGet, "/api/notes/not-a-uuid", nil, hdr)
	if got.Code != http.StatusNotFound {
		t.Fatalf("malformed id: want 404 got %d", got.Code)
	}
}

func TestAudioUploadFlow(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	prov, err := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	srv := api.NewServer(api.Deps{Store: st, Storage: prov})

	_ = doJSON(t, srv, http.MethodPost, "/api/setup",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	rec := doJSON(t, srv, http.MethodPost, "/api/login",
		map[string]string{"email": "o@example.com", "password": "password123"}, nil)
	var login struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &login)
	hdr := map[string]string{"Authorization": "Bearer " + login.Token}

	rec = doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "M"}, hdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)

	// Request a presigned upload URL.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/audio-upload-url", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("presign status %d body %s", rec.Code, rec.Body)
	}
	var grant struct{ URL, Key, Method string }
	_ = json.Unmarshal(rec.Body.Bytes(), &grant)
	if grant.Method != http.MethodPut || grant.Key == "" {
		t.Fatalf("bad grant %+v", grant)
	}

	// Upload bytes via the provider's handler (mounted at /_storage/).
	put := doRaw(t, srv, http.MethodPut, strings.TrimPrefix(grant.URL, "http://example.test"), "audio-bytes",
		map[string]string{"Content-Type": "audio/webm"})
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status %d", put.Code)
	}

	// Notify the server; it verifies the object and advances note status.
	rec = doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/audio-uploaded",
		map[string]string{"key": grant.Key}, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("uploaded notify status %d body %s", rec.Code, rec.Body)
	}

	// Note status is now "uploaded".
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID, nil, hdr)
	var got struct{ Status string }
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Status != "uploaded" {
		t.Fatalf("status %q, want uploaded", got.Status)
	}
}

func TestAudioURLFlow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := store.New(testutil.NewPool(t))
	prov, err := storage.NewLocal(t.TempDir(), "http://example.test", "http://example.test", []byte("test-signing-key-0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	srv := api.NewServer(api.Deps{Store: st, Storage: prov})

	owner, err := st.CreateUser(ctx, "owner@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	raw, hash, err := auth.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateToken(ctx, owner.ID, "session", hash, "session"); err != nil {
		t.Fatal(err)
	}
	ownerHdr := map[string]string{"Authorization": "Bearer " + raw}

	other, err := st.CreateUser(ctx, "other@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	otherRaw, otherHash, err := auth.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateToken(ctx, other.ID, "session", otherHash, "session"); err != nil {
		t.Fatal(err)
	}
	otherHdr := map[string]string{"Authorization": "Bearer " + otherRaw}

	rec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "M"}, ownerHdr)
	var note struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &note)

	// Missing audio returns 404.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/audio-url", nil, ownerHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing audio status %d body %s", rec.Code, rec.Body)
	}

	// Once audio is present, the owner gets a signed GET grant.
	audioKey := "notes/" + note.ID + "/audio/test.webm"
	if err := st.SetNoteAudio(ctx, owner.ID, note.ID, audioKey); err != nil {
		t.Fatal(err)
	}
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/audio-url", nil, ownerHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("audio-url status %d body %s", rec.Code, rec.Body)
	}
	var grant struct {
		URL       string `json:"url"`
		ExpiresAt string `json:"expires_at"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &grant)
	if grant.URL == "" {
		t.Fatalf("empty grant URL body %s", rec.Body)
	}
	if _, err := time.Parse(time.RFC3339, grant.ExpiresAt); err != nil {
		t.Fatalf("bad expires_at %q: %v", grant.ExpiresAt, err)
	}

	// Non-owners must still get a 404.
	rec = doJSON(t, srv, http.MethodGet, "/api/notes/"+note.ID+"/audio-url", nil, otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-owner audio-url status %d body %s", rec.Code, rec.Body)
	}
}
