package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// errBadCursor is returned by cursor decoding for any malformed input: bad
// base64, bad JSON, wrong kind, missing tuple values, or trailing data.
var errBadCursor = errors.New("invalid cursor")

// cursorEnvelope is the on-the-wire shape of every opaque cursor this API
// issues: unpadded base64url JSON carrying a `kind` tag (so a cursor cannot
// be replayed against a different endpoint's ordering) plus that endpoint's
// complete ordering tuple.
type cursorEnvelope struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

// encodeCursor packs a typed ordering tuple into an opaque, unpadded
// base64url cursor tagged with kind. data must be JSON-marshalable (every
// cursor payload here is a plain struct of strings/times, so this cannot
// fail in practice); a marshal failure yields an unusable empty string
// rather than a panic.
func encodeCursor(kind string, data any) string {
	raw, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	b, err := json.Marshal(cursorEnvelope{Kind: kind, Data: raw})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// decodeCursor unpacks a cursor previously produced by encodeCursor into
// dst, verifying its kind and rejecting any trailing input after the JSON
// value at either the envelope or payload level.
func decodeCursor(kind, cursor string, dst any) error {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return errBadCursor
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	var env cursorEnvelope
	if err := dec.Decode(&env); err != nil {
		return errBadCursor
	}
	if dec.More() {
		return errBadCursor
	}
	if env.Kind != kind {
		return errBadCursor
	}
	dec2 := json.NewDecoder(bytes.NewReader(env.Data))
	if err := dec2.Decode(dst); err != nil {
		return errBadCursor
	}
	if dec2.More() {
		return errBadCursor
	}
	return nil
}

// parseLimit reads the `limit` query parameter, defaulting to def and capped
// at max inclusive. A non-numeric, zero, negative, or over-max value is
// rejected (ok=false).
func parseLimit(q url.Values, def, max int) (n int, ok bool) {
	raw := q.Get("limit")
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > max {
		return 0, false
	}
	return n, true
}

// parsePageRequest parses and validates the `limit`/`cursor` query
// parameters shared by every cursor-paginated list endpoint. def/max bound
// the limit. When a cursor is present it is decoded into dst, scoped to
// kind so one endpoint's cursor cannot be replayed against another's; the
// caller is responsible for rejecting a decoded-but-incomplete tuple (e.g.
// zero-value fields) as also invalid. On any malformed input this writes a
// 400 itself and returns ok=false.
func parsePageRequest(w http.ResponseWriter, q url.Values, def, max int, kind string, dst any) (limit int, hasCursor bool, ok bool) {
	limit, okLimit := parseLimit(q, def, max)
	if !okLimit {
		writeError(w, http.StatusBadRequest, "invalid limit")
		return 0, false, false
	}
	cursor := q.Get("cursor")
	if cursor == "" {
		return limit, false, true
	}
	if err := decodeCursor(kind, cursor, dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return 0, false, false
	}
	return limit, true, true
}

// userCursor orders GET /api/users and GET /api/admin/users by
// (created_at, id) -- the same ordering ListUsers uses.
type userCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

const userCursorKind = "users"

// folderMemberCursor orders GET /api/folders/{id}/members by
// (created_at, user_id).
type folderMemberCursor struct {
	CreatedAt time.Time `json:"created_at"`
	UserID    string    `json:"user_id"`
}

const folderMemberCursorKind = "folder_members"

// sharedFolderCursor orders GET /api/folders/shared by
// (folder_members.created_at, folder_id).
type sharedFolderCursor struct {
	CreatedAt time.Time `json:"created_at"`
	FolderID  string    `json:"folder_id"`
}

const sharedFolderCursorKind = "shared_folders"
