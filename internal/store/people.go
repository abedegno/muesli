package store

import (
	"context"
	"errors"
	"strings"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type CompanyWithPeopleCount struct {
	model.Company
	PeopleCount int64
}

func (s *Store) UpsertCompany(ctx context.Context, ownerID, domain, name string) (model.Company, error) {
	id := uuid.NewString()
	var company model.Company
	err := s.pool.QueryRow(ctx,
		`INSERT INTO companies (id, owner_id, domain, name)
		 VALUES ($1,$2,$3,$4)
		 ON CONFLICT (owner_id, domain) DO UPDATE
		 SET name = COALESCE(NULLIF(EXCLUDED.name,''), companies.name),
		     updated_at = now()
		 RETURNING id, owner_id, domain, name, created_at, updated_at`,
		id, ownerID, domain, name).
		Scan(&company.ID, &company.OwnerID, &company.Domain, &company.Name, &company.CreatedAt, &company.UpdatedAt)
	if err != nil {
		return model.Company{}, err
	}
	return company, nil
}

func (s *Store) GetCompany(ctx context.Context, ownerID, id string) (model.Company, error) {
	var company model.Company
	err := s.pool.QueryRow(ctx,
		`SELECT id, owner_id, domain, name, created_at, updated_at
		 FROM companies
		 WHERE id=$1 AND owner_id=$2`, id, ownerID).
		Scan(&company.ID, &company.OwnerID, &company.Domain, &company.Name, &company.CreatedAt, &company.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Company{}, ErrNotFound
	}
	return company, err
}

// MergeCompanies moves all people from one company to another, then removes
// the source company. Both companies must belong to ownerID.
func (s *Store) MergeCompanies(ctx context.Context, ownerID, fromID, intoID string) error {
	if fromID == intoID {
		return ErrInvalidMerge
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var fromOwned, intoOwned bool
	if err := tx.QueryRow(ctx,
		`SELECT
		   EXISTS(SELECT 1 FROM companies WHERE id=$1 AND owner_id=$2),
		   EXISTS(SELECT 1 FROM companies WHERE id=$3 AND owner_id=$2)`,
		fromID, ownerID, intoID).Scan(&fromOwned, &intoOwned); err != nil {
		return err
	}
	if !fromOwned || !intoOwned {
		return ErrNotFound
	}

	if _, err := tx.Exec(ctx,
		`UPDATE people
		 SET company_id=$1, updated_at=now()
		 WHERE owner_id=$2 AND company_id=$3`,
		intoID, ownerID, fromID); err != nil {
		return err
	}

	ct, err := tx.Exec(ctx,
		`DELETE FROM companies
		 WHERE id=$1 AND owner_id=$2`,
		fromID, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}

	return tx.Commit(ctx)
}

func (s *Store) ListCompanies(ctx context.Context, ownerID string) ([]model.Company, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, owner_id, domain, name, created_at, updated_at
		 FROM companies
		 WHERE owner_id=$1
		 ORDER BY domain`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Company{}
	for rows.Next() {
		var company model.Company
		if err := rows.Scan(&company.ID, &company.OwnerID, &company.Domain, &company.Name, &company.CreatedAt, &company.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, company)
	}
	return out, rows.Err()
}

func (s *Store) ListCompaniesWithPeopleCount(ctx context.Context, ownerID, q string) ([]CompanyWithPeopleCount, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if q == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT c.id, c.owner_id, c.domain, c.name, c.created_at, c.updated_at, COUNT(p.id) AS people_count
			 FROM companies c
			 LEFT JOIN people p ON p.company_id = c.id AND p.owner_id = c.owner_id
			 WHERE c.owner_id=$1
			 GROUP BY c.id, c.owner_id, c.domain, c.name, c.created_at, c.updated_at
			 ORDER BY c.domain`, ownerID)
	} else {
		pattern := "%" + q + "%"
		rows, err = s.pool.Query(ctx,
			`SELECT c.id, c.owner_id, c.domain, c.name, c.created_at, c.updated_at, COUNT(p.id) AS people_count
			 FROM companies c
			 LEFT JOIN people p ON p.company_id = c.id AND p.owner_id = c.owner_id
			 WHERE c.owner_id=$1
			   AND (c.name ILIKE $2 OR c.domain ILIKE $2)
			 GROUP BY c.id, c.owner_id, c.domain, c.name, c.created_at, c.updated_at
			 ORDER BY c.domain`, ownerID, pattern)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []CompanyWithPeopleCount{}
	for rows.Next() {
		var company CompanyWithPeopleCount
		if err := rows.Scan(&company.ID, &company.OwnerID, &company.Domain, &company.Name, &company.CreatedAt, &company.UpdatedAt, &company.PeopleCount); err != nil {
			return nil, err
		}
		out = append(out, company)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []CompanyWithPeopleCount{}
	}
	return out, nil
}

func (s *Store) ListPeopleByCompany(ctx context.Context, ownerID, companyID string) ([]model.Person, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, owner_id, primary_email, display_name, company_id, first_seen_at, updated_at
		 FROM people
		 WHERE owner_id=$1 AND company_id=$2
		 ORDER BY display_name, primary_email`, ownerID, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Person{}
	for rows.Next() {
		var person model.Person
		if err := rows.Scan(&person.ID, &person.OwnerID, &person.PrimaryEmail, &person.DisplayName, &person.CompanyID, &person.FirstSeenAt, &person.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, person)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []model.Person{}
	}
	return out, nil
}

func (s *Store) UpsertPerson(ctx context.Context, ownerID, email, name string, companyID *string) (model.Person, error) {
	id := uuid.NewString()
	email = strings.ToLower(strings.TrimSpace(email))

	var person model.Person
	err := s.pool.QueryRow(ctx,
		`INSERT INTO people (id, owner_id, primary_email, display_name, company_id)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (owner_id, primary_email) DO UPDATE
		 SET display_name = COALESCE(NULLIF(EXCLUDED.display_name,''), people.display_name),
		     company_id = COALESCE(EXCLUDED.company_id, people.company_id),
		     updated_at = now()
		 RETURNING id, owner_id, primary_email, display_name, company_id, first_seen_at, updated_at`,
		id, ownerID, email, name, companyID).
		Scan(&person.ID, &person.OwnerID, &person.PrimaryEmail, &person.DisplayName, &person.CompanyID, &person.FirstSeenAt, &person.UpdatedAt)
	if err != nil {
		return model.Person{}, err
	}
	return person, nil
}

func (s *Store) UpdatePerson(ctx context.Context, ownerID, id string, displayName *string, companyID *string, clearCompany bool) (model.Person, error) {
	if displayName == nil && companyID == nil && !clearCompany {
		return s.GetPerson(ctx, ownerID, id)
	}

	if companyID != nil {
		var exists bool
		err := s.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM companies WHERE id=$1 AND owner_id=$2)`,
			*companyID, ownerID).Scan(&exists)
		if err != nil {
			return model.Person{}, err
		}
		if !exists {
			return model.Person{}, ErrNotFound
		}
	}

	updateQuery := `UPDATE people
		SET display_name = COALESCE($3, display_name),
		    updated_at = now()
		WHERE id=$1 AND owner_id=$2
		RETURNING id, owner_id, primary_email, display_name, company_id, first_seen_at, updated_at`
	updateArgs := []any{id, ownerID, displayName}
	if clearCompany {
		updateQuery = `UPDATE people
			SET display_name = COALESCE($3, display_name),
			    company_id = NULL,
			    updated_at = now()
			WHERE id=$1 AND owner_id=$2
			RETURNING id, owner_id, primary_email, display_name, company_id, first_seen_at, updated_at`
	} else if companyID != nil {
		updateQuery = `UPDATE people
			SET display_name = COALESCE($3, display_name),
			    company_id = $4::uuid,
			    updated_at = now()
			WHERE id=$1 AND owner_id=$2
			RETURNING id, owner_id, primary_email, display_name, company_id, first_seen_at, updated_at`
		updateArgs = append(updateArgs, *companyID)
	}

	var person model.Person
	err := s.pool.QueryRow(ctx, updateQuery, updateArgs...).
		Scan(&person.ID, &person.OwnerID, &person.PrimaryEmail, &person.DisplayName, &person.CompanyID, &person.FirstSeenAt, &person.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Person{}, ErrNotFound
	}
	return person, err
}

