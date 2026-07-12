package people

import (
	"strings"

	"golang.org/x/net/publicsuffix"
)

var personalProviders = map[string]struct{}{
	"gmail.com":      {},
	"googlemail.com": {},
	"outlook.com":    {},
	"hotmail.com":    {},
	"live.com":       {},
	"msn.com":        {},
	"yahoo.com":      {},
	"ymail.com":      {},
	"icloud.com":     {},
	"me.com":         {},
	"mac.com":        {},
	"aol.com":        {},
	"proton.me":      {},
	"protonmail.com": {},
	"pm.me":          {},
	"gmx.com":        {},
	"mail.com":       {},
	"zoho.com":       {},
	"fastmail.com":   {},
	"hey.com":        {},
}

var canonicalDomains = map[string]string{
	"youtube.com":      "google.com",
	"youtu.be":         "google.com",
	"office.com":       "microsoft.com",
	"microsoft365.com": "microsoft.com",
	"fb.com":           "facebook.com",
	"instagram.com":    "facebook.com",
	"messenger.com":    "facebook.com",
	"aws.amazon.com":   "amazon.com",
}

// CanonicalCompanyDomain collapses well-known company/domain aliases to a
// single canonical company domain.
func CanonicalCompanyDomain(domain string) string {
	if canonical, ok := canonicalDomains[domain]; ok {
		return canonical
	}
	return domain
}

// CompanyDomain returns the registrable domain for an email address and
// whether it is considered a company domain.
func CompanyDomain(email string) (domain string, isCompany bool) {
	email = strings.ToLower(email)

	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return "", false
	}

	host := email[at+1:]
	domain, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return "", false
	}

	domain = CanonicalCompanyDomain(domain)

	if _, ok := personalProviders[domain]; ok {
		return domain, false
	}

	return domain, true
}
