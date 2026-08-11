package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
)

const maxRuleDepth = 8

type rawRuleNode struct {
	Op       string            `json:"op"`
	Children []json.RawMessage `json:"children"`
	Field    string            `json:"field"`
	Operator string            `json:"operator"`
	Value    json.RawMessage   `json:"value"`
}

var validStatuses = map[string]bool{
	"draft":     true,
	"recording": true, "uploaded": true, "transcribing": true,
	"summarizing": true, "ready": true, "failed": true,
}

var fieldOperators = map[string]map[string]bool{
	"tag":     {"is": true, "isNot": true},
	"title":   {"contains": true, "equals": true},
	"status":  {"is": true, "isNot": true},
	"folder":  {"is": true, "isNot": true},
	"created": {"withinLastDays": true},
}

// ValidateRule checks a rule's SHAPE (root must be a group). Returns the first error or nil.
func ValidateRule(raw json.RawMessage) error { return validateNode(raw, 0, true) }

func validateNode(raw json.RawMessage, depth int, mustBeGroup bool) error {
	if depth > maxRuleDepth {
		return fmt.Errorf("rule too deeply nested (max %d)", maxRuleDepth)
	}
	var n rawRuleNode
	if err := json.Unmarshal(raw, &n); err != nil {
		return fmt.Errorf("invalid rule node: %w", err)
	}
	isGroup := n.Op != "" || n.Children != nil
	if isGroup {
		if n.Op != "and" && n.Op != "or" {
			return fmt.Errorf("group op must be and/or, got %q", n.Op)
		}
		for _, c := range n.Children {
			if err := validateNode(c, depth+1, false); err != nil {
				return err
			}
		}
		return nil
	}
	if mustBeGroup {
		return errors.New("rule root must be a group")
	}
	ops, ok := fieldOperators[n.Field]
	if !ok {
		return fmt.Errorf("unknown field %q", n.Field)
	}
	if !ops[n.Operator] {
		return fmt.Errorf("operator %q not valid for field %q", n.Operator, n.Field)
	}
	switch n.Field {
	case "status":
		var s string
		if err := json.Unmarshal(n.Value, &s); err != nil || !validStatuses[s] {
			return errors.New("invalid status value")
		}
	case "created":
		var d int
		if err := json.Unmarshal(n.Value, &d); err != nil || d <= 0 {
			return errors.New("withinLastDays must be a positive integer")
		}
	default:
		var s string
		if err := json.Unmarshal(n.Value, &s); err != nil || strings.TrimSpace(s) == "" {
			return fmt.Errorf("value for %q must be a non-empty string", n.Field)
		}
	}
	return nil
}

func (s *Store) CreateSmartList(ctx context.Context, ownerID, name string, rule json.RawMessage) (model.SmartList, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return model.SmartList{}, ValidationError("smart list name invalid")
	}
	if err := ValidateRule(rule); err != nil {
		return model.SmartList{}, ValidationError(fmt.Sprintf("invalid rule: %s", err))
	}
	var sl model.SmartList
	err := s.pool.QueryRow(ctx,
		`INSERT INTO smart_lists (id, owner_id, name, rule) VALUES ($1,$2,$3,$4)
		 RETURNING id, name, rule, created_at`,
		uuid.NewString(), ownerID, name, rule).Scan(&sl.ID, &sl.Name, &sl.Rule, &sl.CreatedAt)
	return sl, err
}

func (s *Store) ListSmartLists(ctx context.Context, ownerID string) ([]model.SmartList, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, rule, created_at FROM smart_lists WHERE owner_id=$1 AND deleted_at IS NULL ORDER BY lower(name)`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.SmartList{}
	for rows.Next() {
		var sl model.SmartList
		if err := rows.Scan(&sl.ID, &sl.Name, &sl.Rule, &sl.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sl)
	}
	return out, rows.Err()
}

func (s *Store) UpdateSmartList(ctx context.Context, ownerID, id, name string, rule json.RawMessage) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return ValidationError("smart list name invalid")
	}
	if err := ValidateRule(rule); err != nil {
		return ValidationError(fmt.Sprintf("invalid rule: %s", err))
	}
	ct, err := s.pool.Exec(ctx,
		`UPDATE smart_lists SET name=$1, rule=$2, updated_at=now() WHERE id=$3 AND owner_id=$4 AND deleted_at IS NULL`,
		name, rule, id, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSmartList soft-deletes a smart list (Move to Trash), stamping
// deleted_at=now(). Returns ErrNotFound when the list is absent, not owned, or
// already trashed.
func (s *Store) DeleteSmartList(ctx context.Context, ownerID, id string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE smart_lists SET deleted_at=now(), updated_at=now() WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL`,
		id, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListTrashedSmartLists returns the owner's trashed smart lists, most-recently
// trashed first.
func (s *Store) ListTrashedSmartLists(ctx context.Context, ownerID string) ([]model.SmartList, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, rule, created_at, deleted_at FROM smart_lists WHERE owner_id=$1 AND deleted_at IS NOT NULL ORDER BY deleted_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.SmartList{}
	for rows.Next() {
		var sl model.SmartList
		if err := rows.Scan(&sl.ID, &sl.Name, &sl.Rule, &sl.CreatedAt, &sl.DeletedAt); err != nil {
			return nil, err
		}
		out = append(out, sl)
	}
	return out, rows.Err()
}

// RestoreSmartList un-trashes a smart list. Returns ErrNotFound when the list is
// absent, not owned, or not trashed.
func (s *Store) RestoreSmartList(ctx context.Context, ownerID, id string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE smart_lists SET deleted_at=NULL, updated_at=now() WHERE id=$1 AND owner_id=$2 AND deleted_at IS NOT NULL`,
		id, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PurgeSmartList permanently deletes a trashed smart list. Returns ErrNotFound
// when the list is absent, not owned, or not trashed.
func (s *Store) PurgeSmartList(ctx context.Context, ownerID, id string) error {
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM smart_lists WHERE id=$1 AND owner_id=$2 AND deleted_at IS NOT NULL`, id, ownerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PurgeExpiredSmartLists permanently deletes all smart lists trashed longer ago
// than olderThan. Not owner-scoped — it's the purge job. Returns the number of
// rows deleted.
func (s *Store) PurgeExpiredSmartLists(ctx context.Context, olderThan time.Duration) (int, error) {
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM smart_lists WHERE deleted_at IS NOT NULL AND deleted_at < now() - $1::interval`,
		fmt.Sprintf("%d seconds", int64(olderThan.Seconds())))
	if err != nil {
		return 0, err
	}
	return int(ct.RowsAffected()), nil
}
