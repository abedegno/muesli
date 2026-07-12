package store

import (
	"context"
	"errors"
	"strings"

	"github.com/abedegno/muesli/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

func (s *Store) ListPeople(ctx context.Context, ownerID string) ([]model.Person, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, owner_id, primary_email, display_name, company_id, first_seen_at, updated_at
		 FROM people
		 WHERE owner_id=$1
		 ORDER BY display_name, primary_email`, ownerID)
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
