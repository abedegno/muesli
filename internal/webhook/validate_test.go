package webhook

import (
	"net"
	"testing"
)

// TestIsPrivateIP tests the IsPrivateIP helper directly.
func TestIsPrivateIP(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ip   string
		want bool // true == private/rejected
	}{
		// Globally routable — must be safe.
		{"public-ipv4", "93.184.216.34", false},
		{"google-dns", "8.8.8.8", false},

		// Loopback.
		{"loopback-v4", "127.0.0.1", true},
		{"loopback-v6", "::1", true},

		// Private / RFC1918.
		{"private-10", "10.0.0.1", true},
		{"private-172", "172.16.0.1", true},
		{"private-192", "192.168.1.1", true},
		{"private-ula-v6", "fc00::1", true},

		// Link-local (covers cloud IMDS at 169.254.169.254).
		{"link-local-metadata", "169.254.169.254", true},
		{"link-local-other", "169.254.0.1", true},
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
			got := IsPrivateIP(ip)
			if got != tc.want {
				t.Errorf("IsPrivateIP(%s) = %v, want %v", tc.ip, got, tc.want)
			}
		})
	}
}

// TestValidateWebhookURL tests ValidateWebhookURL with IP-literal URLs so no
// live DNS calls are needed (net.LookupHost on an IP literal returns it unchanged).
func TestValidateWebhookURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		// Valid HTTPS URL with globally-routable IP passes.
		{
			name:    "valid-https",
			rawURL:  "https://93.184.216.34/hook",
			wantErr: false,
		},
		// Valid HTTP URL passes (scheme restriction is only https; http is also allowed).
		{
			name:    "valid-http",
			rawURL:  "http://93.184.216.34/hook",
			wantErr: false,
		},
		// URL with credentials (userinfo) is rejected.
		{
			name:    "credentials-rejected",
			rawURL:  "https://user:pass@93.184.216.34/hook",
			wantErr: true,
		},
		// Loopback address is rejected.
		{
			name:    "loopback-rejected",
			rawURL:  "http://127.0.0.1/hook",
			wantErr: true,
		},
		// Private IP range is rejected.
		{
			name:    "private-ip-rejected",
			rawURL:  "http://10.0.0.1/hook",
			wantErr: true,
		},
		// Link-local / cloud metadata IP is rejected.
		{
			name:    "link-local-metadata-rejected",
			rawURL:  "http://169.254.169.254/hook",
			wantErr: true,
		},
		// FTP scheme is rejected (not http/https).
		{
			name:    "ftp-rejected",
			rawURL:  "ftp://93.184.216.34/hook",
			wantErr: true,
		},
		// Empty URL is rejected.
		{
			name:    "empty-url-rejected",
			rawURL:  "",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateWebhookURL(tc.rawURL)
			if tc.wantErr && err == nil {
				t.Errorf("ValidateWebhookURL(%q) = nil, want error", tc.rawURL)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("ValidateWebhookURL(%q) = %v, want nil", tc.rawURL, err)
			}
		})
	}
}
