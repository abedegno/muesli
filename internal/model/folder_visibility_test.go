package model

import "testing"

// TestFolderJSONRoundTrip proves the folder-sharing response fields added for
// issue #12 (owner_id, visibility, is_owner) always serialize, for both a
// private owned folder and a shared non-owned one — "one response shape" per
// the design doc.
func TestFolderJSONRoundTrip(t *testing.T) {
	t.Parallel()

	parentID := "folder_parent"
	cases := []struct {
		name     string
		value    Folder
		wantKeys []string
	}{
		{
			name: "private owned folder",
			value: Folder{
				ID:         "folder_1",
				OwnerID:    "owner_1",
				Name:       "Clients",
				ParentID:   &parentID,
				Visibility: FolderPrivate,
				NoteCount:  3,
				IsOwner:    true,
			},
			wantKeys: []string{"id", "owner_id", "name", "parent_id", "visibility", "created_at", "note_count", "is_owner"},
		},
		{
			name: "shared non-owned folder",
			value: Folder{
				ID:         "folder_2",
				OwnerID:    "owner_2",
				Name:       "Team notes",
				ParentID:   nil,
				Visibility: FolderShared,
				NoteCount:  0,
				IsOwner:    false,
			},
			wantKeys: []string{"id", "owner_id", "name", "parent_id", "visibility", "created_at", "note_count", "is_owner"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertJSONRoundTrip(t, tc.value, tc.wantKeys)
		})
	}
}

// baseNoteKeys are the Note JSON keys always on the wire regardless of route
// (no omitempty on their struct tags).
var baseNoteKeys = []string{"id", "owner_id", "title", "status", "pinned", "created_at", "updated_at", "tags", "folder_ids", "partial_transcript"}

// TestNoteIsOwnerOnlyOnSharedReadableResponses proves Note.IsOwner (issue
// #12) is omitted from JSON on the owner-only routes (nil, the zero value)
// and present as an explicit boolean on the three shared-readable routes
// (folder-filtered list, detail, full).
func TestNoteIsOwnerOnlyOnSharedReadableResponses(t *testing.T) {
	t.Parallel()

	trueVal := true
	falseVal := false

	cases := []struct {
		name     string
		value    Note
		wantKeys []string
	}{
		{
			name:     "owner-only route: nil is_owner omitted",
			value:    Note{ID: "n1", Tags: []string{}, FolderIDs: []string{}},
			wantKeys: baseNoteKeys,
		},
		{
			name:     "shared-readable route: owner",
			value:    Note{ID: "n2", Tags: []string{}, FolderIDs: []string{}, IsOwner: &trueVal},
			wantKeys: append(append([]string{}, baseNoteKeys...), "is_owner"),
		},
		{
			name:     "shared-readable route: non-owner",
			value:    Note{ID: "n3", Tags: []string{}, FolderIDs: []string{}, IsOwner: &falseVal},
			wantKeys: append(append([]string{}, baseNoteKeys...), "is_owner"),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertJSONRoundTrip(t, tc.value, tc.wantKeys)
		})
	}
}