func (s *Store) MergePeople(ctx context.Context, ownerID, fromID, intoID string) (model.Person, error) {
	if fromID == intoID {
		return model.Person{}, ErrInvalidMerge
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Person{}, err
	}
	defer tx.Rollback(ctx)

	var fromOwned, intoOwned bool
	if err := tx.QueryRow(ctx,
		`SELECT
		   EXISTS(SELECT 1 FROM people WHERE id=$1 AND owner_id=$2),
		   EXISTS(SELECT 1 FROM people WHERE id=$3 AND owner_id=$2)`,
		fromID, ownerID, intoID).Scan(&fromOwned, &intoOwned); err != nil {
		return model.Person{}, err
	}
	if !fromOwned || !intoOwned {
		return model.Person{}, ErrNotFound
	}

	if _, err := tx.Exec(ctx,
		`UPDATE note_speaker_aliases
		 SET person_id=$1
		 WHERE owner_id=$2 AND person_id=$3`,
		intoID, ownerID, fromID); err != nil {
		return model.Person{}, err
	}

	ct, err := tx.Exec(ctx,
		`DELETE FROM people
		 WHERE id=$1 AND owner_id=$2`,
		fromID, ownerID)
	if err != nil {
		return model.Person{}, err
	}
	if ct.RowsAffected() == 0 {
		return model.Person{}, ErrNotFound
	}

	var person model.Person
	if err := tx.QueryRow(ctx,
		`SELECT id, owner_id, primary_email, display_name, company_id, first_seen_at, updated_at
		 FROM people
		 WHERE id=$1 AND owner_id=$2`,
		intoID, ownerID).
		Scan(&person.ID, &person.OwnerID, &person.PrimaryEmail, &person.DisplayName, &person.CompanyID, &person.FirstSeenAt, &person.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Person{}, ErrNotFound
		}
		return model.Person{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return model.Person{}, err
	}
	return person, nil
}

