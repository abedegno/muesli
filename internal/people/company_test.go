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
			name:        "canonical company domain",
			email:       "alice@youtube.com",
			wantDomain:  "google.com",
			wantCompany: true,
		},
		{
			name:        "canonical company domain from aliased host",
			email:       "bob@youtu.be",
			wantDomain:  "google.com",
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

func TestCanonicalCompanyDomain(t *testing.T) {
	tests := []struct {
		name   string
		domain string
		want   string
	}{
		{name: "youtube", domain: "youtube.com", want: "google.com"},
		{name: "youtu.be", domain: "youtu.be", want: "google.com"},
		{name: "office", domain: "office.com", want: "microsoft.com"},
		{name: "microsoft365", domain: "microsoft365.com", want: "microsoft.com"},
		{name: "fb", domain: "fb.com", want: "facebook.com"},
		{name: "aws", domain: "aws.amazon.com", want: "amazon.com"},
		{name: "unmapped", domain: "example.com", want: "example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanonicalCompanyDomain(tt.domain); got != tt.want {
				t.Fatalf("CanonicalCompanyDomain(%q) = %q, want %q", tt.domain, got, tt.want)
			}
		})
	}
}
