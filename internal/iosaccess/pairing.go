package iosaccess

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// pairingPayloadVersion is bumped only on an incompatible payload shape
// change; the iOS client rejects any other value.
const pairingPayloadVersion = 1

// PairingPayload is the versioned QR/manual-entry payload for local iOS
// pairing (issue #767). It intentionally carries no password, token, or
// session — pairing establishes endpoint trust only.
type PairingPayload struct {
	Version    int    `json:"v"`
	Origin     string `json:"origin"`
	SPKISHA256 string `json:"spki_sha256"` // hex-encoded, lowercase
	Phrase     string `json:"phrase"`
}

// BuildOrigin returns the canonical HTTPS origin for (address, port),
// bracketing address per RFC 3986 when it is an IPv6 literal.
func BuildOrigin(address string, port int) string {
	host := address
	if ip := net.ParseIP(address); ip != nil && ip.To4() == nil {
		host = "[" + address + "]"
	}
	return fmt.Sprintf("https://%s:%d", host, port)
}

// VerificationPhrase derives a short, human-readable phrase from a SPKI
// fingerprint so a person can verbally/visually confirm the phone and
// desktop agree on which certificate they've pinned, without transcribing a
// hex string. It is deterministic: the same fingerprint always yields the
// same phrase, on both Electron and iOS, independently.
func VerificationPhrase(fingerprint [32]byte) string {
	// Base32 (Crockford-style alphabet minus ambiguous chars) of the first 5
	// bytes, split into two groups, is compact, easy to read aloud, and has
	// no cross-language decoding dependency (both sides only need to
	// recompute this from the same 32-byte SHA-256 digest).
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(fingerprint[:5])
	enc = strings.ToUpper(enc)
	if len(enc) < 8 {
		return enc
	}
	return enc[:4] + "-" + enc[4:8]
}

// BuildPairingPayload assembles the minimized QR/manual-entry payload for
// one certificate bundle and origin.
func BuildPairingPayload(origin string, fingerprint [32]byte) PairingPayload {
	return PairingPayload{
		Version:    pairingPayloadVersion,
		Origin:     origin,
		SPKISHA256: fmt.Sprintf("%x", fingerprint),
		Phrase:     VerificationPhrase(fingerprint),
	}
}

// Encode serializes the payload to the compact JSON string embedded in the
// QR code / offered for manual entry.
func (p PairingPayload) Encode() (string, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// DecodePairingPayload parses and validates a scanned/typed pairing payload
// per the accepted spec: it must be the current version, its origin must be
// a syntactically valid HTTPS origin (no path/query/fragment/credentials)
// resolving to a private-network address, and its fingerprint must be a
// well-formed 32-byte hex digest.
func DecodePairingPayload(raw string) (PairingPayload, error) {
	var p PairingPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return PairingPayload{}, fmt.Errorf("malformed pairing payload: %w", err)
	}
	if p.Version != pairingPayloadVersion {
		return PairingPayload{}, fmt.Errorf("unsupported pairing payload version %d", p.Version)
	}
	if err := validatePrivateHTTPSOrigin(p.Origin); err != nil {
		return PairingPayload{}, err
	}
	if len(p.SPKISHA256) != sha256.Size*2 {
		return PairingPayload{}, fmt.Errorf("invalid fingerprint length")
	}
	for _, c := range p.SPKISHA256 {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return PairingPayload{}, fmt.Errorf("invalid fingerprint encoding")
		}
	}
	if p.Phrase == "" {
		return PairingPayload{}, fmt.Errorf("missing verification phrase")
	}
	return p, nil
}

// validatePrivateHTTPSOrigin enforces the accepted spec's origin shape for
// local pairing: absolute HTTPS, no path/query/fragment/credentials, and a
// host that is a private-network literal address (RFC1918 or ULA) — never a
// hostname (this is pairing to a specific LAN endpoint, not a name the
// client's DNS would resolve).
func validatePrivateHTTPSOrigin(origin string) error {
	u, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("invalid origin: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("origin must be https")
	}
	if u.Path != "" && u.Path != "/" {
		return fmt.Errorf("origin must not contain a path")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("origin must not contain a query or fragment")
	}
	if u.User != nil {
		return fmt.Errorf("origin must not embed credentials")
	}
	host := u.Hostname()
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("origin host must be a literal IP address")
	}
	if _, eligible := classify(ip); !eligible {
		return fmt.Errorf("origin host must be a private-network address")
	}
	if u.Port() == "" {
		return fmt.Errorf("origin must include a port")
	}
	if _, err := strconv.Atoi(u.Port()); err != nil {
		return fmt.Errorf("invalid origin port")
	}
	return nil
}
