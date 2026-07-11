package main

import "testing"

func TestReadyBanner(t *testing.T) {
	b := readyBanner("http://localhost:8080/")
	t.Logf("rendered banner:\n%s", b)

	for _, want := range []string{
		"muesli is served",              // the message
		"Everything is up.",             // the ready indicator
		"\\_______________/",            // the bowl
		"http://localhost:8080/admin",   // admin link (trailing slash trimmed)
		"http://localhost:8080/healthz", // health link
		"http://localhost:8080/readyz",  // readyz link
	} {
		if !contains(b, want) {
			t.Errorf("banner missing %q\n---\n%s", want, b)
		}
	}

	// The trailing slash on the public URL must not produce a double slash.
	if contains(b, "8080//admin") {
		t.Errorf("double slash in admin URL:\n%s", b)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
