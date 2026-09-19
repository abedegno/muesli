package api_test

// team_sharing_authorization_test.go: DB-backed and CI-only (see
// testutil.NewPool). Proves the shared-folder read widening from issue #12
// is exactly as narrow as the design doc specifies: PUT
// /api/folders/{id}/visibility, the filing routes, and exactly the three
// shared-readable note routes (folder-filtered GET /api/notes, GET
// /api/notes/{id}, GET /api/notes/{id}/full) behave per the design doc; every
// other authenticated /api/notes/{id}/... route stays owner-only and 404s for
// a non-owner even when the note is otherwise shared-readable. The
// completeness check mechanically enumerates every registered
// /api/notes/{id}/... route so a future addition must be explicitly
// classified into one of: shared-readable, filing, or the denial table.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/api"
	"github.com/abedegno/muesli/internal/model"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// sharedNoteFixture sets up an owner with a note filed into a folder they
// share, and a second, unrelated user who can read that note only via
// CanReadNote (owns nothing about it).
type sharedNoteFixture struct {
	srv                *api.Server
	ownerHdr, otherHdr map[string]string
	noteID, folderID   string
}

func newSharedNoteFixture(t *testing.T) sharedNoteFixture {
	t.Helper()
	srv, st := newTestServer(t)
	ownerHdr, _ := authHeaderForUser(t, st, "team-share-owner@example.com")
	otherHdr, _ := authHeaderForUser(t, st, "team-share-other@example.com")

	c := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]any{"title": "Standup"}, ownerHdr)
	if c.Code != http.StatusCreated {
		t.Fatalf("create note: %d %s", c.Code, c.Body.String())
	}
	var note struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(c.Body.Bytes(), &note)

	f := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Team"}, ownerHdr)
	if f.Code != http.StatusCreated {
		t.Fatalf("create folder: %d %s", f.Code, f.Body.String())
	}
	var folder struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(f.Body.Bytes(), &folder)

	v := doJSON(t, srv, http.MethodPut, "/api/folders/"+folder.ID+"/visibility", map[string]any{"visibility": "shared"}, ownerHdr)
	if v.Code != http.StatusOK {
		t.Fatalf("share folder: %d %s", v.Code, v.Body.String())
	}

	af := doJSON(t, srv, http.MethodPost, "/api/notes/"+note.ID+"/folders", map[string]any{"folder_id": folder.ID}, ownerHdr)
	if af.Code != http.StatusOK {
		t.Fatalf("file note: %d %s", af.Code, af.Body.String())
	}

	return sharedNoteFixture{srv: srv, ownerHdr: ownerHdr, otherHdr: otherHdr, noteID: note.ID, folderID: folder.ID}
}

// --- PUT /api/folders/{id}/visibility -------------------------------------

