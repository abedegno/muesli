package api_test

import (
	"context"
	"encoding/json"
	"github.com/abedegno/muesli/internal/auth"
	"net/http"
	"testing"
)

func TestMemberCannotAdministerServer(t *testing.T) {
	t.Parallel()
	srv, st, admin := adminServer(t)
	member, err := st.CreateUser(context.Background(), "member@example.com", "unused")
	if err != nil {
		t.Fatal(err)
	}
	raw, hash, err := auth.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if err = st.CreateToken(context.Background(), member.ID, "member", hash, "app"); err != nil {
		t.Fatal(err)
	}
	hdr := map[string]string{"Authorization": "Bearer " + raw}
	for _, path := range []string{"/api/admin/plugins", "/api/admin/jobs", "/api/admin/backups", "/api/admin/config", "/api/admin/embeddings", "/api/admin/health"} {
		rec := doJSON(t, srv, http.MethodGet, path, nil, hdr)
		if rec.Code != http.StatusForbidden {
			t.Errorf("member GET %s: got %d want 403", path, rec.Code)
		}
	}
	if rec := doJSON(t, srv, http.MethodPost, "/api/admin/plugins", map[string]string{"kind": "agent", "name": "unauthorized", "endpoint_url": "http://evil.test"}, hdr); rec.Code != http.StatusForbidden {
		t.Errorf("member mutation: %d", rec.Code)
	}
	if rec := doJSON(t, srv, http.MethodGet, "/api/admin/plugins", nil, admin); rec.Code != http.StatusOK {
		t.Errorf("admin denied: %d", rec.Code)
	}
	if rec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": "Member's note"}, hdr); rec.Code != http.StatusCreated {
		t.Errorf("normal member action denied: %d", rec.Code)
	}
	rec := doJSON(t, srv, http.MethodGet, "/api/capabilities", nil, hdr)
	var caps map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &caps)
	if caps["is_admin"] != false {
		t.Errorf("member capability: %s", rec.Body)
	}
}

func TestAdminRolesCannotRemoveLastAdministrator(t *testing.T) {
	t.Parallel()
	srv, st, hdr := adminServer(t)
	if rec := doJSON(t, srv, http.MethodPut, "/api/admin/users/role", map[string]string{"email": "o@example.com", "role": "member"}, hdr); rec.Code != 409 {
		t.Fatalf("last administrator: %d %s", rec.Code, rec.Body)
	}
	_, err := st.CreateUser(context.Background(), "next@example.com", "unused")
	if err != nil {
		t.Fatal(err)
	}
	if rec := doJSON(t, srv, http.MethodPut, "/api/admin/users/role", map[string]string{"email": "next@example.com", "role": "admin"}, hdr); rec.Code != 200 {
		t.Fatalf("promote: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, http.MethodPut, "/api/admin/users/role", map[string]string{"email": "o@example.com", "role": "member"}, hdr); rec.Code != 200 {
		t.Fatalf("demote: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(t, srv, http.MethodGet, "/api/admin/plugins", nil, hdr); rec.Code != 403 {
		t.Fatalf("stale administrator rights: %d", rec.Code)
	}
	if rec := doJSON(t, srv, http.MethodGet, "/api/transcription/services", nil, hdr); rec.Code != 200 {
		t.Fatalf("member cannot inspect available transcription: %d", rec.Code)
	}
}