func (s *Store) DeletePerson(ctx context.Context, ownerID, id string) error {
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM people
		 WHERE id=$1 AND owner_id=$2`,
		id, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListPeople(ctx context.Context, ownerID, q string) ([]model.Person, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if q == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT id, owner_id, primary_email, display_name, company_id, first_seen_at, updated_at
			 FROM people
			 WHERE owner_id=$1
			 ORDER BY display_name, primary_email`, ownerID)
	} else {
		pattern := "%" + q + "%"
		rows, err = s.pool.Query(ctx,
			`SELECT id, owner_id, primary_email, display_name, company_id, first_seen_at, updated_at
			 FROM people
			 WHERE owner_id=$1
			   AND (display_name ILIKE $2 OR primary_email ILIKE $2)
			 ORDER BY display_name, primary_email`, ownerID, pattern)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Person{}
	for rows.Next() {
		var person model.Person
		if err := rows.Scan(&person.ID, &person.OwnerID, &person.PrimaryEmail, &person.DisplayName, &person.CompanyID, &person.FirstSeenAt, &person.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, person)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []model.Person{}
	}
	return out, nil
}

// PeopleForNoteEvent returns the people whose primary_email matches a note's
// linked calendar event attendees, scoped to the note owner. It returns an
// empty (non-nil) slice when the note has no event, the event has no attendees,
// or no attendee emails resolve to people.
func (s *Store) PeopleForNoteEvent(ctx context.Context, ownerID, noteID string) ([]model.Person, error) {
	var eventID *string
	err := s.pool.QueryRow(ctx,
		`SELECT event_id
		 FROM notes
		 WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL`,
		noteID, ownerID).Scan(&eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if eventID == nil {
		return []model.Person{}, nil
	}

	var attendeesRaw string
	err = s.pool.QueryRow(ctx,
		`SELECT attendees::text
		 FROM calendar_events
		 WHERE id=$1 AND owner_id=$2`,
		*eventID, ownerID).Scan(&attendeesRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	attendees, err := decodeCalendarAttendees(attendeesRaw)
	if err != nil {
		return nil, err
	}
	emails := make([]string, 0, len(attendees))
	seen := make(map[string]struct{}, len(attendees))
	for _, attendee := range attendees {
		email := strings.ToLower(strings.TrimSpace(attendee.Email))
		if email == "" {
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		emails = append(emails, email)
	}
	if len(emails) == 0 {
		return []model.Person{}, nil
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, owner_id, primary_email, display_name, company_id, first_seen_at, updated_at
		 FROM people
		 WHERE owner_id=$1
		   AND lower(primary_email) = ANY($2::text[])
		 ORDER BY display_name, primary_email`,
		ownerID, emails)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Person{}
	for rows.Next() {
		var person model.Person
		if err := rows.Scan(&person.ID, &person.OwnerID, &person.PrimaryEmail, &person.DisplayName, &person.CompanyID, &person.FirstSeenAt, &person.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, person)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []model.Person{}
	}
	return out, nil
}

// NotesForPerson returns the live notes for ownerID where the person appears
// either via a linked calendar event attendee email or via speaker aliases.
// It always returns a non-nil slice.
func (s *Store) NotesForPerson(ctx context.Context, ownerID, personID string) ([]model.Note, error) {
	var primaryEmail string
	err := s.pool.QueryRow(ctx,
		`SELECT primary_email
		 FROM people
		 WHERE id=$1 AND owner_id=$2`,
		personID, ownerID).Scan(&primaryEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	email := strings.ToLower(strings.TrimSpace(primaryEmail))
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT n.id, n.owner_id, n.title, n.status, n.pinned, n.started_at, n.ended_at,
		        n.partial_transcript, n.created_at, n.updated_at, COALESCE(nb.content, ''), n.event_id
		 FROM notes n
		 LEFT JOIN note_bodies nb ON nb.note_id = n.id
		 WHERE n.owner_id = $1
		   AND n.deleted_at IS NULL
		   AND (
		     EXISTS (
		       SELECT 1
		       FROM calendar_events ce
		       WHERE ce.id = n.event_id
		         AND ce.owner_id = n.owner_id
		         AND EXISTS (
		           SELECT 1
		           FROM jsonb_array_elements(COALESCE(ce.attendees, '[]'::jsonb)) attendee
		           WHERE lower(attendee->>'email') = $2
		         )
		     )
		     OR EXISTS (
		       SELECT 1
		       FROM note_speaker_aliases nsa
		       WHERE nsa.note_id = n.id
		         AND nsa.owner_id = $1
		         AND nsa.person_id = $3
		     )
		   )
		 ORDER BY n.created_at DESC`,
		ownerID, email, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Note{}
	for rows.Next() {
		var n model.Note
		var body string
		if err := rows.Scan(&n.ID, &n.OwnerID, &n.Title, &n.Status, &n.Pinned, &n.StartedAt, &n.EndedAt, &n.PartialTranscript, &n.CreatedAt, &n.UpdatedAt, &body, &n.EventID); err != nil {
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
	if out == nil {
		out = []model.Note{}
	}
	return out, nil
}

func (s *Store) PeopleForNoteSpeakers(ctx context.Context, ownerID, noteID string) ([]model.Person, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT p.id, p.owner_id, p.primary_email, p.display_name, p.company_id, p.first_seen_at, p.updated_at
		 FROM people p
		 JOIN note_speaker_aliases nsa ON nsa.person_id = p.id
		 WHERE nsa.owner_id = $1
		   AND nsa.note_id = $2
		   AND p.owner_id = $1
		 ORDER BY p.display_name, p.primary_email`,
		ownerID, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Person{}
	for rows.Next() {
		var person model.Person
		if err := rows.Scan(&person.ID, &person.OwnerID, &person.PrimaryEmail, &person.DisplayName, &person.CompanyID, &person.FirstSeenAt, &person.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, person)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []model.Person{}
	}
	return out, nil
}

func (s *Store) GetPerson(ctx context.Context, ownerID, id string) (model.Person, error) {
	var person model.Person
	err := s.pool.QueryRow(ctx,
		`SELECT id, owner_id, primary_email, display_name, company_id, first_seen_at, updated_at
		 FROM people
		 WHERE id=$1 AND owner_id=$2`, id, ownerID).
		Scan(&person.ID, &person.OwnerID, &person.PrimaryEmail, &person.DisplayName, &person.CompanyID, &person.FirstSeenAt, &person.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Person{}, ErrNotFound
	}
	return person, err
}