func TestFolderVisibilityEndpoint(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ownerHdr, _ := authHeaderForUser(t, st, "vis-owner@example.com")
	otherHdr, _ := authHeaderForUser(t, st, "vis-other@example.com")

	f := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "F"}, ownerHdr)
	var folder struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(f.Body.Bytes(), &folder)

	// Valid exact bodies succeed.
	shared := doJSON(t, srv, http.MethodPut, "/api/folders/"+folder.ID+"/visibility", map[string]any{"visibility": "shared"}, ownerHdr)
	if shared.Code != http.StatusOK {
		t.Fatalf("set shared: %d %s", shared.Code, shared.Body.String())
	}
	var got model.Folder
	if err := json.Unmarshal(shared.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Visibility != model.FolderShared || !got.IsOwner || got.OwnerID == "" {
		t.Fatalf("shared folder response: %+v", got)
	}
	private := doJSON(t, srv, http.MethodPut, "/api/folders/"+folder.ID+"/visibility", map[string]any{"visibility": "private"}, ownerHdr)
	if private.Code != http.StatusOK {
		t.Fatalf("set private: %d %s", private.Code, private.Body.String())
	}

	// Missing/invalid values -> 400.
	if rec := doJSON(t, srv, http.MethodPut, "/api/folders/"+folder.ID+"/visibility", map[string]any{"visibility": "public"}, ownerHdr); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid value: want 400, got %d", rec.Code)
	}
	if rec := doJSON(t, srv, http.MethodPut, "/api/folders/"+folder.ID+"/visibility", map[string]any{}, ownerHdr); rec.Code != http.StatusBadRequest {
		t.Errorf("missing value: want 400, got %d", rec.Code)
	}

	// Unknown fields -> 400.
	if rec := doJSON(t, srv, http.MethodPut, "/api/folders/"+folder.ID+"/visibility", map[string]any{"visibility": "shared", "extra": true}, ownerHdr); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field: want 400, got %d", rec.Code)
	}

	// Malformed / trailing JSON -> 400.
	if rec := doRaw(t, srv, http.MethodPut, "/api/folders/"+folder.ID+"/visibility", `{"visibility":`, ownerHdr); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed json: want 400, got %d", rec.Code)
	}
	if rec := doRaw(t, srv, http.MethodPut, "/api/folders/"+folder.ID+"/visibility", `{"visibility":"shared"}{}`, ownerHdr); rec.Code != http.StatusBadRequest {
		t.Errorf("trailing json: want 400, got %d", rec.Code)
	}

	// Invisible id (random uuid) -> 404.
	if rec := doJSON(t, srv, http.MethodPut, "/api/folders/"+uuid.NewString()+"/visibility", map[string]any{"visibility": "shared"}, ownerHdr); rec.Code != http.StatusNotFound {
		t.Errorf("random id: want 404, got %d", rec.Code)
	}

	// Private foreign -> 404 (invisible).
	if rec := doJSON(t, srv, http.MethodPut, "/api/folders/"+folder.ID+"/visibility", map[string]any{"visibility": "shared"}, otherHdr); rec.Code != http.StatusNotFound {
		t.Errorf("private foreign: want 404, got %d", rec.Code)
	}

	// Shared foreign -> 403 (visible, not theirs).
	_ = doJSON(t, srv, http.MethodPut, "/api/folders/"+folder.ID+"/visibility", map[string]any{"visibility": "shared"}, ownerHdr)
	if rec := doJSON(t, srv, http.MethodPut, "/api/folders/"+folder.ID+"/visibility", map[string]any{"visibility": "private"}, otherHdr); rec.Code != http.StatusForbidden {
		t.Errorf("shared foreign: want 403, got %d", rec.Code)
	}
}

// --- filing matrix over HTTP ----------------------------------------------

func TestNoteFolderFilingHTTP(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ownerHdr, _ := authHeaderForUser(t, st, "file-owner@example.com")
	otherHdr, _ := authHeaderForUser(t, st, "file-other@example.com")

	sharedFolder := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Shared"}, ownerHdr)
	var sf struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(sharedFolder.Body.Bytes(), &sf)
	_ = doJSON(t, srv, http.MethodPut, "/api/folders/"+sf.ID+"/visibility", map[string]any{"visibility": "shared"}, ownerHdr)

	privateFolder := doJSON(t, srv, http.MethodPost, "/api/folders", map[string]any{"name": "Private"}, ownerHdr)
	var pf struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(privateFolder.Body.Bytes(), &pf)

	theirNote := doJSON(t, srv, http.MethodPost, "/api/notes", map[string]any{"title": "Theirs"}, otherHdr)
	var tn struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(theirNote.Body.Bytes(), &tn)

	// Own note into someone else's shared folder: 200.
	if rec := doJSON(t, srv, http.MethodPost, "/api/notes/"+tn.ID+"/folders", map[string]any{"folder_id": sf.ID}, otherHdr); rec.Code != http.StatusOK {
		t.Errorf("file own note into shared folder: want 200, got %d %s", rec.Code, rec.Body.String())
	}

	// Own note into someone else's private folder: invisible -> 404.
	if rec := doJSON(t, srv, http.MethodPost, "/api/notes/"+tn.ID+"/folders", map[string]any{"folder_id": pf.ID}, otherHdr); rec.Code != http.StatusNotFound {
		t.Errorf("file into private foreign folder: want 404, got %d", rec.Code)
	}

	// Someone else's note (now readable via the shared folder) into my own
	// folder: visible but forbidden to file.
	if rec := doJSON(t, srv, http.MethodPost, "/api/notes/"+tn.ID+"/folders", map[string]any{"folder_id": pf.ID}, ownerHdr); rec.Code != http.StatusForbidden {
		t.Errorf("file foreign visible note: want 403, got %d", rec.Code)
	}

	// Folder owner may remove a teammate's contribution from their own folder.
	if rec := doJSON(t, srv, http.MethodDelete, "/api/notes/"+tn.ID+"/folders/"+sf.ID, nil, ownerHdr); rec.Code != http.StatusOK {
		t.Errorf("folder owner removes teammate note: want 200, got %d %s", rec.Code, rec.Body.String())
	}
}

