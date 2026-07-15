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

const (
	templatePhaseAfter  = "after"
	templatePhasePre    = "pre"
	templatePhaseDuring = "during"
	templatePhaseCross  = "cross"
)

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
	{
		Name: "1:1",
		Sections: []templateSection{
			{"Talking points", "Capture the main topics, personal updates, and anything either person wants to discuss."},
			{"Decisions", "List agreements, commitments, or other decisions made in the conversation."},
			{"Action items", "List tasks assigned to either person, with owners and deadlines if mentioned."},
			{"Follow-ups", "Note open questions, check-ins, or follow-up conversations to revisit later."},
		},
	},
	{
		Name: "Standup",
		Sections: []templateSection{
			{"Yesterday", "Summarize what was completed since the last standup, focusing on shipped work and progress."},
			{"Today", "Summarize what the speaker plans to work on next, including immediate priorities."},
			{"Blockers", "List blockers, risks, dependencies, or anything preventing progress."},
		},
	},
	{
		Name: "Interview",
		Sections: []templateSection{
			{"Candidate summary", "Summarize the candidate's background, experience, and the overall shape of the interview."},
			{"Strengths", "List the strongest signals, relevant skills, and positive evidence from the discussion."},
			{"Concerns", "Note gaps, risks, unclear areas, or follow-up topics that need more evaluation."},
			{"Recommendation", "State the hiring recommendation and brief rationale, grounded in the conversation."},
		},
	},
	{
		Name: "Sales call",
		Sections: []templateSection{
			{"Needs", "Summarize the prospect's goals, pain points, requirements, and buying context."},
			{"Objections", "List objections, hesitation, or blockers raised by the prospect."},
			{"Next steps", "Capture agreed follow-ups, meetings, trials, or information to send."},
			{"Decision-makers", "Identify who is involved in the decision, their roles, and any approval process mentioned."},
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
			`INSERT INTO templates (id, owner_id, name, phase, sections)
			 SELECT $1, NULL, $2, $4, $3::jsonb
			 WHERE NOT EXISTS (SELECT 1 FROM templates WHERE owner_id IS NULL AND name=$2)`,
			uuid.NewString(), t.Name, string(sections), templatePhaseAfter)
		if err != nil {
			return err
		}
	}
	return nil
}

