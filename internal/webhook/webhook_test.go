package webhook

import (
	"context"
	"net"
	neturl "net/url"
	"testing"
)

// TestValidateIP tests the IP-level SSRF guard without any DNS.
func TestValidateIP(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		ip      string
		wantErr bool
	}{
		// Globally routable — must pass.
		{"globally-routable-ipv4", "93.184.216.34", false},
		{"globally-routable-ipv4-google-dns", "8.8.8.8", false},

		// Loopback.
		{"loopback-v4", "127.0.0.1", true},
		{"loopback-v4-alt", "127.0.0.2", true},
		{"loopback-v6", "::1", true},

		// Private / RFC1918.
		{"private-10", "10.0.0.1", true},
		{"private-192", "192.168.1.1", true},
		{"private-172", "172.16.0.1", true},
		{"private-172-end", "172.31.255.255", true},
		{"private-ipv6-ula", "fc00::1", true},

		// Link-local unicast (includes AWS metadata endpoint).
		{"link-local-v4-metadata", "169.254.169.254", true},
		{"link-local-v4-other", "169.254.0.1", true},
		{"link-local-v6", "fe80::1", true},

		// Multicast.
		{"multicast-v4", "224.0.0.1", true},
		{"multicast-v6", "ff02::1", true},

		// Unspecified.
		{"unspecified-v4", "0.0.0.0", true},
		{"unspecified-v6", "::", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("invalid test IP %q", tc.ip)
			}
			err := ValidateIP(ip)
			if tc.wantErr && err == nil {
				t.Errorf("ValidateIP(%s) = nil, want error", tc.ip)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("ValidateIP(%s) = %v, want nil", tc.ip, err)
			}
		})
	}
}

// TestValidate tests Validate with IP-literal URLs (no real DNS needed) and
// allowList logic. Using IP literals avoids any live DNS dependency.
func TestValidate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name      string
		rawURL    string
		allowList []string
		wantErr   bool
	}{
		// Globally-routable HTTPS with IP literal — must pass (LookupIPAddr on
		// an IP literal returns it directly without a network call).
		{
			name:    "https-global-ip",
			rawURL:  "https://93.184.216.34",
			wantErr: false,
		},
		{
			name:    "https-global-ip-with-path",
			rawURL:  "https://93.184.216.34/events",
			wantErr: false,
		},

		// HTTP without allowList — rejected (bad scheme).
		{
			name:    "http-global-ip-no-allowlist",
			rawURL:  "http://93.184.216.34",
			wantErr: true,
		},

		// Private-range IPs via HTTPS — rejected.
		{
			name:    "loopback-v4",
			rawURL:  "https://127.0.0.1",
			wantErr: true,
		},
		{
			name:    "private-10",
			rawURL:  "https://10.0.0.1",
			wantErr: true,
		},
		{
			name:    "private-192",
			rawURL:  "https://192.168.1.1",
			wantErr: true,
		},
		{
			name:    "private-172",
			rawURL:  "https://172.16.0.1",
			wantErr: true,
		},
		{
			name:    "link-local-metadata",
			rawURL:  "https://169.254.169.254",
			wantErr: true,
		},
		{
			name:    "loopback-v6",
			rawURL:  "https://[::1]",
			wantErr: true,
		},
		{
			name:    "link-local-v6",
			rawURL:  "https://[fe80::1]",
			wantErr: true,
		},

		// allowList exact match — both scheme and IP checks bypassed.
		{
			name:      "allowlist-localhost-exact-match",
			rawURL:    "http://localhost:9000",
			allowList: []string{"http://localhost:9000"},
			wantErr:   false,
		},
		// allowList — different port, no match → error (bad scheme for http without allowlist).
		{
			name:      "allowlist-localhost-wrong-port",
			rawURL:    "http://localhost:9001",
			allowList: []string{"http://localhost:9000"},
			wantErr:   true,
		},
		// allowList — default port normalisation (https://host == https://host:443).
		{
			name:      "allowlist-https-default-port-normalisation",
			rawURL:    "https://93.184.216.34:443",
			allowList: []string{"https://93.184.216.34"},
			wantErr:   false, // canonicalized same, and IP is globally routable anyway
		},

		// allowList bypass for non-http/https scheme — must pass.
		{
			name:      "allowlist-non-https-scheme-bypassed",
			rawURL:    "http://internal.example.com:8080",
			allowList: []string{"http://internal.example.com:8080"},
			wantErr:   false,
		},
		// allowList — case-insensitive hostname matching.
		{
			name:      "allowlist-hostname-case-insensitive",
			rawURL:    "http://localhost:9000",
			allowList: []string{"http://LOCALHOST:9000"},
			wantErr:   false,
		},

		// Unparseable / bad-scheme URLs.
		{
			name:    "not-a-url",
			rawURL:  "not-a-url",
			wantErr: true,
		},
		{
			name:    "ftp-scheme",
			rawURL:  "ftp://93.184.216.34",
			wantErr: true,
		},
		{
			name:    "empty-url",
			rawURL:  "",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := Validate(ctx, tc.rawURL, tc.allowList)
			if tc.wantErr && err == nil {
				t.Errorf("Validate(%q, %v) = nil, want error", tc.rawURL, tc.allowList)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Validate(%q, %v) = %v, want nil", tc.rawURL, tc.allowList, err)
			}
		})
	}
}

// TestCanonicalOrigin exercises port normalisation directly (same package).
func TestCanonicalOrigin(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want string
	}{
		{"https://example.com", "https://example.com:443"},
		{"https://example.com:8443", "https://example.com:8443"},
		{"http://example.com", "http://example.com:80"},
		{"http://example.com:9000", "http://example.com:9000"},
		{"http://localhost:9000", "http://localhost:9000"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			u, err := neturl.Parse(tc.raw)
			if err != nil {
				t.Fatalf("url.Parse(%q): %v", tc.raw, err)
			}
			got := canonicalOrigin(u)
			if got != tc.want {
				t.Errorf("canonicalOrigin(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
