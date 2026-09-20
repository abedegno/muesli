package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/jackc/pgx/v5"
)

// mobileSnippetMaxRunes bounds a mobile list snippet (issue #767). The
// desktop snippet() helper stays bounded at 160 runes; mobile gets a longer
// 240-rune allowance per the accepted spec/plan.
const mobileSnippetMaxRunes = 240

// MobileNoteCursor is the decoded, validated keyset position for mobile note
// pagination: the exact ordering tuple (pinned, created_at, id) of the last
// row returned on the previous page. It is opaque to callers of the API but
// the store consumes it directly once the API layer has decoded/validated it.
type MobileNoteCursor struct {
	Pinned    bool
	CreatedAt time.Time
	ID        string
}

// MobileNote is the field-minimized note shape returned by the mobile list
// and (for its shared fields) detail routes. It intentionally omits owner_id,
// folder ids, event/audio metadata, transcripts, and summary bodies.
type MobileNote struct {
	ID        string
	Title     string
	Status    string
	Pinned    bool
	StartedAt *time.Time
	EndedAt   *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
	// Snippet is populated only by ListMobileNotes; the detail route omits it.
	Snippet string
	// Tags is always a non-nil slice.
	Tags []string
}

// MobileNoteSummary is the field-minimized summary shape for the mobile
// detail route: it omits owner, transcript refs, agent/model, and mutation
// fields, keeping only what the reader displays.
type MobileNoteSummary struct {
	ID           string
	TemplateName string
	Status       string
	Truncated    bool
	// Sections is always a non-nil slice of (heading, content_markdown) pairs.
	Sections []model.SummarySection
}

// MobileNoteDetail is the full payload for the mobile note-detail route.
type MobileNoteDetail struct {
	Note         MobileNote
	BodyMarkdown string
	// Summaries is always a non-nil slice, ordered created_at ASC, id ASC.
	Summaries []MobileNoteSummary
}

// mobileNoteListQuery selects exactly the columns the mobile list needs, plus
// the raw body (for the bounded snippet) — never the full transcript or
// summary content. owner_id and deleted_at are filtered, not selected.
const mobileNoteListBaseQuery = `SELECT n.id, n.title, n.status, n.pinned, n.started_at, n.ended_at,
	       n.created_at, n.updated_at, COALESCE(nb.content, '')
	FROM notes n
	LEFT JOIN note_bodies nb ON nb.note_id = n.id
	WHERE n.owner_id = $1 AND n.deleted_at IS NULL`

// ListMobileNotes returns up to limit live notes owned by ownerID, ordered
// pinned DESC, created_at DESC, id DESC, using a strict keyset predicate so
// idx_notes_owner_pagination (owner_id, pinned DESC, created_at DESC, id DESC)
// WHERE deleted_at IS NULL can serve the whole query without a sequential
// scan and without an OFFSET that degrades at depth.
//
// It fetches at most limit+1 rows in one query (no per-row queries, no
// server-held cursor/result-set state): the caller uses the presence of the
// (limit+1)th row to decide whether to emit a next_cursor, then discards it.
// Tags are attached afterward via one bounded bulk query keyed by the
// returned note ids (never one query per note).
//
// after is nil for the first page. limit must already be validated by the
// caller (1-100); this function does not re-validate it, but always caps the
// query to limit+1 regardless of caller input to defend a future call site
// that skips validation.
func (s *Store) ListMobileNotes(ctx context.Context, ownerID string, limit int, after *MobileNoteCursor) ([]MobileNote, bool, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}

	query := mobileNoteListBaseQuery
	args := []any{ownerID}
	if after != nil {
		// Strict "seek" predicate for ORDER BY pinned DESC, created_at DESC,
		// id DESC: return only rows strictly before the cursor tuple in that
		// order. Expanded (rather than a ROW(...) comparison) so every branch
		// maps cleanly onto the composite index's column order.
		query += ` AND (
			n.pinned < $2
			OR (n.pinned = $2 AND n.created_at < $3)
			OR (n.pinned = $2 AND n.created_at = $3 AND n.id < $4)
		)`
		args = append(args, after.Pinned, after.CreatedAt, after.ID)
	}
	query += fmt.Sprintf(" ORDER BY n.pinned DESC, n.created_at DESC, n.id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit+1)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	out := []MobileNote{}
	for rows.Next() {
		var n MobileNote
		var body string
		if err := rows.Scan(&n.ID, &n.Title, &n.Status, &n.Pinned, &n.StartedAt, &n.EndedAt,
			&n.CreatedAt, &n.UpdatedAt, &body); err != nil {
			return nil, false, err
		}
		n.Snippet = snippetN(body, mobileSnippetMaxRunes)
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}

	ids := make([]string, len(out))
	for i := range out {
		ids[i] = out[i].ID
	}
	tagMap, err := s.tagsForNotes(ctx, ids)
	if err != nil {
		return nil, false, err
	}
	for i := range out {
		if tags := tagMap[out[i].ID]; tags != nil {
			out[i].Tags = tags
		} else {
			out[i].Tags = []string{}
		}
	}

	return out, hasMore, nil
}