// BuiltInTemplates returns the seeded built-in templates with parsed sections.
func (s *Store) BuiltInTemplates(ctx context.Context) ([]model.Template, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, phase, sections, system_prompt, model, temperature
		   FROM templates WHERE owner_id IS NULL ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Template
	for rows.Next() {
		var tm model.Template
		var sectionsJSON []byte
		var systemPrompt, modelName *string
		var temperature *float64
		if err := rows.Scan(&tm.ID, &tm.Name, &tm.Phase, &sectionsJSON, &systemPrompt, &modelName, &temperature); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(sectionsJSON, &tm.Sections); err != nil {
			return nil, err
		}
		applyTemplateOverrides(&tm, systemPrompt, modelName, temperature)
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

// applyTemplateOverrides copies scanned nullable system_prompt/model/temperature
// columns onto tm. A nil systemPrompt/modelName scans as "" (unset); temperature
// is copied as-is (nil = unset).
func applyTemplateOverrides(tm *model.Template, systemPrompt, modelName *string, temperature *float64) {
	if systemPrompt != nil {
		tm.SystemPrompt = *systemPrompt
	}
	if modelName != nil {
		tm.Model = *modelName
	}
	tm.Temperature = temperature
}

// nullableTemplateStr returns nil (SQL NULL) for a blank string, else the
// trimmed string, for writing optional system_prompt/model columns.
func nullableTemplateStr(v string) any {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return v
}

func validateTemplateOverrides(systemPrompt, modelName string, temperature *float64) error {
	if len([]rune(strings.TrimSpace(systemPrompt))) > 4000 {
		return ValidationError("template system prompt too long")
	}
	if len([]rune(strings.TrimSpace(modelName))) > 200 {
		return ValidationError("template model invalid")
	}
	if temperature != nil && (*temperature < 0 || *temperature > 2) {
		return ValidationError("template temperature must be between 0 and 2")
	}
	return nil
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

func normalizeTemplatePhase(phase string) string {
	if phase == "" {
		return templatePhaseAfter
	}
	return phase
}

func validateTemplatePhase(phase string) error {
	switch phase {
	case templatePhaseAfter, templatePhasePre, templatePhaseDuring, templatePhaseCross:
		return nil
	default:
		return ValidationError("template phase invalid")
	}
}

func (s *Store) ListTemplates(ctx context.Context, ownerID string) ([]model.Template, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, phase, sections, (owner_id IS NULL) AS built_in,
		        system_prompt, model, temperature
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
		var systemPrompt, modelName *string
		var temperature *float64
		if err := rows.Scan(&tm.ID, &tm.Name, &tm.Phase, &sectionsJSON, &tm.BuiltIn,
			&systemPrompt, &modelName, &temperature); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(sectionsJSON, &tm.Sections); err != nil {
			return nil, err
		}
		applyTemplateOverrides(&tm, systemPrompt, modelName, temperature)
		out = append(out, tm)
	}
	return out, rows.Err()
}

// GetTemplate returns a single template by id, visible to ownerID when it is
// either a built-in (owner_id NULL) or owned by ownerID. Returns ErrNotFound
// otherwise (including cross-owner lookups).
func (s *Store) GetTemplate(ctx context.Context, ownerID, id string) (model.Template, error) {
	var tm model.Template
	var sectionsJSON []byte
	var systemPrompt, modelName *string
	var temperature *float64
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, phase, sections, (owner_id IS NULL) AS built_in,
		        system_prompt, model, temperature
		   FROM templates WHERE id=$1 AND (owner_id IS NULL OR owner_id=$2)`,
		id, ownerID).Scan(&tm.ID, &tm.Name, &tm.Phase, &sectionsJSON, &tm.BuiltIn,
		&systemPrompt, &modelName, &temperature)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Template{}, ErrNotFound
	}
	if err != nil {
		return model.Template{}, err
	}
	if err := json.Unmarshal(sectionsJSON, &tm.Sections); err != nil {
		return model.Template{}, err
	}
	applyTemplateOverrides(&tm, systemPrompt, modelName, temperature)
	return tm, nil
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

// CreateTemplate creates an owner-scoped template. systemPrompt, modelName,
// and temperature are optional per-template agent overrides: an empty
// systemPrompt/modelName or a nil temperature means "unset".
func (s *Store) CreateTemplate(ctx context.Context, ownerID, name, phase string, sections []model.TemplateSection, systemPrompt, modelName string, temperature *float64) (model.Template, error) {
	phase = normalizeTemplatePhase(phase)
	if err := validateTemplate(name, sections); err != nil {
		return model.Template{}, err
	}
	if err := validateTemplatePhase(phase); err != nil {
		return model.Template{}, err
	}
	if err := validateTemplateOverrides(systemPrompt, modelName, temperature); err != nil {
		return model.Template{}, err
	}
	name = strings.TrimSpace(name)
	systemPrompt = strings.TrimSpace(systemPrompt)
	modelName = strings.TrimSpace(modelName)
	if taken, err := s.nameTaken(ctx, ownerID, name, ""); err != nil {
		return model.Template{}, err
	} else if taken {
		return model.Template{}, ErrDuplicate
	}
	secJSON, err := json.Marshal(sections)
	if err != nil {
		return model.Template{}, err
	}
	tm := model.Template{
		ID: uuid.NewString(), Name: name, Phase: phase, Sections: sections, BuiltIn: false,
		SystemPrompt: systemPrompt, Model: modelName, Temperature: temperature,
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO templates (id, owner_id, name, phase, sections, system_prompt, model, temperature)
		 VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7,$8)`,
		tm.ID, ownerID, name, phase, string(secJSON),
		nullableTemplateStr(systemPrompt), nullableTemplateStr(modelName), temperature)
	return tm, err
}

// UpdateTemplate updates an owner-scoped template, including its optional
// agent overrides. Passing an empty systemPrompt/modelName or a nil
// temperature clears that override (unset).
func (s *Store) UpdateTemplate(ctx context.Context, ownerID, id, name, phase string, sections []model.TemplateSection, systemPrompt, modelName string, temperature *float64) error {
	phase = normalizeTemplatePhase(phase)
	if err := validateTemplate(name, sections); err != nil {
		return err
	}
	if err := validateTemplatePhase(phase); err != nil {
		return err
	}
	if err := validateTemplateOverrides(systemPrompt, modelName, temperature); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	systemPrompt = strings.TrimSpace(systemPrompt)
	modelName = strings.TrimSpace(modelName)
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
		`UPDATE templates SET name=$1, phase=$2, sections=$3::jsonb, system_prompt=$4, model=$5, temperature=$6
		  WHERE id=$7 AND owner_id=$8`,
		name, phase, string(secJSON), nullableTemplateStr(systemPrompt), nullableTemplateStr(modelName), temperature,
		id, ownerID)
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