// --- the three shared-readable routes --------------------------------------

func TestSharedReadableRoutes(t *testing.T) {
	t.Parallel()
	fx := newSharedNoteFixture(t)

	// PUT the body as owner so /full has non-trivial content to check.
	if rec := doJSON(t, fx.srv, http.MethodPut, "/api/notes/"+fx.noteID+"/body", map[string]any{"content": "Meeting notes"}, fx.ownerHdr); rec.Code != http.StatusOK {
		t.Fatalf("set body: %d %s", rec.Code, rec.Body.String())
	}

	// Folder-filtered list.
	list := doJSON(t, fx.srv, http.MethodGet, "/api/notes?folder_id="+fx.folderID, nil, fx.otherHdr)
	if list.Code != http.StatusOK {
		t.Fatalf("folder-filtered list: %d %s", list.Code, list.Body.String())
	}
	var notes []model.Note
	if err := json.Unmarshal(list.Body.Bytes(), &notes); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(notes) != 1 || notes[0].ID != fx.noteID {
		t.Fatalf("folder-filtered list = %+v, want [%s]", notes, fx.noteID)
	}
	if notes[0].IsOwner == nil || *notes[0].IsOwner {
		t.Errorf("shared reader list IsOwner = %v, want false", notes[0].IsOwner)
	}

	// Detail.
	detail := doJSON(t, fx.srv, http.MethodGet, "/api/notes/"+fx.noteID, nil, fx.otherHdr)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail: %d %s", detail.Code, detail.Body.String())
	}
	var n model.Note
	_ = json.Unmarshal(detail.Body.Bytes(), &n)
	if n.IsOwner == nil || *n.IsOwner {
		t.Errorf("shared reader detail IsOwner = %v, want false", n.IsOwner)
	}
	for _, fid := range n.FolderIDs {
		if fid != fx.folderID {
			t.Errorf("detail folder_ids leaked non-shared folder: %v", n.FolderIDs)
		}
	}

	// Full.
	full := doJSON(t, fx.srv, http.MethodGet, "/api/notes/"+fx.noteID+"/full", nil, fx.otherHdr)
	if full.Code != http.StatusOK {
		t.Fatalf("full: %d %s", full.Code, full.Body.String())
	}
	var fullResp struct {
		Note         model.Note      `json:"note"`
		BodyMarkdown string          `json:"body_markdown"`
		Summaries    []model.Summary `json:"summaries"`
	}
	if err := json.Unmarshal(full.Body.Bytes(), &fullResp); err != nil {
		t.Fatalf("unmarshal full: %v", err)
	}
	if fullResp.BodyMarkdown != "Meeting notes" {
		t.Errorf("full body_markdown = %q, want %q", fullResp.BodyMarkdown, "Meeting notes")
	}
	if fullResp.Note.IsOwner == nil || *fullResp.Note.IsOwner {
		t.Errorf("shared reader full IsOwner = %v, want false", fullResp.Note.IsOwner)
	}
	if fullResp.Summaries == nil {
		t.Errorf("summaries should be [] not null")
	}

	// A private (never shared) note is invisible to the other user on all three.
	priv := doJSON(t, fx.srv, http.MethodPost, "/api/notes", map[string]any{"title": "Private"}, fx.ownerHdr)
	var p struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(priv.Body.Bytes(), &p)
	if rec := doJSON(t, fx.srv, http.MethodGet, "/api/notes/"+p.ID, nil, fx.otherHdr); rec.Code != http.StatusNotFound {
		t.Errorf("private note detail: want 404, got %d", rec.Code)
	}
	if rec := doJSON(t, fx.srv, http.MethodGet, "/api/notes/"+p.ID+"/full", nil, fx.otherHdr); rec.Code != http.StatusNotFound {
		t.Errorf("private note full: want 404, got %d", rec.Code)
	}
}

// --- exhaustive denial table -----------------------------------------------

// denialCase is one owner-only /api/notes/{id}/... route that must 404 for a
// non-owner even when the note is shared-readable (issue #12). route is the
// registered chi pattern (used by the completeness check below); path builds
// the concrete request path for fx.noteID.
type denialCase struct {
	name   string
	method string
	route  string
	path   func(noteID string) string
	body   any
}

