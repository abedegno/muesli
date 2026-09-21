package iosaccess_test

import (
	"strings"
	"testing"

	"github.com/abedegno/muesli/internal/iosaccess"
)

func TestBuildOriginBracketsIPv6(t *testing.T) {
	if got, want := iosaccess.BuildOrigin("192.168.1.20", 8443), "https://192.168.1.20:8443"; got != want {
		t.Fatalf("ipv4: got %q want %q", got, want)
	}
	if got, want := iosaccess.BuildOrigin("fd12:3456:789a:1::20", 8443), "https://[fd12:3456:789a:1::20]:8443"; got != want {
		t.Fatalf("ipv6: got %q want %q", got, want)
	}
}

func TestVerificationPhraseDeterministicAndDistinct(t *testing.T) {
	fp1, err := iosaccess.LoadOrCreateCertificate(t.TempDir(), "192.168.1.20")
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	p1a := iosaccess.VerificationPhrase(fp1.SPKIFingerprint)
	p1b := iosaccess.VerificationPhrase(fp1.SPKIFingerprint)
	if p1a != p1b {
		t.Fatalf("phrase not deterministic: %q vs %q", p1a, p1b)
	}

	fp2, err := iosaccess.LoadOrCreateCertificate(t.TempDir(), "192.168.1.21")
	if err != nil {
		t.Fatalf("cert2: %v", err)
	}
	p2 := iosaccess.VerificationPhrase(fp2.SPKIFingerprint)
	if p1a == p2 {
		t.Fatalf("distinct fingerprints produced the same phrase (collision in fixture, or logic bug)")
	}
}

func TestPairingPayloadRoundTripAndMinimization(t *testing.T) {
	cert, err := iosaccess.LoadOrCreateCertificate(t.TempDir(), "192.168.1.20")
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	origin := iosaccess.BuildOrigin(cert.Address, 8443)
	payload := iosaccess.BuildPairingPayload(origin, cert.SPKIFingerprint)

	raw, err := payload.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	for _, forbidden := range []string{"password", "token", "session", "key", "BEGIN "} {
		if strings.Contains(strings.ToLower(raw), strings.ToLower(forbidden)) {
			t.Fatalf("pairing payload leaked forbidden content %q: %s", forbidden, raw)
		}
	}

	decoded, err := iosaccess.DecodePairingPayload(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Origin != origin || decoded.Phrase != payload.Phrase {
		t.Fatalf("round trip mismatch: %+v vs %+v", decoded, payload)
	}
}

func TestDecodePairingPayloadRejectsInvalid(t *testing.T) {
	cert, err := iosaccess.LoadOrCreateCertificate(t.TempDir(), "192.168.1.20")
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	validOrigin := iosaccess.BuildOrigin(cert.Address, 8443)
	valid := iosaccess.BuildPairingPayload(validOrigin, cert.SPKIFingerprint)
	validRaw, _ := valid.Encode()

	cases := map[string]string{
		"not json":          "not json at all",
		"wrong version":     `{"v":99,"origin":"https://192.168.1.20:8443","spki_sha256":"` + valid.SPKISHA256 + `","phrase":"AB-CD"}`,
		"http not https":    `{"v":1,"origin":"http://192.168.1.20:8443","spki_sha256":"` + valid.SPKISHA256 + `","phrase":"AB-CD"}`,
		"public address":    `{"v":1,"origin":"https://8.8.8.8:8443","spki_sha256":"` + valid.SPKISHA256 + `","phrase":"AB-CD"}`,
		"hostname not ip":   `{"v":1,"origin":"https://example.com:8443","spki_sha256":"` + valid.SPKISHA256 + `","phrase":"AB-CD"}`,
		"path present":      `{"v":1,"origin":"https://192.168.1.20:8443/pair","spki_sha256":"` + valid.SPKISHA256 + `","phrase":"AB-CD"}`,
		"query present":     `{"v":1,"origin":"https://192.168.1.20:8443?x=1","spki_sha256":"` + valid.SPKISHA256 + `","phrase":"AB-CD"}`,
		"embedded creds":    `{"v":1,"origin":"https://user:pass@192.168.1.20:8443","spki_sha256":"` + valid.SPKISHA256 + `","phrase":"AB-CD"}`,
		"short fingerprint": `{"v":1,"origin":"https://192.168.1.20:8443","spki_sha256":"abcd","phrase":"AB-CD"}`,
		"missing phrase":    `{"v":1,"origin":"https://192.168.1.20:8443","spki_sha256":"` + valid.SPKISHA256 + `","phrase":""}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := iosaccess.DecodePairingPayload(raw); err == nil {
				t.Fatalf("expected rejection")
			}
		})
	}

	if _, err := iosaccess.DecodePairingPayload(validRaw); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}
}
