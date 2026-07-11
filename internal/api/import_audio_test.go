package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func buildImportBody(t *testing.T, title, filename, contentType, payload string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if title != "" {
		if err := w.WriteField("title", title); err != nil {
			t.Fatalf("write title field: %v", err)
		}
	}
	hdr := make(textproto.MIMEHeader)
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	hdr.Set("Content-Type", contentType)
	part, err := w.CreatePart(hdr)
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err := part.Write([]byte(payload)); err != nil {
		t.Fatalf("write file payload: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart body: %v", err)
	}
	return &buf, w.FormDataContentType()
}

func TestImportAudioFlow(t *testing.T) {
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

	body, contentType := buildImportBody(t, "Imported meeting", "meeting.webm", "audio/webm", "audio-bytes")
	req := httptest.NewRequest(http.MethodPost, "/api/notes/import", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+login.Token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import status %d body %s", rec.Code, rec.Body)
	}
	var created struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" || created.Title != "Imported meeting" || created.Status != "uploaded" {
		t.Fatalf("unexpected created note %+v body %s", created, rec.Body)
	}

	dupBody, dupContentType := buildImportBody(t, "Imported meeting", "meeting.webm", "audio/webm", "audio-bytes")
	dupReq := httptest.NewRequest(http.MethodPost, "/api/notes/import", dupBody)
	dupReq.Header.Set("Content-Type", dupContentType)
	dupReq.Header.Set("Authorization", "Bearer "+login.Token)
	dupRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(dupRec, dupReq)
	if dupRec.Code != http.StatusConflict {
		t.Fatalf("duplicate import status %d body %s", dupRec.Code, dupRec.Body)
	}
	var conflict struct {
		Error string `json:"error"`
		Match struct {
			NoteID    string `json:"note_id"`
			Title     string `json:"title"`
			Status    string `json:"status"`
			CreatedAt string `json:"created_at"`
		} `json:"match"`
	}
	_ = json.Unmarshal(dupRec.Body.Bytes(), &conflict)
	if conflict.Match.NoteID != created.ID || conflict.Error != "duplicate audio" {
		t.Fatalf("unexpected conflict body %+v", conflict)
	}
}
