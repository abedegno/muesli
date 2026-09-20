package store

import (
	"context"
	"errors"

	"github.com/abedegno/muesli/internal/model"
	"github.com/jackc/pgx/v5"
)

// CreateConversation creates a new conversation for ownerID. noteID may be nil
// for a global (cross-note) conversation; when non-nil, the note must belong
// to ownerID or ErrNotFound is returned.
func (s *Store) CreateConversation(ctx context.Context, ownerID string, noteID *string, title string, modelOverride *string) (model.Conversation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Conversation{}, err
	}
	defer tx.Rollback(ctx)

	if noteID != nil {
		var ok bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM notes WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL)`,
			*noteID, ownerID).Scan(&ok); err != nil {
			return model.Conversation{}, err
		}
		if !ok {
			return model.Conversation{}, ErrNotFound
		}
	}

	var c model.Conversation
	err = tx.QueryRow(ctx,
		`INSERT INTO conversations (owner_id, note_id, title, model_override)
		 VALUES ($1,$2,$3,$4)
		 RETURNING id, owner_id, note_id, title, model_override, created_at, updated_at`,
		ownerID, noteID, title, modelOverride).
		Scan(&c.ID, &c.OwnerID, &c.NoteID, &c.Title, &c.ModelOverride, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return model.Conversation{}, err
	}
	return c, tx.Commit(ctx)
}

// GetConversation returns one conversation, owner-scoped. ErrNotFound when
// absent or not owned by ownerID.
func (s *Store) GetConversation(ctx context.Context, ownerID, id string) (model.Conversation, error) {
	var c model.Conversation
	err := s.pool.QueryRow(ctx,
		`SELECT id, owner_id, note_id, title, model_override, created_at, updated_at
		 FROM conversations WHERE id=$1 AND owner_id=$2`, id, ownerID).
		Scan(&c.ID, &c.OwnerID, &c.NoteID, &c.Title, &c.ModelOverride, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Conversation{}, ErrNotFound
	}
	return c, err
}

// ListConversations lists conversations owned by ownerID. When noteID is
// non-nil, only that note's conversations are returned; when nil, every
// conversation owned by ownerID is returned (note-scoped and global alike),
// most-recently-updated first.
func (s *Store) ListConversations(ctx context.Context, ownerID string, noteID *string) ([]model.Conversation, error) {
	var rows pgx.Rows
	var err error
	if noteID != nil {
		rows, err = s.pool.Query(ctx,
			`SELECT id, owner_id, note_id, title, model_override, created_at, updated_at
			 FROM conversations WHERE owner_id=$1 AND note_id=$2
			 ORDER BY updated_at DESC`, ownerID, *noteID)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT id, owner_id, note_id, title, model_override, created_at, updated_at
			 FROM conversations WHERE owner_id=$1
			 ORDER BY updated_at DESC`, ownerID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Conversation{}
	for rows.Next() {
		var c model.Conversation
		if err := rows.Scan(&c.ID, &c.OwnerID, &c.NoteID, &c.Title, &c.ModelOverride, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteConversation hard-deletes a conversation, owner-scoped. FK cascade
// removes its messages. Returns ErrNotFound if absent or not owned.
func (s *Store) DeleteConversation(ctx context.Context, ownerID, id string) error {
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM conversations WHERE id=$1 AND owner_id=$2`, id, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AppendMessage inserts a new message into conversationID and bumps the
// parent conversation's updated_at, in a single transaction.
func (s *Store) AppendMessage(ctx context.Context, conversationID, role, content, modelName string, tokensUsed *int) (model.Message, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Message{}, err
	}
	defer tx.Rollback(ctx)

	var m model.Message
	err = tx.QueryRow(ctx,
		`INSERT INTO messages (conversation_id, role, content, model, tokens_used)
		 VALUES ($1,$2,$3,$4,$5)
		 RETURNING id, conversation_id, role, content, model, tokens_used, created_at`,
		conversationID, role, content, modelName, tokensUsed).
		Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.Model, &m.TokensUsed, &m.CreatedAt)
	if err != nil {
		return model.Message{}, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE conversations SET updated_at=now() WHERE id=$1`, conversationID); err != nil {
		return model.Message{}, err
	}
	return m, tx.Commit(ctx)
}

// ListMessages returns a conversation's messages in chronological order.
// Owner-scoped: the conversation must belong to ownerID, or ErrNotFound.
//
// A second, bounded query joins message_sources for every message id just
// loaded (issue #765) and populates model.Message.Sources only where rows
// exist, ordered by citation number -- an ordinary (non-cross) message
// simply has none and its Sources stays nil, matching its omitempty JSON
// tag. This never falls back to a per-message query.
func (s *Store) ListMessages(ctx context.Context, ownerID, conversationID string) ([]model.Message, error) {
	var owned bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM conversations WHERE id=$1 AND owner_id=$2)`,
		conversationID, ownerID).Scan(&owned); err != nil {
		return nil, err
	}
	if !owned {
		return nil, ErrNotFound
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, conversation_id, role, content, model, tokens_used, created_at
		 FROM messages WHERE conversation_id=$1
		 ORDER BY created_at ASC`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Message{}
	ids := make([]string, 0)
	for rows.Next() {
		var m model.Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.Model, &m.TokensUsed, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
		ids = append(ids, m.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return out, nil
	}

	// LEFT JOIN notes (not a bare column read) so a *soft-deleted* (trashed)
	// note's citation goes inert immediately: n.id comes back NULL whenever
	// the joined note is missing OR has deleted_at set, not only once the
	// note is later hard-deleted and the FK's ON DELETE SET NULL fires. This
	// matches ChatThread.tsx treating note_id !== null as the sole signal
	// that a citation is clickable.
	sourceRows, err := s.pool.Query(ctx,
		`SELECT ms.message_id, ms.citation_number, n.id, ms.transcript_generation, ms.segment_index, ms.timestamp_ms, ms.snippet
		   FROM message_sources ms
		   LEFT JOIN notes n ON n.id = ms.note_id AND n.deleted_at IS NULL
		  WHERE ms.message_id = ANY($1)
		  ORDER BY ms.message_id, ms.citation_number`, ids)
	if err != nil {
		return nil, err
	}
	defer sourceRows.Close()
	sourcesByMessageID := make(map[string][]model.MessageSource)
	for sourceRows.Next() {
		var messageID string
		var src model.MessageSource
		if err := sourceRows.Scan(&messageID, &src.N, &src.NoteID, &src.TranscriptGeneration, &src.SegmentIndex, &src.Timestamp, &src.Snippet); err != nil {
			return nil, err
		}
		sourcesByMessageID[messageID] = append(sourcesByMessageID[messageID], src)
	}
	if err := sourceRows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if srcs, ok := sourcesByMessageID[out[i].ID]; ok {
			out[i].Sources = srcs
		}
	}
	return out, nil
}

// SetConversationTitleIfEmpty sets the title of a conversation ONLY if it is
// currently empty (the empty string). Returns true if a row was updated (title
// was empty and is now set), false if the title was already non-empty. This
// DB-level guard ensures an existing title is never overwritten, even under
// concurrent attempts to set one.
func (s *Store) SetConversationTitleIfEmpty(ctx context.Context, conversationID, title string) (bool, error) {
	ct, err := s.pool.Exec(ctx,
		`UPDATE conversations SET title=$2, updated_at=now() WHERE id=$1 AND title=''`,
		conversationID, title)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}
