package store

import (
	"context"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
)

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
