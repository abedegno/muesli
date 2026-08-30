package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ListNotesFilter holds optional parameters for narrowing ListNotes results.
// All fields are independently optional; omitting all returns every live note
// for the owner (existing behaviour). Active filters are AND-ed together.
type ListNotesFilter struct {
	Tag         string // filter to notes carrying this tag (case-insensitive)
	Status      string // filter to notes with this exact status
	FolderID    string // UUID of folder; applies only when FolderIDSet is true
	FolderIDSet bool   // true when FolderID was explicitly supplied
	PersonID    string // UUID of person; applies only when PersonIDSet is true
	PersonIDSet bool   // true when PersonID was explicitly supplied
	CreatedFrom *time.Time
	CreatedTo   *time.Time
}

const getNoteSelectQuery = `SELECT id, owner_id, title, status, pinned, started_at, ended_at, partial_transcript, audio_object_key, retention_state, created_at, updated_at, event_id
		 FROM notes WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL`

func notesOrderClause(folderIDSet bool) string {
	if folderIDSet {
		return "n.pinned DESC, nf.position, n.created_at DESC, n.id"
	}
	return "n.pinned DESC, n.created_at DESC, n.id"
}

func noteFilterSQL(ownerID string, f ListNotesFilter) (string, string, []any) {
	where := []string{"n.owner_id=$1", "n.deleted_at IS NULL"}
	args := []any{ownerID}
	joinFolder := ""
	if f.Tag != "" {
		args = append(args, f.Tag)
		where = append(where, fmt.Sprintf(`EXISTS (SELECT 1 FROM note_tags nt JOIN tags t ON t.id = nt.tag_id
			WHERE nt.note_id = n.id AND lower(t.name) = lower($%d) AND t.owner_id = n.owner_id)`, len(args)))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		where = append(where, fmt.Sprintf("n.status = $%d", len(args)))
	}
	if f.FolderIDSet {
		args = append(args, f.FolderID)
		joinFolder = fmt.Sprintf(`JOIN note_folders nf ON nf.note_id = n.id AND nf.folder_id = $%d`, len(args))
	}
	if f.PersonIDSet {
		args = append(args, f.PersonID)
		where = append(where, fmt.Sprintf(`(
			EXISTS (SELECT 1 FROM calendar_events ce JOIN people p ON p.id = $%d AND p.owner_id = n.owner_id
				WHERE ce.id = n.event_id AND ce.owner_id = n.owner_id AND EXISTS (
					SELECT 1 FROM jsonb_array_elements(COALESCE(ce.attendees, '[]'::jsonb)) attendee
					WHERE lower(attendee->>'email') = lower(p.primary_email)))
			OR EXISTS (SELECT 1 FROM note_speaker_aliases nsa WHERE nsa.note_id = n.id
				AND nsa.owner_id = n.owner_id AND nsa.person_id = $%d))`, len(args), len(args)))
	}
	if f.CreatedFrom != nil {
		args = append(args, *f.CreatedFrom)
		where = append(where, fmt.Sprintf("n.created_at >= $%d", len(args)))
	}
	if f.CreatedTo != nil {
		args = append(args, *f.CreatedTo)
		where = append(where, fmt.Sprintf("n.created_at <= $%d", len(args)))
	}
	return joinFolder, strings.Join(where, " AND "), args
}