func noBodyCase(name, method, route string) denialCase {
	return denialCase{name: name, method: method, route: route, path: func(id string) string {
		return "/api/notes/" + id + strings.TrimPrefix(route, "/api/notes/{id}")
	}}
}

func denialTable() []denialCase {
	uuidLabel := uuid.NewString()
	cases := []denialCase{
		noBodyCase("delete note", http.MethodDelete, "/api/notes/{id}"),
		noBodyCase("duplicate", http.MethodPost, "/api/notes/{id}/duplicate"),
		noBodyCase("start capture", http.MethodPost, "/api/notes/{id}/start-capture"),
		{name: "create share", method: http.MethodPost, route: "/api/notes/{id}/share", path: notePath("/share")},
		noBodyCase("list shares", http.MethodGet, "/api/notes/{id}/shares"),
		noBodyCase("pin", http.MethodPost, "/api/notes/{id}/pin"),
		noBodyCase("unpin", http.MethodDelete, "/api/notes/{id}/pin"),
		{name: "set event", method: http.MethodPost, route: "/api/notes/{id}/event", path: notePath("/event"), body: map[string]any{"event_id": uuid.NewString()}},
		noBodyCase("clear event", http.MethodDelete, "/api/notes/{id}/event"),
		noBodyCase("list action items", http.MethodGet, "/api/notes/{id}/action-items"),
		noBodyCase("restore", http.MethodPost, "/api/notes/{id}/restore"),
		noBodyCase("purge", http.MethodDelete, "/api/notes/{id}/permanent"),
		noBodyCase("export", http.MethodGet, "/api/notes/{id}/export"),
		noBodyCase("resummarize", http.MethodPost, "/api/notes/{id}/resummarize"),
		noBodyCase("retranscribe", http.MethodPost, "/api/notes/{id}/retranscribe"),
		{
			name: "template summarize", method: http.MethodPost, route: "/api/notes/{id}/templates/{templateID}/summarize",
			path: func(id string) string { return "/api/notes/" + id + "/templates/" + uuid.NewString() + "/summarize" },
		},
		noBodyCase("retry", http.MethodPost, "/api/notes/{id}/retry"),
		noBodyCase("process next", http.MethodPost, "/api/notes/{id}/process-next"),
		{name: "update body", method: http.MethodPut, route: "/api/notes/{id}/body", path: notePath("/body"), body: map[string]any{"content": "x"}},
		{name: "update title", method: http.MethodPatch, route: "/api/notes/{id}", path: notePath(""), body: map[string]any{"title": "x"}},
		{name: "add tag", method: http.MethodPost, route: "/api/notes/{id}/tags", path: notePath("/tags"), body: map[string]any{"name": "x"}},
		noBodyCase("remove tag", http.MethodDelete, "/api/notes/{id}/tags"),
		{name: "add link", method: http.MethodPost, route: "/api/notes/{id}/links", path: notePath("/links"), body: map[string]any{"to_note_id": uuid.NewString()}},
		noBodyCase("remove link", http.MethodDelete, "/api/notes/{id}/links"),
		noBodyCase("list links", http.MethodGet, "/api/notes/{id}/links"),
		noBodyCase("related", http.MethodGet, "/api/notes/{id}/related"),
		noBodyCase("list people", http.MethodGet, "/api/notes/{id}/people"),
		noBodyCase("relink speakers", http.MethodPost, "/api/notes/{id}/relink-speakers"),
		noBodyCase("list speaker aliases", http.MethodGet, "/api/notes/{id}/speaker-aliases"),
		{
			name: "upsert speaker alias", method: http.MethodPut, route: "/api/notes/{id}/speaker-aliases/{label}",
			path: func(id string) string { return "/api/notes/" + id + "/speaker-aliases/" + uuidLabel },
			body: map[string]any{"alias_name": "Alex"},
		},
		{
			name: "set speaker alias person", method: http.MethodPut, route: "/api/notes/{id}/speaker-aliases/{label}/person",
			path: func(id string) string { return "/api/notes/" + id + "/speaker-aliases/" + uuidLabel + "/person" },
			body: map[string]any{},
		},
		{
			name: "delete speaker alias", method: http.MethodDelete, route: "/api/notes/{id}/speaker-aliases/{label}",
			path: func(id string) string { return "/api/notes/" + id + "/speaker-aliases/" + uuidLabel },
		},
		noBodyCase("get transcript review", http.MethodGet, "/api/notes/{id}/transcript/review"),
		{
			name: "post transcript review", method: http.MethodPost, route: "/api/notes/{id}/transcript/review",
			path: notePath("/transcript/review"), body: map[string]any{"review_state": "in_review", "generation": 1},
		},
		noBodyCase("audio download url", http.MethodGet, "/api/notes/{id}/audio-url"),
		noBodyCase("audio upload url", http.MethodPost, "/api/notes/{id}/audio-upload-url"),
	}
	return cases
}

