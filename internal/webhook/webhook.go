// Package webhook provides SSRF-safe outbound webhook delivery helpers.
package webhook

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateIP returns an error if ip is not globally routable.
//
// Rejected ranges:
//   - Loopback            (IsLoopback)         e.g. 127.0.0.1, ::1
//   - Private / RFC1918   (IsPrivate)           e.g. 10/8, 172.16/12, 192.168/16, fc00::/7
//   - Link-local unicast  (IsLinkLocalUnicast)  e.g. 169.254.0.0/16, fe80::/10
//   - Link-local multicast (IsLinkLocalMulticast)
//   - Unspecified         (IsUnspecified)       e.g. 0.0.0.0, ::
//   - Multicast           (IsMulticast)         e.g. 224.0.0.0/4
func ValidateIP(ip net.IP) error {
	switch {
	case ip.IsLoopback():
		return fmt.Errorf("webhook: IP %s is loopback", ip)
	case ip.IsPrivate():
		return fmt.Errorf("webhook: IP %s is a private/RFC1918 address", ip)
	case ip.IsLinkLocalUnicast():
		return fmt.Errorf("webhook: IP %s is link-local (covers AWS metadata 169.254.169.254)", ip)
	case ip.IsLinkLocalMulticast():
		return fmt.Errorf("webhook: IP %s is link-local multicast", ip)
	case ip.IsUnspecified():
		return fmt.Errorf("webhook: IP %s is unspecified", ip)
	case ip.IsMulticast():
		return fmt.Errorf("webhook: IP %s is multicast", ip)
	}
	return nil
}

// canonicalOrigin returns the lower-cased scheme+host+port string for u,
// filling in the default port (443 for https, 80 for http) when absent.
// Lowercasing the hostname ensures case-insensitive allowList matching.
func canonicalOrigin(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		default:
			// Unknown scheme: no default port; may still match an allowList entry.
			return u.Scheme + "://" + host
		}
	}
	return u.Scheme + "://" + host + ":" + port
}

// Validate checks rawURL for SSRF safety.
// It is ALSO called at delivery time (guard against DNS rebinding).
//
// Rules:
//   - Scheme must be https (exception: http is allowed only when the exact
//     scheme+host+port string appears in allowList)
//   - Resolves the host via net.DefaultResolver.LookupIPAddr; ALL resolved IPs
//     must be globally-routable (not loopback, RFC1918, link-local, metadata,
//     unspecified, or multicast)
//   - allowList entries that start with http:// bypass the https requirement
//     but NOT the IP check (a localhost entry bypasses both)
//
// For the allowList bypass: if the canonicalized scheme+host+port of rawURL
// exactly matches any entry, skip both the scheme check and the IP check.
//
// Returns a descriptive error on any rejection; nil means safe to use.
func Validate(ctx context.Context, rawURL string, allowList []string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("webhook: invalid URL %q: %w", rawURL, err)
	}
	if u.Host == "" {
		return fmt.Errorf("webhook: URL %q has no host", rawURL)
	}

	canonical := canonicalOrigin(u)

	// AllowList check runs BEFORE any scheme gate so that any scheme+host+port
	// present in the list bypasses both the scheme check and the IP check.
	for _, entry := range allowList {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		eu, err := url.Parse(entry)
		if err != nil {
			continue // ignore malformed entries
		}
		if canonicalOrigin(eu) == canonical {
			// Exact match — skip both scheme check and IP check.
			return nil
		}
	}

	// Scheme check: only https is permitted outside the allowList.
	if u.Scheme != "https" {
		return fmt.Errorf("webhook: URL %q uses scheme %q; only https is permitted (or add to allowList)", rawURL, u.Scheme)
	}

	// Resolve all IPs for the host and verify each one.
	host := u.Hostname()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("webhook: DNS lookup for %q failed: %w", host, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("webhook: DNS lookup for %q returned no addresses", host)
	}
	for _, addr := range addrs {
		if err := ValidateIP(addr.IP); err != nil {
			return err
		}
	}
	return nil
}
