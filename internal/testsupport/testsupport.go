package testsupport

import "os"

// TestingT captures the small subset of *testing.T that RequireDependency uses.
type TestingT interface {
	Helper()
	Skip(args ...any)
	Fatalf(format string, args ...any)
}

// RequireDependency enforces a dependency gate consistently across tests.
//
// If the dependency is available, it is a no-op. If unavailable, local runs
// skip while CI runs fail loudly so missing CI-provided dependencies do not
// silently pass.
func RequireDependency(t TestingT, _ string, available bool, reason string) {
	requireDependency(t, available, reason, os.Getenv("CI") != "")
}

func requireDependency(t TestingT, available bool, reason string, ci bool) {
	t.Helper()
	if available {
		return
	}
	if ci {
		t.Fatalf("%s", reason)
	}
	t.Skip(reason)
}
