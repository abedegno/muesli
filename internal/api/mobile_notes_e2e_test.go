// TestMobileNotesHostedTwoUserIsolation is the accepted spec's "one hosted
// end-to-end test [that] uses the real server, two users, and multiple pages
// to prove browse/read and isolation" for issue #767. It drives the real
// Server.Handler() (real auth middleware, real Store, real Postgres via
// TEST_DATABASE_URL) exactly as a mobile client would: sign in, page through
// notes across several requests, open detail, and confirm neither user can
// ever observe the other's data through either mobile route.
//
// DB-backed and CI-only: testutil.NewPool (via newTestServer) skips when
// TEST_DATABASE_URL is unset, so it must not be run on the local runner.
package api_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

func TestMobileNotesHostedTwoUserIsolation(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)

	aliceHdr, _ := authHeaderForUser(t, st, "mobile-e2e-alice@example.com")
	bobHdr, _ := authHeaderForUser(t, st, "mobile-e2e-bob@example.com")

	createNotes := func(hdr map[string]string, count int, titlePrefix string) []string {
		ids := make([]string, count)
		for i := 0; i < count; i++ {
			rec := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]string{"title": titlePrefix}, hdr)
			if rec.Code != http.StatusCreated {
				t.Fatalf("create note: status %d body %s", rec.Code, rec.Body)
			}
			var n struct{ ID string }
			_ = json.Unmarshal(rec.Body.Bytes(), &n)
			ids[i] = n.ID
			_ = doJSON(t, srv, http.MethodPut, "/api/notes/"+n.ID+"/body",
				map[string]string{"content": "Body for " + titlePrefix}, hdr)
		}
		return ids
	}

	// Alice has enough notes to span several pages at a small page size;
	// Bob has fewer, on the same server, at the same time.
	aliceIDs := createNotes(aliceHdr, 7, "Alice's note")
	bobIDs := createNotes(bobHdr, 3, "Bob's note")

	pageThrough := func(hdr map[string]string, limit int) []mobileNoteItemDTO {
		var all []mobileNoteItemDTO
		cursor := ""
		for i := 0; i < 20; i++ {
			url := "/api/mobile/v1/notes?limit=" + strconv.Itoa(limit)
			if cursor != "" {
				url += "&cursor=" + cursor
			}
			rec := doJSON(t, srv, http.MethodGet, url, nil, hdr)
			if rec.Code != http.StatusOK {
				t.Fatalf("list page %d: status %d body %s", i, rec.Code, rec.Body)
			}
			var page mobileNotesListDTO
			if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
				t.Fatalf("decode page %d: %v", i, err)
			}
			all = append(all, page.Items...)
			if page.NextCursor == "" {
				return all
			}
			cursor = page.NextCursor
		}
		t.Fatalf("pagination did not terminate")
		return nil
	}

	aliceList := pageThrough(aliceHdr, 2) // forces >1 page for Alice's 7 notes
	bobList := pageThrough(bobHdr, 2)

	assertExactIDSet := func(t *testing.T, got []mobileNoteItemDTO, want []string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("got %d items, want %d: %+v", len(got), len(want), got)
		}
		wantSet := map[string]bool{}
		for _, id := range want {
			wantSet[id] = true
		}
		seen := map[string]bool{}
		for _, item := range got {
			if !wantSet[item.ID] {
				t.Fatalf("unexpected note %s leaked into list", item.ID)
			}
			if seen[item.ID] {
				t.Fatalf("duplicate note %s across pages", item.ID)
			}
			seen[item.ID] = true
		}
	}
	assertExactIDSet(t, aliceList, aliceIDs)
	assertExactIDSet(t, bobList, bobIDs)

	// Detail: each user can read every one of their own notes...
	for _, id := range aliceIDs {
		rec := doJSON(t, srv, http.MethodGet, "/api/mobile/v1/notes/"+id, nil, aliceHdr)
		if rec.Code != http.StatusOK {
			t.Fatalf("alice detail %s: status %d body %s", id, rec.Code, rec.Body)
		}
	}
	for _, id := range bobIDs {
		rec := doJSON(t, srv, http.MethodGet, "/api/mobile/v1/notes/"+id, nil, bobHdr)
		if rec.Code != http.StatusOK {
			t.Fatalf("bob detail %s: status %d body %s", id, rec.Code, rec.Body)
		}
	}

	// ...and can never read the other's, in either direction, indistinguishable from absent.
	for _, id := range bobIDs {
		rec := doJSON(t, srv, http.MethodGet, "/api/mobile/v1/notes/"+id, nil, aliceHdr)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("alice reading bob's note %s: status %d (isolation violated)", id, rec.Code)
		}
	}
	for _, id := range aliceIDs {
		rec := doJSON(t, srv, http.MethodGet, "/api/mobile/v1/notes/"+id, nil, bobHdr)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("bob reading alice's note %s: status %d (isolation violated)", id, rec.Code)
		}
	}
}
