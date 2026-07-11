package people

import "testing"

func TestCompanyDomain(t *testing.T) {
	tests := []struct {
		name        string
		email       string
		wantDomain  string
		wantCompany bool
	}{
		{
			name:        "company domain",
			email:       "alice@acme.com",
			wantDomain:  "acme.com",
			wantCompany: true,
		},
		{
			name:        "subdomain collapses to registrable domain",
			email:       "bob@mail.acme.co.uk",
			wantDomain:  "acme.co.uk",
			wantCompany: true,
		},
		{
			name:        "personal provider",
			email:       "x@gmail.com",
			wantDomain:  "gmail.com",
			wantCompany: false,
		},
		{
			name:        "malformed email",
			email:       "bad",
			wantDomain:  "",
			wantCompany: false,
		},
		{
			name:        "unresolvable host",
			email:       "x@localhost",
			wantDomain:  "",
			wantCompany: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDomain, gotCompany := CompanyDomain(tt.email)
			if gotDomain != tt.wantDomain || gotCompany != tt.wantCompany {
				t.Fatalf("CompanyDomain(%q) = (%q, %v), want (%q, %v)", tt.email, gotDomain, gotCompany, tt.wantDomain, tt.wantCompany)
			}
		})
	}
}
