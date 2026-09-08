package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

type userListItemView struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

type userListView struct {
	Items      []userListItemView `json:"items"`
	NextCursor string             `json:"next_cursor"`
}

func TestListUsersAnyAuthenticatedUser(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	srv := api.NewServer(api.Deps{Store: st})
	adminHdr := setupLoginHdr(t, srv, "list-admin@example.com")
	_ = createSecondUser(t, st, "list-member@example.com", "password123")
	memberHdr := loginHdr(t, srv, "list-member@example.com", "password123")

	for _, hdr := range []map[string]string{adminHdr, memberHdr} {
		rec := doJSON(t, srv, http.MethodGet, "/api/users", nil, hdr)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
		}
		var got userListView
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(got.Items) != 2 {
			t.Fatalf("expected 2 users, got %d: %+v", len(got.Items), got.Items)
		}
		for _, it := range got.Items {
			if it.ID == "" || it.Email == "" || it.Role == "" {
				t.Fatalf("incomplete item: %+v", it)
			}
		}
	}
}

func TestListUsersFieldsExactlyIDEmailRole(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	srv := api.NewServer(api.Deps{Store: st})
	hdr := setupLoginHdr(t, srv, "fields-admin@example.com")

	rec := doJSON(t, srv, http.MethodGet, "/api/users", nil, hdr)
	var raw struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(raw.Items) != 1 {
		t.Fatalf("expected 1 user, got %d", len(raw.Items))
	}
	item := raw.Items[0]
	if len(item) != 3 {
		t.Fatalf("expected exactly id/email/role, got %+v", item)
	}
	for _, field := range []string{"id", "email", "role"} {
		if _, ok := item[field]; !ok {
			t.Fatalf("missing field %q in %+v", field, item)
		}
	}
}

func TestListUsersPaginationDefaultAndMax(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	srv := api.NewServer(api.Deps{Store: st})
	hdr := setupLoginHdr(t, srv, "paginate-admin@example.com")
	for i := 0; i < 5; i++ {
		_ = createSecondUser(t, st, userEmail(i), "password123")
	}

	// limit=2 -> next_cursor set, exactly 2 items.
	rec := doJSON(t, srv, http.MethodGet, "/api/users?limit=2", nil, hdr)
	var page1 userListView
	_ = json.Unmarshal(rec.Body.Bytes(), &page1)
	if len(page1.Items) != 2 || page1.NextCursor == "" {
		t.Fatalf("page1 = %+v, want 2 items with a next_cursor", page1)
	}

	// Follow the cursor.
	rec = doJSON(t, srv, http.MethodGet, "/api/users?limit=2&cursor="+page1.NextCursor, nil, hdr)
	var page2 userListView
	_ = json.Unmarshal(rec.Body.Bytes(), &page2)
	if len(page2.Items) != 2 {
		t.Fatalf("page2 = %+v, want 2 items", page2)
	}
	for _, a := range page1.Items {
		for _, b := range page2.Items {
			if a.ID == b.ID {
				t.Fatalf("page2 repeated id %s from page1", a.ID)
			}
		}
	}

	// limit=0 rejected.
	rec = doJSON(t, srv, http.MethodGet, "/api/users?limit=0", nil, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("limit=0: status = %d, want 400", rec.Code)
	}
	// limit=101 rejected (max 100).
	rec = doJSON(t, srv, http.MethodGet, "/api/users?limit=101", nil, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("limit=101: status = %d, want 400", rec.Code)
	}
	// malformed cursor rejected.
	rec = doJSON(t, srv, http.MethodGet, "/api/users?cursor=not-a-cursor!!!", nil, hdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed cursor: status = %d, want 400", rec.Code)
	}
}

func userEmail(i int) string {
	return "paginate" + string(rune('a'+i)) + "@example.com"
}

func TestAdminListUsersMirrorsListUsers(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	srv := api.NewServer(api.Deps{Store: st})
	adminHdr := setupLoginHdr(t, srv, "admin-mirror@example.com")
	_ = createSecondUser(t, st, "member-mirror@example.com", "password123")
	memberHdr := loginHdr(t, srv, "member-mirror@example.com", "password123")

	rec := doJSON(t, srv, http.MethodGet, "/api/admin/users", nil, memberHdr)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member: status = %d, want 403", rec.Code)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/admin/users", nil, adminHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin: status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	var got userListView
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Items) != 2 {
		t.Fatalf("expected 2 users, got %d", len(got.Items))
	}
}

func TestPatchUserRoleAdminOnlyAndValidated(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	srv := api.NewServer(api.Deps{Store: st})
	adminHdr := setupLoginHdr(t, srv, "patch-admin@example.com")
	targetID := createSecondUser(t, st, "patch-target@example.com", "password123")
	memberHdr := loginHdr(t, srv, "patch-target@example.com", "password123")

	// Member cannot patch anyone's role.
	rec := doJSON(t, srv, http.MethodPatch, "/api/admin/users/"+targetID, map[string]string{"role": "admin"}, memberHdr)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member patch: status = %d, want 403", rec.Code)
	}

	// Invalid role.
	rec = doJSON(t, srv, http.MethodPatch, "/api/admin/users/"+targetID, map[string]string{"role": "root"}, adminHdr)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid role: status = %d, want 400", rec.Code)
	}

	// Malformed id.
	rec = doJSON(t, srv, http.MethodPatch, "/api/admin/users/not-a-uuid", map[string]string{"role": "admin"}, adminHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("malformed id: status = %d, want 404", rec.Code)
	}

	// Absent user.
	rec = doJSON(t, srv, http.MethodPatch, "/api/admin/users/00000000-0000-0000-0000-000000000000",
		map[string]string{"role": "admin"}, adminHdr)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("absent user: status = %d, want 404", rec.Code)
	}

	// Valid promotion.
	rec = doJSON(t, srv, http.MethodPatch, "/api/admin/users/"+targetID, map[string]string{"role": "admin"}, adminHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("promote: status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	var got userListItemView
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Role != "admin" {
		t.Fatalf("role = %q, want admin", got.Role)
	}

	// Idempotent reapply.
	rec = doJSON(t, srv, http.MethodPatch, "/api/admin/users/"+targetID, map[string]string{"role": "admin"}, adminHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("idempotent reapply: status = %d, want 200", rec.Code)
	}

	// Now two admins exist; demoting the original admin succeeds...
	adminID, err := st.GetUserByEmail(context.Background(), "patch-admin@example.com")
	if err != nil {
		t.Fatalf("lookup admin: %v", err)
	}
	rec = doJSON(t, srv, http.MethodPatch, "/api/admin/users/"+adminID.ID, map[string]string{"role": "member"}, adminHdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("demote original admin (two admins): status = %d, want 200; body %s", rec.Code, rec.Body)
	}

	// ...but demoting the sole remaining admin (target, now the only admin)
	// via the ORIGINAL admin's now-member session is forbidden by the gate
	// itself (403), so use the promoted target's own session to prove 409.
	targetHdr := loginHdr(t, srv, "patch-target@example.com", "password123")
	rec = doJSON(t, srv, http.MethodPatch, "/api/admin/users/"+targetID, map[string]string{"role": "member"}, targetHdr)
	if rec.Code != http.StatusConflict {
		t.Fatalf("demote sole admin: status = %d, want 409; body %s", rec.Code, rec.Body)
	}
}
