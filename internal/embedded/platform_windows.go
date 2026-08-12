//go:build windows

package embedded

// embeddedPostgresSupported reports whether this platform can run the
// embedded server. See platform_unix.go for the other side.
//
// Windows has real implementations of the two things the shutdown path
// depends on -- process liveness (processliveness_windows.go) and postmaster
// identity (processidentity_windows.go) -- so this no longer refuses. Nothing
// packages embedded Postgres for Windows today: there is no binaries bundle
// and no packaging target. This flag is about the code being correct if/when
// there is one, not about Windows being a shipped deployment target.
const embeddedPostgresSupported = true
