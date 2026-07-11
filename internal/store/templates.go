package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type templateSection struct {
	Heading     string `json:"heading"`
	Instruction string `json:"instruction"`
}

var builtInTemplates = []struct {
	Name     string
	Sections []templateSection
}{
	{
		Name: "General meeting",
		Sections: []templateSection{
			{"Overview", "Summarise the meeting in 2-3 sentences."},
			{"Key points", "List the main discussion points as concise bullets."},
			{"Decisions", "List concrete decisions made, if any."},
		},
	},
	{
		Name: "Action items",
		Sections: []templateSection{
			{"Action items", "List each action item with its owner and any deadline mentioned."},
		},
	},
}

// SeedBuiltInTemplates inserts the built-in (owner_id NULL) templates if absent. Idempotent.
func (s *Store) SeedBuiltInTemplates(ctx context.Context) error {
	for _, t := range builtInTemplates {
		sections, err := json.Marshal(t.Sections)
		if err != nil {
			return err
		}
		_, err = s.pool.Exec(ctx,
			`INSERT INTO templates (id, owner_id, name, sections)
			 SELECT $1, NULL, $2, $3::jsonb
			 WHERE NOT EXISTS (SELECT 1 FROM templates WHERE owner_id IS NULL AND name=$2)`,
			uuid.NewString(), t.Name, string(sections))
		if err != nil {
			return err
		}
	}
	return nil
}

// BuiltInTemplates returns the seeded built-in templates with parsed sections.
func (s *Store) BuiltInTemplates(ctx context.Context) ([]model.Template, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, sections FROM templates WHERE owner_id IS NULL ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Template
	for rows.Next() {
		var tm model.Template
		var sectionsJSON []byte
		if err := rows.Scan(&tm.ID, &tm.Name, &sectionsJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(sectionsJSON, &tm.Sections); err != nil {
			return nil, err
		}
		out = append(out, tm)
	}
	if out == nil {
		out = []model.Template{}
	}
	return out, rows.Err()
}

func (s *Store) BuiltInTemplateNames(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT name FROM templates WHERE owner_id IS NULL ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func validateTemplate(name string, sections []model.TemplateSection) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return ValidationError("template name invalid")
	}
	if len(sections) < 1 || len(sections) > 12 {
		return ValidationError("template must have 1-12 sections")
	}
	for _, s := range sections {
		h := strings.TrimSpace(s.Heading)
		ins := strings.TrimSpace(s.Instruction)
		if h == "" || len([]rune(h)) > 80 {
			return ValidationError("section heading invalid")
		}
		if ins == "" || len([]rune(ins)) > 500 {
			return ValidationError("section instruction invalid")
		}
	}
	return nil
}

func (s *Store) ListTemplates(ctx context.Context, ownerID string) ([]model.Template, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, sections, (owner_id IS NULL) AS built_in
		   FROM templates WHERE owner_id IS NULL OR owner_id=$1
		   ORDER BY (owner_id IS NULL) DESC, lower(name)`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Template{}
	for rows.Next() {
		var tm model.Template
		var sectionsJSON []byte
		if err := rows.Scan(&tm.ID, &tm.Name, &sectionsJSON, &tm.BuiltIn); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(sectionsJSON, &tm.Sections); err != nil {
			return nil, err
		}
		out = append(out, tm)
	}
	return out, rows.Err()
}

// TemplatesForSummary returns built-ins + the owner's templates for the summarize fan-out.
func (s *Store) TemplatesForSummary(ctx context.Context, ownerID string) ([]model.Template, error) {
	return s.ListTemplates(ctx, ownerID)
}

func (s *Store) nameTaken(ctx context.Context, ownerID, name, excludeID string) (bool, error) {
	var exists bool
	var err error
	if excludeID == "" {
		err = s.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM templates WHERE owner_id=$1 AND lower(name)=lower($2))`,
			ownerID, name).Scan(&exists)
	} else {
		err = s.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM templates WHERE owner_id=$1 AND lower(name)=lower($2) AND id<>$3::uuid)`,
			ownerID, name, excludeID).Scan(&exists)
	}
	return exists, err
}

func (s *Store) CreateTemplate(ctx context.Context, ownerID, name string, sections []model.TemplateSection) (model.Template, error) {
	if err := validateTemplate(name, sections); err != nil {
		return model.Template{}, err
	}
	name = strings.TrimSpace(name)
	if taken, err := s.nameTaken(ctx, ownerID, name, ""); err != nil {
		return model.Template{}, err
	} else if taken {
		return model.Template{}, ErrDuplicate
	}
	secJSON, err := json.Marshal(sections)
	if err != nil {
		return model.Template{}, err
	}
	tm := model.Template{ID: uuid.NewString(), Name: name, Sections: sections, BuiltIn: false}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO templates (id, owner_id, name, sections) VALUES ($1,$2,$3,$4::jsonb)`,
		tm.ID, ownerID, name, string(secJSON))
	return tm, err
}

func (s *Store) UpdateTemplate(ctx context.Context, ownerID, id, name string, sections []model.TemplateSection) error {
	if err := validateTemplate(name, sections); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if taken, err := s.nameTaken(ctx, ownerID, name, id); err != nil {
		return err
	} else if taken {
		return ErrDuplicate
	}
	secJSON, err := json.Marshal(sections)
	if err != nil {
		return err
	}
	ct, err := s.pool.Exec(ctx,
		`UPDATE templates SET name=$1, sections=$2::jsonb WHERE id=$3 AND owner_id=$4`,
		name, string(secJSON), id, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteTemplate(ctx context.Context, ownerID, id string) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM templates WHERE id=$1 AND owner_id=$2`, id, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) NoteOwnerID(ctx context.Context, noteID string) (string, error) {
	var owner string
	err := s.pool.QueryRow(ctx, `SELECT owner_id FROM notes WHERE id=$1`, noteID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return owner, err
}
