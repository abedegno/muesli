package webhook

import (
	"fmt"
	"net"
	"net/url"
)

// IsPrivateIP reports whether ip is a loopback, private (RFC1918/ULA),
// link-local, or otherwise non-globally-routable address that must never be
// the target of an outbound webhook request.
//
// Covered ranges:
//   - Loopback          127.0.0.0/8, ::1
//   - Private/RFC1918   10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16
//   - ULA               fc00::/7
//   - Link-local        169.254.0.0/16, fe80::/10
//   - Cloud IMDS        169.254.169.254/32 (already in link-local range)
//   - Unspecified       0.0.0.0, ::
//   - Multicast         224.0.0.0/4, ff00::/8
func IsPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsMulticast()
}

// ValidateWebhookURL checks rawURL for SSRF safety before an outbound
// webhook request is made. It is intentionally strict: no allow-list
// overrides (use Validate if you need that).
//
// Rules:
//   - Scheme must be "http" or "https"
//   - URL must not contain userinfo (credentials)
//   - Host must not be empty
//   - All IPs the hostname resolves to must pass IsPrivateIP (i.e. must NOT
//     be private/loopback/link-local/reserved)
//
// DNS resolution is done via net.LookupHost so it uses the process's
// resolver configuration.
func ValidateWebhookURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("webhook: invalid URL %q: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("webhook: URL %q uses scheme %q; only http or https is allowed", rawURL, u.Scheme)
	}
	if u.User != nil {
		return fmt.Errorf("webhook: URL %q must not contain credentials", rawURL)
	}
	if u.Host == "" {
		return fmt.Errorf("webhook: URL %q has no host", rawURL)
	}

	host := u.Hostname()
	addrs, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("webhook: DNS lookup for %q failed: %w", host, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("webhook: DNS lookup for %q returned no addresses", host)
	}
	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			return fmt.Errorf("webhook: DNS resolved non-IP address %q for %q", addr, host)
		}
		if IsPrivateIP(ip) {
			return fmt.Errorf("webhook: URL %q resolves to private/reserved IP %s", rawURL, ip)
		}
	}
	return nil
}