func notePath(suffix string) func(string) string {
	return func(id string) string { return "/api/notes/" + id + suffix }
}

func TestNoteOwnerOnlyRoutesDenyNonOwnerEvenWhenShared(t *testing.T) {
	t.Parallel()
	fx := newSharedNoteFixture(t)

	for _, tc := range denialTable() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, fx.srv, tc.method, tc.path(fx.noteID), tc.body, fx.otherHdr)
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s %s: want 404 for non-owner on a shared note, got %d %s",
					tc.method, tc.path(fx.noteID), rec.Code, rec.Body.String())
			}
		})
	}

	// One route that isn't path-scoped by note id: batch export. Documented
	// here rather than in the table above because it takes note_ids in the
	// body, not the URL.
	rec := doJSON(t, fx.srv, http.MethodPost, "/api/export/batch",
		map[string]any{"note_ids": []string{fx.noteID}, "format": "md"}, fx.otherHdr)
	if rec.Code != http.StatusNotFound {
		t.Errorf("batch export of a shared note by non-owner: want 404, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestNoteScopedRouteRegistrationCompleteness mechanically enumerates every
// registered /api/notes/{id}/... route and requires it to be exactly one of:
// a shared-readable route, a filing route, an explicitly documented exclusion
// (not amenable to this table), or a row in denialTable() above. A new note
// route that omits this classification fails the build, by design.
func TestNoteScopedRouteRegistrationCompleteness(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	const idPrefix = "/api/notes/{id}"
	sharedReadable := map[string]bool{
		"GET " + idPrefix:           true,
		"GET " + idPrefix + "/full": true,
		// Live in-meeting prompt stream (issue #764): gated by
		// GetReadableNote like /full, since it carries only material derived
		// from the transcript a reader can already see.
		"GET " + idPrefix + "/live-prompts": true,
	}
	filing := map[string]bool{
		"POST " + idPrefix + "/folders":              true,
		"DELETE " + idPrefix + "/folders/{folderID}": true,
	}
	// Documented exclusions: not amenable to a simple httptest 404 assertion.
	excluded := map[string]bool{
		"GET " + idPrefix + "/stream":          true, // websocket upgrade
		"POST " + idPrefix + "/audio-uploaded": true, // requires a verified real upload
	}

	inTable := map[string]bool{}
	for _, tc := range denialTable() {
		inTable[tc.method+" "+tc.route] = true
	}

	routes, ok := srv.Handler().(chi.Routes)
	if !ok {
		t.Fatalf("server handler does not implement chi.Routes")
	}

	var uncovered []string
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if route != idPrefix && !strings.HasPrefix(route, idPrefix+"/") {
			return nil
		}
		key := method + " " + route
		if sharedReadable[key] || filing[key] || excluded[key] || inTable[key] {
			return nil
		}
		uncovered = append(uncovered, key)
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	if len(uncovered) > 0 {
		t.Errorf("registered /api/notes/{id}/... routes missing an issue-#12 classification (add to denialTable, sharedReadable, filing, or excluded): %v", uncovered)
	}
}

// --- team_sharing_available capability -------------------------------------

func TestTeamSharingAvailableCapability(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	hdr, _ := authHeaderForUser(t, st, "cap-solo@example.com")

	rec := doJSON(t, srv, http.MethodGet, "/api/capabilities", nil, hdr)
	if rec.Code != http.StatusOK {
		t.Fatalf("capabilities (1 user): %d %s", rec.Code, rec.Body.String())
	}
	var caps struct {
		TeamSharingAvailable bool `json:"team_sharing_available"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &caps)
	if caps.TeamSharingAvailable {
		t.Error("team_sharing_available with 1 user = true, want false")
	}

	_, _ = authHeaderForUser(t, st, "cap-duo@example.com")

	rec2 := doJSON(t, srv, http.MethodGet, "/api/capabilities", nil, hdr)
	_ = json.Unmarshal(rec2.Body.Bytes(), &caps)
	if !caps.TeamSharingAvailable {
		t.Error("team_sharing_available with 2 users = false, want true")
	}
}
