package api

import "net/http"

const (
	sharedFoldersDefaultLimit = 50
	sharedFoldersMaxLimit     = 100
)

type sharedFolderView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	OwnerID    string `json:"owner_id"`
	OwnerEmail string `json:"owner_email"`
	NoteCount  int    `json:"note_count"`
	CountScope string `json:"count_scope"`
}

type sharedFolderListResponse struct {
	Items      []sharedFolderView `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

// handleListSharedFolders is GET /api/folders/shared: folders the requester
// can see solely through an explicit folder_members grant, never through
// ownership. Deliberately a separate endpoint from GET /api/folders (see
// design doc) -- owner and shared rows have no common meaningful order.
func (s *Server) handleListSharedFolders(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())

	var cursor sharedFolderCursor
	limit, hasCursor, ok := parsePageRequest(w, r.URL.Query(), sharedFoldersDefaultLimit, sharedFoldersMaxLimit, sharedFolderCursorKind, &cursor)
	if !ok {
		return
	}
	if hasCursor && (cursor.CreatedAt.IsZero() || cursor.FolderID == "") {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return
	}

	folders, err := s.deps.Store.ListSharedFolders(r.Context(), uid, limit+1, cursor.CreatedAt, cursor.FolderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := sharedFolderListResponse{Items: []sharedFolderView{}}
	for i, f := range folders {
		if i == limit {
			resp.NextCursor = encodeCursor(sharedFolderCursorKind, sharedFolderCursor{CreatedAt: folders[limit-1].GrantedAt, FolderID: folders[limit-1].ID})
			break
		}
		resp.Items = append(resp.Items, sharedFolderView{
			ID: f.ID, Name: f.Name, OwnerID: f.OwnerID, OwnerEmail: f.OwnerEmail,
			NoteCount: f.NoteCount, CountScope: f.CountScope,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}
