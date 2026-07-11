package model

import "time"

type Company struct {
	ID        string    `json:"id"`
	OwnerID   string    `json:"owner_id"`
	Domain    string    `json:"domain"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Person struct {
	ID           string    `json:"id"`
	OwnerID      string    `json:"owner_id"`
	PrimaryEmail string    `json:"primary_email"`
	DisplayName  string    `json:"display_name"`
	CompanyID    *string   `json:"company_id,omitempty"`
	FirstSeenAt  time.Time `json:"first_seen_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