// GetMobileNoteDetail returns the field-minimized detail payload for one live
// note owned by ownerID. It returns ErrNotFound for an absent, deleted, or
// other-owner note — callers must not distinguish those cases in the response
// they send (issue #767: indistinguishable 404).
//
// noteID is used as an opaque string in the query; callers are expected to
// have already rejected syntactically invalid UUIDs so those also collapse
// into ErrNotFound before reaching the store, rather than depending on a
// Postgres type-cast error.
func (s *Store) GetMobileNoteDetail(ctx context.Context, ownerID, noteID string) (MobileNoteDetail, error) {
	var d MobileNoteDetail
	var body string
	err := s.pool.QueryRow(ctx,
		`SELECT n.id, n.title, n.status, n.pinned, n.started_at, n.ended_at,
		        n.created_at, n.updated_at, COALESCE(nb.content, '')
		 FROM notes n
		 LEFT JOIN note_bodies nb ON nb.note_id = n.id
		 WHERE n.id = $1 AND n.owner_id = $2 AND n.deleted_at IS NULL`,
		noteID, ownerID).
		Scan(&d.Note.ID, &d.Note.Title, &d.Note.Status, &d.Note.Pinned, &d.Note.StartedAt, &d.Note.EndedAt,
			&d.Note.CreatedAt, &d.Note.UpdatedAt, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return MobileNoteDetail{}, ErrNotFound
	}
	if err != nil {
		return MobileNoteDetail{}, err
	}
	d.BodyMarkdown = body

	tags, err := s.tagsForNotes(ctx, []string{d.Note.ID})
	if err != nil {
		return MobileNoteDetail{}, err
	}
	if t := tags[d.Note.ID]; t != nil {
		d.Note.Tags = t
	} else {
		d.Note.Tags = []string{}
	}

	summaries, err := mobileSummariesForNote(ctx, s.pool, d.Note.ID)
	if err != nil {
		return MobileNoteDetail{}, err
	}
	d.Summaries = summaries

	return d, nil
}

// mobileSummariesForNote loads one note's summaries ordered created_at ASC,
// id ASC (issue #767; distinct from GetSummaries, which orders by template
// name for the desktop full-note view), field-minimized to id, template
// name, status, truncated, and sections.
func mobileSummariesForNote(ctx context.Context, q txQuerier, noteID string) ([]MobileNoteSummary, error) {
	rows, err := q.Query(ctx,
		`SELECT s.id, COALESCE(t.name,''),
		        COALESCE(s.content->>'status', $2), COALESCE(s.content->'sections','[]'::jsonb),
		        COALESCE((s.content->>'truncated')::boolean, false)
		 FROM summaries s
		 LEFT JOIN templates t ON t.id = s.template_id
		 WHERE s.note_id = $1
		 ORDER BY s.created_at ASC, s.id ASC`, noteID, model.SummaryReady)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []MobileNoteSummary{}
	for rows.Next() {
		var sm MobileNoteSummary
		var sectionsJSON []byte
		if err := rows.Scan(&sm.ID, &sm.TemplateName, &sm.Status, &sectionsJSON, &sm.Truncated); err != nil {
			return nil, err
		}
		sections := []model.SummarySection{}
		if err := json.Unmarshal(sectionsJSON, &sections); err != nil {
			return nil, err
		}
		sm.Sections = sections
		out = append(out, sm)
	}
	return out, rows.Err()
}