func (s *Store) CreateNote(ctx context.Context, ownerID, title string) (model.Note, error) {
	id := uuid.NewString()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Note{}, err
	}
	defer tx.Rollback(ctx)

	var n model.Note
	err = tx.QueryRow(ctx,
		`INSERT INTO notes (id, owner_id, title, status)
		 VALUES ($1,$2,$3,$4)
		 RETURNING id, owner_id, title, status, pinned, created_at, updated_at, event_id`,
		id, ownerID, title, model.NoteDraft).
		Scan(&n.ID, &n.OwnerID, &n.Title, &n.Status, &n.Pinned, &n.CreatedAt, &n.UpdatedAt, &n.EventID)
	if err != nil {
		return model.Note{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO note_bodies (note_id, content) VALUES ($1,'')`, id); err != nil {
		return model.Note{}, err
	}
	return n, tx.Commit(ctx)
}

// DuplicateNote creates a fresh note owned by ownerID that copies the editable
// content and organization of noteID. The new note starts as a draft, with no
// audio/job/transcript/summary data carried over.
func (s *Store) DuplicateNote(ctx context.Context, ownerID, noteID string) (model.Note, error) {
	orig, err := s.GetNote(ctx, ownerID, noteID)
	if err != nil {
		return model.Note{}, err
	}

	body, err := s.NoteBody(ctx, noteID)
	if err != nil {
		return model.Note{}, err
	}
	tags, err := s.NoteTags(ctx, noteID)
	if err != nil {
		return model.Note{}, err
	}
	folderIDs, err := s.NoteFolderIDs(ctx, noteID)
	if err != nil {
		return model.Note{}, err
	}
	liveFolders, err := s.ListFolders(ctx, ownerID)
	if err != nil {
		return model.Note{}, err
	}
	liveFolderSet := make(map[string]struct{}, len(liveFolders))
	for _, folder := range liveFolders {
		liveFolderSet[folder.ID] = struct{}{}
	}

	copyNote, err := s.CreateNote(ctx, ownerID, "Copy of "+orig.Title)
	if err != nil {
		return model.Note{}, err
	}
	if err := s.UpdateNoteBody(ctx, ownerID, copyNote.ID, body); err != nil {
		return model.Note{}, err
	}
	for _, tag := range tags {
		if _, err := s.AddNoteTag(ctx, ownerID, copyNote.ID, tag); err != nil {
			return model.Note{}, err
		}
	}
	for _, folderID := range folderIDs {
		if _, ok := liveFolderSet[folderID]; !ok {
			continue
		}
		if err := s.AddNoteFolder(ctx, ownerID, copyNote.ID, folderID); err != nil {
			return model.Note{}, err
		}
	}

	copyNote.Tags = append([]string{}, tags...)
	copyNote.FolderIDs = append([]string{}, folderIDs...)
	return copyNote, nil
}

// StartNoteCapture advances a draft note to recording. Notes already beyond
// the draft state are returned unchanged so retries cannot regress the pipeline.
func (s *Store) StartNoteCapture(ctx context.Context, ownerID, noteID string) (*model.Note, error) {
	var n model.Note
	var audioKey, retention *string
	err := s.pool.QueryRow(ctx,
		`UPDATE notes SET status=$1, updated_at=now()
		 WHERE id=$2 AND owner_id=$3 AND deleted_at IS NULL AND status=$4
		 RETURNING id, owner_id, title, status, pinned, started_at, ended_at, partial_transcript, audio_object_key, retention_state, created_at, updated_at, event_id`,
		model.NoteRecording, noteID, ownerID, model.NoteDraft).
		Scan(&n.ID, &n.OwnerID, &n.Title, &n.Status, &n.Pinned, &n.StartedAt, &n.EndedAt, &n.PartialTranscript, &audioKey, &retention, &n.CreatedAt, &n.UpdatedAt, &n.EventID)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := s.GetNote(ctx, ownerID, noteID)
		if getErr != nil {
			return nil, getErr
		}
		return &existing, nil
	}
	if err != nil {
		return nil, err
	}
	if audioKey != nil {
		n.AudioObjectKey = *audioKey
	}
	if retention != nil {
		n.RetentionState = *retention
	}
	return &n, nil
}

func (s *Store) GetNote(ctx context.Context, ownerID, noteID string) (model.Note, error) {
	var n model.Note
	var audioKey, retention *string
	err := s.pool.QueryRow(ctx, getNoteSelectQuery, noteID, ownerID).
		Scan(&n.ID, &n.OwnerID, &n.Title, &n.Status, &n.Pinned, &n.StartedAt, &n.EndedAt, &n.PartialTranscript, &audioKey, &retention, &n.CreatedAt, &n.UpdatedAt, &n.EventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Note{}, ErrNotFound
	}
	if audioKey != nil {
		n.AudioObjectKey = *audioKey
	}
	if retention != nil {
		n.RetentionState = *retention
	}
	return n, err
}

// SetNoteAudio records the uploaded audio key and advances status to uploaded.
func (s *Store) SetNoteAudio(ctx context.Context, ownerID, noteID, audioKey string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE notes SET audio_object_key=$1, status=$2, updated_at=now()
		 WHERE id=$3 AND owner_id=$4`,
		audioKey, model.NoteUploaded, noteID, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateNoteTitle sets the note's title, owner-scoped.
func (s *Store) UpdateNoteTitle(ctx context.Context, ownerID, noteID, title string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE notes SET title=$1, updated_at=now()
		 WHERE id=$2 AND owner_id=$3`,
		title, noteID, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateNoteBody sets the note's body content, owner-scoped.
func (s *Store) UpdateNoteBody(ctx context.Context, ownerID, noteID, content string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE note_bodies SET content=$1
		 WHERE note_id=$2 AND note_id IN (SELECT id FROM notes WHERE owner_id=$3)`,
		content, noteID, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// FindNoteByTitleCI returns the owner's live note whose title matches title
// case-insensitively. If there is no match or more than one match, ErrNotFound
// is returned.
func (s *Store) FindNoteByTitleCI(ctx context.Context, ownerID, title string) (model.Note, error) {
	if strings.TrimSpace(title) == "" {
		return model.Note{}, ErrNotFound
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, owner_id, title, status, pinned, started_at, ended_at, partial_transcript, audio_object_key, retention_state, created_at, updated_at, event_id
		 FROM notes
		 WHERE owner_id=$1
		   AND deleted_at IS NULL
		   AND lower(title)=lower($2)
		 ORDER BY id
		 LIMIT 2`,
		ownerID, title)
	if err != nil {
		return model.Note{}, err
	}
	defer rows.Close()

	var matches []model.Note
	for rows.Next() {
		var n model.Note
		var audioKey, retention *string
		if err := rows.Scan(&n.ID, &n.OwnerID, &n.Title, &n.Status, &n.Pinned, &n.StartedAt, &n.EndedAt, &n.PartialTranscript, &audioKey, &retention, &n.CreatedAt, &n.UpdatedAt, &n.EventID); err != nil {
			return model.Note{}, err
		}
		if audioKey != nil {
			n.AudioObjectKey = *audioKey
		}
		if retention != nil {
			n.RetentionState = *retention
		}
		matches = append(matches, n)
	}
	if err := rows.Err(); err != nil {
		return model.Note{}, err
	}
	if len(matches) != 1 {
		return model.Note{}, ErrNotFound
	}
	return matches[0], nil
}

// SetNotePinned updates the note's pinned flag, owner-scoped.
func (s *Store) SetNotePinned(ctx context.Context, ownerID, noteID string, pinned bool) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE notes SET pinned=$1, updated_at=now()
		 WHERE id=$2 AND owner_id=$3`,
		pinned, noteID, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetNoteEvent links a note to a calendar event, owner-scoped. The event must
// belong to the same owner (verified via calendar_events); returns ErrNotFound
// if the event is missing/not the owner's, or if the note is missing/not the
// owner's.
func (s *Store) SetNoteEvent(ctx context.Context, ownerID, noteID, eventID string) error {
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM calendar_events WHERE id=$1 AND owner_id=$2)`,
		eventID, ownerID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	ct, err := s.pool.Exec(ctx,
		`UPDATE notes SET event_id=$1, updated_at=now()
		 WHERE id=$2 AND owner_id=$3`,
		eventID, noteID, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ClearNoteEvent unlinks a note's calendar event (sets event_id to NULL),
// owner-scoped. Returns ErrNotFound if the note is missing/not the owner's.
func (s *Store) ClearNoteEvent(ctx context.Context, ownerID, noteID string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE notes SET event_id=NULL, updated_at=now()
		 WHERE id=$1 AND owner_id=$2`,
		noteID, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetNoteBody returns the note's body content, owner-scoped.
func (s *Store) GetNoteBody(ctx context.Context, ownerID, noteID string) (string, error) {
	var content string
	err := s.pool.QueryRow(ctx,
		`SELECT nb.content FROM note_bodies nb JOIN notes n ON n.id = nb.note_id
		 WHERE nb.note_id=$1 AND n.owner_id=$2`, noteID, ownerID).Scan(&content)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return content, err
}

// SetNoteStatus updates a note's status (worker-side; no owner scoping because
// the worker acts on behalf of the system, not a request user).
func (s *Store) SetNoteStatus(ctx context.Context, noteID, status string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE notes SET status=$1, updated_at=now() WHERE id=$2`, status, noteID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ClaimNoteForTranscription marks the note transcribing, records which job did
// so, and returns the status it freshly displaced — all in one statement, so
// no other writer can slip between the read and the write.
//
// A re-claim by the SAME jobID (transcribing_job_id already equals it — a
// retry of a job that claimed once, then failed before saving) does NOT
// overwrite status: it is already "transcribing" from the original claim, and
// re-reading it here would hand back "transcribing" as the "prior" status,
// which a later release would then restore — permanently losing whatever the
// note's real status was before this job's FIRST claim (e.g. "ready"). On a
// re-claim this returns "" (no note status is ever the empty string) so the
// caller knows to fall back to the prior status it captured and persisted on
// the original claim, rather than trusting this call's answer.
//
// The prior status comes from a FOR UPDATE row lock in the UPDATE's own FROM
// clause, not a "WITH prev AS (... FOR UPDATE) UPDATE ... RETURNING (SELECT
// status FROM prev)" CTE. That form looks equivalent but is not: verified
// against Postgres 16, RETURNING (SELECT status FROM prev) comes back NULL
// even though prev's FOR UPDATE lock succeeds and the UPDATE itself affects
// the row. The FROM-subquery form gives the same atomic before/after read and
// returns the correct value.
func (s *Store) ClaimNoteForTranscription(ctx context.Context, noteID, jobID string) (string, error) {
	var prior *string
	err := s.pool.QueryRow(ctx,
		`UPDATE notes n
		 SET status = CASE WHEN cur.transcribing_job_id = $3 THEN n.status ELSE $2 END,
		     transcribing_job_id = $3,
		     updated_at = now()
		 FROM (SELECT status, transcribing_job_id FROM notes WHERE id=$1 FOR UPDATE) AS cur
		 WHERE n.id=$1
		 RETURNING CASE WHEN cur.transcribing_job_id = $3 THEN NULL ELSE cur.status END`,
		noteID, model.NoteTranscribing, jobID).Scan(&prior)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if prior == nil {
		return "", nil
	}
	return *prior, nil
}

// ReleaseNoteTranscriptionClaim undoes ClaimNoteForTranscription, but only while
// this job's own claim still stands. If any other writer has moved the note or
// claimed it since, the newer state wins and this is a no-op.
func (s *Store) ReleaseNoteTranscriptionClaim(ctx context.Context, noteID, jobID, prior string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE notes SET status=$1, transcribing_job_id=NULL, updated_at=now()
		  WHERE id=$2 AND status=$3 AND transcribing_job_id=$4`,
		prior, noteID, model.NoteTranscribing, jobID)
	return err
}

// MarkNoteReady atomically transitions a note to ready and reports whether
// THIS call performed the transition (won=true) via a single
// UPDATE ... WHERE status <> 'ready' RowsAffected check. This is the only
// correct way for a caller to detect "I performed the ready transition" -
// deliberately not a separate read-then-write, which would be racy under
// concurrent finalize calls (two callers could both read a non-ready status
// and both proceed). Worker-side; not owner-scoped. A missing note also
// yields won=false, err=nil (RowsAffected==0), which callers treat as a no-op.
func (s *Store) MarkNoteReady(ctx context.Context, noteID string) (bool, error) {
	ct, err := s.pool.Exec(ctx,
		`UPDATE notes SET status=$1, updated_at=now() WHERE id=$2 AND status <> $1`,
		model.NoteReady, noteID)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() == 1, nil
}

// SetNoteHashes persists the audio content hashes on a note. Called from the
// worker (no owner scope). normalizedAudioHash may be empty (stored as NULL).
func (s *Store) SetNoteHashes(ctx context.Context, noteID, audioHash, normalizedAudioHash string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE notes
		 SET audio_hash=$2, normalized_audio_hash=NULLIF($3, ''), updated_at=now()
		 WHERE id=$1`,
		noteID, audioHash, normalizedAudioHash)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetNoteHashesForGeneration persists audio hashes only while expectedGeneration
// is still the note's current transcript generation. A superseded generation is
// a successful no-op.
func (s *Store) SetNoteHashesForGeneration(ctx context.Context, noteID, audioHash, normalizedAudioHash string, expectedGeneration int) (bool, error) {
	ct, err := s.pool.Exec(ctx,
		`UPDATE notes n
		 SET audio_hash=$2, normalized_audio_hash=NULLIF($3, ''), updated_at=now()
		 WHERE n.id=$1 AND EXISTS (
			SELECT 1 FROM transcripts t WHERE t.note_id=n.id AND t.generation=$4
		 )`,
		noteID, audioHash, normalizedAudioHash, expectedGeneration)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() == 1, nil
}

// SetNoteHashesForOwner persists audio hashes on an owner's note. It mirrors
// SetNoteHashes but keeps the write owner-scoped for request handlers.
func (s *Store) SetNoteHashesForOwner(ctx context.Context, ownerID, noteID, audioHash, normalizedAudioHash string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE notes
		 SET audio_hash=$3, normalized_audio_hash=NULLIF($4, ''), updated_at=now()
		 WHERE id=$1 AND owner_id=$2`,
		noteID, ownerID, audioHash, normalizedAudioHash)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetNoteByID fetches a note without owner scoping (worker use). The returned
// Note includes AudioObjectKey and RetentionState for the pipeline.
func (s *Store) GetNoteByID(ctx context.Context, noteID string) (model.Note, error) {
	var n model.Note
	var audioKey, retention *string
	err := s.pool.QueryRow(ctx,
		`SELECT id, owner_id, title, status, pinned, started_at, ended_at,
		        audio_object_key, retention_state, partial_transcript, created_at, updated_at, event_id
		 FROM notes WHERE id=$1`, noteID).
		Scan(&n.ID, &n.OwnerID, &n.Title, &n.Status, &n.Pinned, &n.StartedAt, &n.EndedAt,
			&audioKey, &retention, &n.PartialTranscript, &n.CreatedAt, &n.UpdatedAt, &n.EventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Note{}, ErrNotFound
	}
	if err != nil {
		return model.Note{}, err
	}
	if audioKey != nil {
		n.AudioObjectKey = *audioKey
	}
	if retention != nil {
		n.RetentionState = *retention
	}
	return n, nil
}

// NoteIsTrashed reports whether a note is currently soft-deleted (worker use, not
// owner-scoped). A missing note is treated as trashed (true) so the worker skips it.
func (s *Store) NoteIsTrashed(ctx context.Context, noteID string) (bool, error) {
	var trashed bool
	err := s.pool.QueryRow(ctx,
		`SELECT deleted_at IS NOT NULL FROM notes WHERE id=$1`, noteID).Scan(&trashed)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return trashed, nil
}

// DeleteNote moves an owner's note to the trash (soft delete). The audio blob and
// children are kept until permanent delete or auto-purge. ErrNotFound if absent or
// already trashed.
func (s *Store) DeleteNote(ctx context.Context, ownerID, id string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE notes SET deleted_at=now(), updated_at=now()
		 WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL`,
		id, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetRetentionState records the audio's post-transcription disposition.
func (s *Store) SetRetentionState(ctx context.Context, noteID, state string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE notes SET retention_state=$1, updated_at=now() WHERE id=$2`, state, noteID)
	return err
}

// SetRetentionStateForGeneration records retention only while expectedGeneration
// is current. The bool reports whether the guarded update was applied.
func (s *Store) SetRetentionStateForGeneration(ctx context.Context, noteID, state string, expectedGeneration int) (bool, error) {
	ct, err := s.pool.Exec(ctx,
		`UPDATE notes n SET retention_state=$1, updated_at=now()
		 WHERE n.id=$2 AND EXISTS (
			SELECT 1 FROM transcripts t WHERE t.note_id=n.id AND t.generation=$3
		 )`, state, noteID, expectedGeneration)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() == 1, nil
}

// NoteBody returns the user's live-typed Markdown for a note.
func (s *Store) NoteBody(ctx context.Context, noteID string) (string, error) {
	var body string
	err := s.pool.QueryRow(ctx, `SELECT content FROM note_bodies WHERE note_id=$1`, noteID).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return body, err
}

// ListNotes returns live (non-deleted) notes for ownerID, optionally filtered by
// the fields in f. All active filters are AND-ed. An unknown tag/status/folder
// returns an empty slice rather than an error.
func (s *Store) ListNotes(ctx context.Context, ownerID string, f ListNotesFilter) ([]model.Note, error) {
	joinFolder, where, args := noteFilterSQL(ownerID, f)

	query := fmt.Sprintf(
		`SELECT n.id, n.owner_id, n.title, n.status, n.pinned, n.started_at, n.ended_at,
		        n.partial_transcript, n.created_at, n.updated_at, COALESCE(nb.content, ''), n.event_id
		 FROM notes n
		 %s
		 LEFT JOIN note_bodies nb ON nb.note_id = n.id
		 WHERE %s ORDER BY %s`,
		joinFolder, where,
		notesOrderClause(f.FolderIDSet))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Note
	for rows.Next() {
		var n model.Note
		var body string
		if err := rows.Scan(&n.ID, &n.OwnerID, &n.Title, &n.Status, &n.Pinned,
			&n.StartedAt, &n.EndedAt, &n.PartialTranscript, &n.CreatedAt, &n.UpdatedAt, &body, &n.EventID); err != nil {
			return nil, err
		}
		n.Snippet = snippet(body)
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ids := make([]string, len(out))
	for i := range out {
		ids[i] = out[i].ID
	}
	tagMap, err := s.tagsForNotes(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if tags := tagMap[out[i].ID]; tags != nil {
			out[i].Tags = tags
		} else {
			out[i].Tags = []string{}
		}
	}
	folderMap, err := s.foldersForNotes(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if fids := folderMap[out[i].ID]; fids != nil {
			out[i].FolderIDs = fids
		} else {
			out[i].FolderIDs = []string{}
		}
	}
	return out, nil
}

// ListTrash returns the owner's trashed notes, most-recently-trashed first, with the
// same snippet/tags/folder fields as ListNotes.
func (s *Store) ListTrash(ctx context.Context, ownerID string) ([]model.Note, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT n.id, n.owner_id, n.title, n.status, n.pinned, n.started_at, n.ended_at,
		        n.partial_transcript, n.created_at, n.updated_at, COALESCE(nb.content, ''), n.deleted_at, n.event_id
		 FROM notes n
		 LEFT JOIN note_bodies nb ON nb.note_id = n.id
		 WHERE n.owner_id=$1 AND n.deleted_at IS NOT NULL ORDER BY n.deleted_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Note
	for rows.Next() {
		var n model.Note
		var body string
		if err := rows.Scan(&n.ID, &n.OwnerID, &n.Title, &n.Status, &n.Pinned,
			&n.StartedAt, &n.EndedAt, &n.PartialTranscript, &n.CreatedAt, &n.UpdatedAt, &body, &n.DeletedAt, &n.EventID); err != nil {
			return nil, err
		}
		n.Snippet = snippet(body)
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ids := make([]string, len(out))
	for i := range out {
		ids[i] = out[i].ID
	}
	tagMap, err := s.tagsForNotes(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if tags := tagMap[out[i].ID]; tags != nil {
			out[i].Tags = tags
		} else {
			out[i].Tags = []string{}
		}
	}
	folderMap, err := s.foldersForNotes(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if fids := folderMap[out[i].ID]; fids != nil {
			out[i].FolderIDs = fids
		} else {
			out[i].FolderIDs = []string{}
		}
	}
	return out, nil
}

// RestoreNote clears deleted_at, moving a trashed note back to the live set.
// ErrNotFound if absent or not currently trashed.
func (s *Store) RestoreNote(ctx context.Context, ownerID, id string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE notes SET deleted_at=NULL, updated_at=now()
		 WHERE id=$1 AND owner_id=$2 AND deleted_at IS NOT NULL`,
		id, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PurgeNote permanently removes a trashed note (children cascade via FKs), returning its
// audio object key (may be ""). ErrNotFound if absent or not trashed.
func (s *Store) PurgeNote(ctx context.Context, ownerID, id string) (string, error) {
	var audioKey string
	// note_embeddings rows are cleaned up automatically by the ON DELETE CASCADE
	// FK on notes(id) - no explicit DELETE FROM note_embeddings is needed here.
	err := s.pool.QueryRow(ctx,
		`DELETE FROM notes WHERE id=$1 AND owner_id=$2 AND deleted_at IS NOT NULL
		 RETURNING COALESCE(audio_object_key,'')`,
		id, ownerID).Scan(&audioKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return audioKey, nil
}

// PurgeExpired permanently removes every note trashed longer than olderThan ago (across
// ALL owners - it is the auto-purge job, not owner-scoped) and returns the audio object
// keys of everything purged.
func (s *Store) PurgeExpired(ctx context.Context, olderThan time.Duration) ([]string, error) {
	interval := fmt.Sprintf("%d seconds", int64(olderThan.Seconds()))
	rows, err := s.pool.Query(ctx,
		`DELETE FROM notes
		 WHERE deleted_at IS NOT NULL AND deleted_at < now() - $1::interval
		 RETURNING COALESCE(audio_object_key,'')`, interval)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		if key != "" {
			keys = append(keys, key)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}

// snippet turns Markdown body text into a short, single-line preview: strips
// leading heading/list marks and collapses lines, capped at 160 runes.
func snippet(body string) string {
	fields := strings.FieldsFunc(body, func(r rune) bool { return r == '\n' || r == '\r' })
	var b strings.Builder
	for _, line := range fields {
		line = strings.TrimSpace(strings.TrimLeft(line, "#> -*"))
		// Strip ordered-list prefix: one or more digits followed by ". " (or
		// just "." at end-of-string when the body was all whitespace and
		// TrimSpace already consumed the trailing space).
		{
			i := 0
			for i < len(line) && line[i] >= '0' && line[i] <= '9' {
				i++
			}
			if i > 0 && i < len(line) && line[i] == '.' {
				if i+1 >= len(line) {
					// "2." with nothing after - body is empty.
					line = ""
				} else if line[i+1] == ' ' {
					line = strings.TrimSpace(line[i+2:])
				}
			}
		}
		if line == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(line)
		if len([]rune(b.String())) >= 160 {
			break
		}
	}
	r := []rune(b.String())
	if len(r) > 160 {
		return string(r[:160])
	}
	return string(r)
}

// GetNoteAdmin fetches a note by ID without owner scoping (admin use).
// Returns ErrNotFound if the note is absent or soft-deleted.
func (s *Store) GetNoteAdmin(ctx context.Context, noteID string) (model.Note, error) {
	var n model.Note
	err := s.pool.QueryRow(ctx,
		`SELECT id, status FROM notes WHERE id=$1 AND deleted_at IS NULL`, noteID).
		Scan(&n.ID, &n.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Note{}, ErrNotFound
	}
	return n, err
}

// FindNotesByAudioHash returns live notes for the given owner whose audio_hash
// or normalized_audio_hash match the provided values. This method enforces
// that at least one argument is non-empty. Returns an empty slice, not an
// error, when there are no matches.
func (s *Store) FindNotesByAudioHash(ctx context.Context, ownerID, audioHash, normalizedAudioHash string) ([]model.Note, error) {
	if audioHash == "" && normalizedAudioHash == "" {
		return []model.Note{}, nil
	}

	where := []string{"owner_id=$1", "deleted_at IS NULL"}
	args := []any{ownerID}

	var hashConds []string
	if audioHash != "" {
		args = append(args, audioHash)
		hashConds = append(hashConds, fmt.Sprintf("audio_hash=$%d", len(args)))
	}
	if normalizedAudioHash != "" {
		args = append(args, normalizedAudioHash)
		hashConds = append(hashConds, fmt.Sprintf("normalized_audio_hash=$%d", len(args)))
	}
	if len(hashConds) > 0 {
		where = append(where, "("+strings.Join(hashConds, " OR ")+")")
	}

	query := fmt.Sprintf(
		`SELECT id, title, status, created_at FROM notes WHERE %s`,
		strings.Join(where, " AND "))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Note
	for rows.Next() {
		var n model.Note
		if err := rows.Scan(&n.ID, &n.Title, &n.Status, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []model.Note{}
	}
	return out, nil
}

// SetNotePartialTranscript sets or clears the partial_transcript flag on a note
// (worker use; not owner-scoped). Called by the pipeline after a partial transcription.
func (s *Store) SetNotePartialTranscript(ctx context.Context, noteID string, partial bool) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE notes SET partial_transcript=$1, updated_at=now() WHERE id=$2`,
		partial, noteID)
	return err
}

// SetNotePartialTranscriptForGeneration updates the partial flag only while
// expectedGeneration is current. A superseded generation is a successful no-op.
func (s *Store) SetNotePartialTranscriptForGeneration(ctx context.Context, noteID string, partial bool, expectedGeneration int) (bool, error) {
	ct, err := s.pool.Exec(ctx,
		`UPDATE notes n SET partial_transcript=$1, updated_at=now()
		 WHERE n.id=$2 AND EXISTS (
			SELECT 1 FROM transcripts t WHERE t.note_id=n.id AND t.generation=$3
		 )`, partial, noteID, expectedGeneration)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() == 1, nil
}
