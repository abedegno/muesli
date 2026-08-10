package store

import (
	"context"
	"fmt"

	"github.com/abedegno/muesli/internal/model"
	"github.com/jackc/pgx/v5"
)

// SearchNotesLexical returns at most limit owner-scoped notes matching title,
// the existing body snippet, or indexed transcript text.
func (s *Store) SearchNotesLexical(ctx context.Context, ownerID, q string, f ListNotesFilter, limit int) ([]model.Note, error) {
	joinFolder, where, args := noteFilterSQL(ownerID, f)
	args = append(args, q)
	qarg := len(args)
	args = append(args, limit)
	query := fmt.Sprintf(`SELECT n.id, n.owner_id, n.title, n.status, n.pinned, n.started_at, n.ended_at,
		n.partial_transcript, n.created_at, n.updated_at, COALESCE(nb.content, ''), n.event_id
		FROM notes n %s LEFT JOIN note_bodies nb ON nb.note_id = n.id
		WHERE %s AND (n.title ILIKE '%%' || $%d || '%%'
			OR COALESCE(nb.content, '') ILIKE '%%' || $%d || '%%'
			OR EXISTS (SELECT 1 FROM transcripts tr JOIN transcript_segments ts ON ts.transcript_id = tr.id
				WHERE tr.note_id = n.id AND ts.text_tsv @@ websearch_to_tsquery('english', $%d)))
		ORDER BY n.created_at DESC, n.id LIMIT $%d`, joinFolder, where, qarg, qarg, qarg, len(args))
	return s.scanSearchNotes(ctx, query, args...)
}

// FilterNotesByIDs applies the same owner and list filters to a bounded set of IDs.
func (s *Store) FilterNotesByIDs(ctx context.Context, ownerID string, ids []string, f ListNotesFilter) ([]model.Note, error) {
	if len(ids) == 0 {
		return []model.Note{}, nil
	}
	joinFolder, where, args := noteFilterSQL(ownerID, f)
	args = append(args, ids)
	query := fmt.Sprintf(`SELECT n.id, n.owner_id, n.title, n.status, n.pinned, n.started_at, n.ended_at,
		n.partial_transcript, n.created_at, n.updated_at, COALESCE(nb.content, ''), n.event_id
		FROM notes n %s LEFT JOIN note_bodies nb ON nb.note_id = n.id
		WHERE %s AND n.id = ANY($%d)`, joinFolder, where, len(args))
	return s.scanSearchNotes(ctx, query, args...)
}

func (s *Store) scanSearchNotes(ctx context.Context, query string, args ...any) ([]model.Note, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Note, error) {
		var n model.Note
		var body string
		err := row.Scan(&n.ID, &n.OwnerID, &n.Title, &n.Status, &n.Pinned, &n.StartedAt, &n.EndedAt,
			&n.PartialTranscript, &n.CreatedAt, &n.UpdatedAt, &body, &n.EventID)
		n.Snippet = snippet(body)
		return n, err
	})
	if out == nil {
		out = []model.Note{}
	}
	return out, err
}
