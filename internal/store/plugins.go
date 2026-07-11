package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// sealConfig encrypts a plaintext config JSON into a JSON string value suitable
// for the JSONB column (e.g. `"<base64>"`).
func sealConfig(cr *crypto.Crypto, cfg json.RawMessage) (string, error) {
	if len(cfg) == 0 {
		cfg = json.RawMessage(`{}`)
	}
	sealed, err := cr.Seal([]byte(cfg))
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(sealed) // wrap the base64 string as a JSON string
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// openConfig reverses sealConfig.
func openConfig(cr *crypto.Crypto, stored string) (json.RawMessage, error) {
	var sealed string
	if err := json.Unmarshal([]byte(stored), &sealed); err != nil {
		return nil, err
	}
	plain, err := cr.Open(sealed)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(plain), nil
}

func (s *Store) CreatePlugin(ctx context.Context, cr *crypto.Crypto, p model.Plugin) (model.Plugin, error) {
	p.ID = uuid.NewString()
	stored, err := sealConfig(cr, p.Config)
	if err != nil {
		return model.Plugin{}, err
	}
	if len(p.ConfigSchema) == 0 {
		p.ConfigSchema = json.RawMessage(`{}`)
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO plugins (id, kind, name, endpoint_url, token, config, config_schema, enabled, is_default)
		 VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9)`,
		p.ID, p.Kind, p.Name, p.EndpointURL, p.Token, stored, string(p.ConfigSchema), p.Enabled, p.IsDefault)
	if err != nil {
		return model.Plugin{}, err
	}
	return p, nil
}

func (s *Store) UpdatePlugin(ctx context.Context, cr *crypto.Crypto, p model.Plugin) error {
	stored, err := sealConfig(cr, p.Config)
	if err != nil {
		return err
	}
	if len(p.ConfigSchema) == 0 {
		p.ConfigSchema = json.RawMessage(`{}`)
	}
	ct, err := s.pool.Exec(ctx,
		`UPDATE plugins SET name=$1, endpoint_url=$2, token=$3, config=$4::jsonb, config_schema=$5::jsonb, enabled=$6
		 WHERE id=$7`,
		p.Name, p.EndpointURL, p.Token, stored, string(p.ConfigSchema), p.Enabled, p.ID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PatchPlugin atomically (in one transaction) optionally makes the plugin the
// default for its kind and, if updateFields is set, writes the merged fields.
// Either part alone can fail without a partial write, unlike calling
// SetDefaultPlugin then UpdatePlugin separately.
func (s *Store) PatchPlugin(ctx context.Context, cr *crypto.Crypto, p model.Plugin, makeDefault, updateFields bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if makeDefault {
		var kind string
		err := tx.QueryRow(ctx, `SELECT kind FROM plugins WHERE id=$1`, p.ID).Scan(&kind)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE plugins SET is_default=false WHERE kind=$1`, kind); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE plugins SET is_default=true WHERE id=$1`, p.ID); err != nil {
			return err
		}
	}

	if updateFields {
		stored, err := sealConfig(cr, p.Config)
		if err != nil {
			return err
		}
		if len(p.ConfigSchema) == 0 {
			p.ConfigSchema = json.RawMessage(`{}`)
		}
		ct, err := tx.Exec(ctx,
			`UPDATE plugins SET name=$1, endpoint_url=$2, token=$3, config=$4::jsonb, config_schema=$5::jsonb, enabled=$6
			 WHERE id=$7`,
			p.Name, p.EndpointURL, p.Token, stored, string(p.ConfigSchema), p.Enabled, p.ID)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			return ErrNotFound
		}
	}

	return tx.Commit(ctx)
}

func (s *Store) DeletePlugin(ctx context.Context, id string) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM plugins WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetPlugin returns a fully-decrypted plugin (config plaintext + token).
func (s *Store) GetPlugin(ctx context.Context, cr *crypto.Crypto, id string) (model.Plugin, error) {
	var p model.Plugin
	var stored string
	var schemaRaw string
	err := s.pool.QueryRow(ctx,
		`SELECT id, kind, name, endpoint_url, token, config::text, config_schema::text, enabled, is_default
		 FROM plugins WHERE id=$1`, id).
		Scan(&p.ID, &p.Kind, &p.Name, &p.EndpointURL, &p.Token, &stored, &schemaRaw, &p.Enabled, &p.IsDefault)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Plugin{}, ErrNotFound
	}
	if err != nil {
		return model.Plugin{}, err
	}
	cfg, err := openConfig(cr, stored)
	if err != nil {
		return model.Plugin{}, err
	}
	p.Config = cfg
	p.ConfigSchema = json.RawMessage(schemaRaw)
	return p, nil
}

// schemaPasswordKeys returns the set of property keys in a JSON Schema that
// have "writeOnly": true OR "format": "password".
func schemaPasswordKeys(schema json.RawMessage) map[string]bool {
	if len(schema) == 0 {
		return nil
	}
	var s struct {
		Properties map[string]struct {
			WriteOnly bool   `json:"writeOnly"`
			Format    string `json:"format"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &s); err != nil || s.Properties == nil {
		return nil
	}
	out := make(map[string]bool)
	for k, v := range s.Properties {
		if v.WriteOnly || v.Format == "password" {
			out[k] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ListPlugins returns all plugins for the admin view. Fields marked
// "format":"password" or "writeOnly":true in config_schema are redacted to "*";
// if no schema is present the entire config is returned as {} (legacy behaviour).
// The token is always omitted.
func (s *Store) ListPlugins(ctx context.Context, cr *crypto.Crypto) ([]model.Plugin, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, kind, name, endpoint_url, enabled, is_default, config_schema::text, config::text
		 FROM plugins ORDER BY kind, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Plugin
	for rows.Next() {
		var p model.Plugin
		var schemaRaw, storedConfig string
		if err := rows.Scan(&p.ID, &p.Kind, &p.Name, &p.EndpointURL, &p.Enabled, &p.IsDefault, &schemaRaw, &storedConfig); err != nil {
			return nil, err
		}
		p.ConfigSchema = json.RawMessage(schemaRaw)

		keys := schemaPasswordKeys(p.ConfigSchema)
		if len(keys) == 0 {
			// No schema or no sensitive fields — redact entire config (legacy behaviour).
			p.Config = json.RawMessage(`{}`)
		} else {
			// Decrypt and redact only the sensitive keys.
			plain, err := openConfig(cr, storedConfig)
			if err != nil {
				// On decrypt failure fall back to full redaction.
				p.Config = json.RawMessage(`{}`)
			} else {
				var m map[string]json.RawMessage
				if err := json.Unmarshal(plain, &m); err != nil {
					p.Config = json.RawMessage(`{}`)
				} else {
					for k := range keys {
						if _, ok := m[k]; ok {
							m[k] = json.RawMessage(`"*"`)
						}
					}
					b, err := json.Marshal(m)
					if err != nil {
						p.Config = json.RawMessage(`{}`)
					} else {
						p.Config = json.RawMessage(b)
					}
				}
			}
		}
		out = append(out, p)
	}
	if out == nil {
		out = []model.Plugin{}
	}
	return out, rows.Err()
}

// SetDefaultPlugin makes one plugin the default for its kind, unsetting any other
// default of the same kind in a single transaction.
func (s *Store) SetDefaultPlugin(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var kind string
	err = tx.QueryRow(ctx, `SELECT kind FROM plugins WHERE id=$1`, id).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE plugins SET is_default=false WHERE kind=$1`, kind); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE plugins SET is_default=true WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// EnsureDefaultPlugin idempotently registers a single, stable plugin identified
// by (kind, name) and marks it the default for its kind. If no plugin with that
// name+kind exists it is created (enabled, config encrypted via cr); otherwise
// its endpoint_url/token/config are updated in place. Re-running never
// duplicates and always reflects the latest url/token/config. configJSON must be
// valid JSON; an empty string defaults to "{}".
func (s *Store) EnsureDefaultPlugin(ctx context.Context, cr *crypto.Crypto, kind, name, endpointURL, token, configJSON string) error {
	if configJSON == "" {
		configJSON = "{}"
	}
	if !json.Valid([]byte(configJSON)) {
		return errors.New("EnsureDefaultPlugin: configJSON is not valid JSON")
	}

	var id string
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM plugins WHERE kind=$1 AND name=$2`, kind, name).Scan(&id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		created, cerr := s.CreatePlugin(ctx, cr, model.Plugin{
			Kind:        kind,
			Name:        name,
			EndpointURL: endpointURL,
			Token:       token,
			Config:      json.RawMessage(configJSON),
			Enabled:     true,
		})
		if cerr != nil {
			return cerr
		}
		id = created.ID
	case err != nil:
		return err
	default:
		// Load the existing config_schema so EnsureDefaultPlugin never clobbers
		// a schema registered via the admin API.
		var schemaRaw string
		if err := s.pool.QueryRow(ctx, `SELECT config_schema::text FROM plugins WHERE id=$1`, id).Scan(&schemaRaw); err != nil {
			return err
		}
		// Update fields and make default atomically.
		return s.PatchPlugin(ctx, cr, model.Plugin{
			ID:           id,
			Name:         name,
			EndpointURL:  endpointURL,
			Token:        token,
			Config:       json.RawMessage(configJSON),
			ConfigSchema: json.RawMessage(schemaRaw),
			Enabled:      true,
		}, true, true)
	}

	// Newly created: mark it the default for its kind.
	return s.SetDefaultPlugin(ctx, id)
}

// DefaultPlugin returns the enabled default plugin for a kind, fully decrypted.
func (s *Store) DefaultPlugin(ctx context.Context, cr *crypto.Crypto, kind string) (model.Plugin, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM plugins WHERE kind=$1 AND is_default=true AND enabled=true`, kind).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Plugin{}, ErrNotFound
	}
	if err != nil {
		return model.Plugin{}, err
	}
	return s.GetPlugin(ctx, cr, id)
}
